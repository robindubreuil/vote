package vote

import (
	"sync"
	"testing"
)

func TestCounterIncrement(t *testing.T) {
	var c Counter
	c.Inc()
	c.Inc()
	c.Add(3)
	if got := c.Value(); got != 5 {
		t.Errorf("expected 5, got %d", got)
	}
}

func TestHistogramObserve(t *testing.T) {
	h := NewHistogram([]float64{1, 5, 10})
	for _, v := range []float64{0.5, 3, 3, 7, 12} {
		h.Observe(v)
	}
	snap := h.Snapshot()
	if snap.Count != 5 {
		t.Fatalf("expected count 5, got %d", snap.Count)
	}
	if snap.Sum != 25.5 {
		t.Errorf("expected sum 25.5, got %v", snap.Sum)
	}
	wantLE := []struct {
		le    float64
		count int64
	}{
		{1, 1},  // 0.5
		{5, 3},  // 0.5, 3, 3
		{10, 4}, // + 7
	}
	for i, w := range wantLE {
		if snap.Buckets[i].LE != w.le || snap.Buckets[i].Count != w.count {
			t.Errorf("bucket %d: expected le=%v count=%d, got le=%v count=%d",
				i, w.le, w.count, snap.Buckets[i].LE, snap.Buckets[i].Count)
		}
	}
}

func TestHistogramEmpty(t *testing.T) {
	h := NewHistogram([]float64{1, 5})
	snap := h.Snapshot()
	if snap.Count != 0 || snap.Sum != 0 {
		t.Errorf("expected zero snapshot, got count=%d sum=%v", snap.Count, snap.Sum)
	}
	if len(snap.Buckets) != 2 {
		t.Errorf("expected 2 buckets, got %d", len(snap.Buckets))
	}
}

func TestProductStatsWiredThroughManager(t *testing.T) {
	m := NewManager()
	sess, err := m.CreateSession("ABC", "trainer1")
	if err != nil {
		t.Fatal(err)
	}
	const id1 = "stagiaire001"
	const id2 = "stagiaire002"
	m.JoinStagiaire("ABC", id1, "Alice", "")
	m.JoinStagiaire("ABC", id2, "Bob", "")
	if err := m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, false, false); err != nil {
		t.Fatal(err)
	}
	m.SubmitVote("ABC", id1, []string{"rouge"})
	m.SubmitVote("ABC", id2, []string{"rouge"})

	snap := m.Stats().Snapshot()
	if snap.SessionsCreated != 1 {
		t.Errorf("SessionsCreated: expected 1, got %d", snap.SessionsCreated)
	}
	if snap.TraineesJoined != 2 {
		t.Errorf("TraineesJoined: expected 2, got %d", snap.TraineesJoined)
	}
	if snap.VotesStarted != 1 {
		t.Errorf("VotesStarted: expected 1, got %d", snap.VotesStarted)
	}
	if snap.VotesCast != 2 {
		t.Errorf("VotesCast: expected 2, got %d", snap.VotesCast)
	}
	if snap.GameEnabledVotes != 0 {
		t.Errorf("GameEnabledVotes: expected 0, got %d", snap.GameEnabledVotes)
	}

	// Ending the session should observe exactly one sample in each histogram.
	m.RemoveSession("ABC")
	snap = m.Stats().Snapshot()
	if snap.VotesPerSession.Count != 1 {
		t.Errorf("VotesPerSession.Count after removal: expected 1, got %d", snap.VotesPerSession.Count)
	}
	if snap.TraineesPerSession.Count != 1 {
		t.Errorf("TraineesPerSession.Count after removal: expected 1, got %d", snap.TraineesPerSession.Count)
	}
	if snap.SessionDuration.Count != 1 {
		t.Errorf("SessionDuration.Count after removal: expected 1, got %d", snap.SessionDuration.Count)
	}
	// Avoid unused warning for sess.
	_ = sess
}

func TestProductStatsFeatureFlags(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	if err := m.StartVote("ABC", "trainer1", []string{"rouge", "bleu"}, true, nil, true, false, false); err != nil {
		t.Fatal(err)
	}
	snap := m.Stats().Snapshot()
	if snap.GameEnabledVotes != 1 {
		t.Errorf("GameEnabledVotes: expected 1, got %d", snap.GameEnabledVotes)
	}
	if snap.MultipleChoiceVotes != 1 {
		t.Errorf("MultipleChoiceVotes: expected 1, got %d", snap.MultipleChoiceVotes)
	}
}

func TestProductStatsFailedVoteDoesNotCount(t *testing.T) {
	m := NewManager()
	// No session created — StartVote must error and must NOT bump the counter.
	_ = m.StartVote("NOPE", "trainer1", []string{"rouge"}, false, nil, false, false, false)
	snap := m.Stats().Snapshot()
	if snap.VotesStarted != 0 {
		t.Errorf("VotesStarted should be 0 after failed start, got %d", snap.VotesStarted)
	}
}

// TestHistogramSnapshotNeverTorn stresses the BM3 invariant: under
// concurrent Observe + Snapshot, the snapshot's total count must always be
// ≥ every cumulative bucket count (Prometheus's monotonicity requirement
// — the implicit +Inf bucket equals count). Before the RWMutex was added,
// a racy snapshot could observe count incremented but buckets not yet
// updated, violating the invariant and causing /metrics scrapes to fail.
func TestHistogramSnapshotNeverTorn(t *testing.T) {
	h := NewHistogram([]float64{1, 5, 10, 50})
	const observers = 4
	const writers = 4
	const iters = 500

	var wg sync.WaitGroup

	// Readers: each does a fixed number of snapshots to bound runtime
	// (hot-loop readers starve writers under -race and turn the test
	// into a minutes-long mutex brawl).
	for r := 0; r < observers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				snap := h.Snapshot()
				var highest int64
				for _, b := range snap.Buckets {
					if b.Count > highest {
						highest = b.Count
					}
				}
				if highest > snap.Count {
					t.Errorf("torn snapshot: bucket %d > count %d", highest, snap.Count)
					return
				}
			}
		}()
	}

	// Writers: observe a mix of values across buckets.
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				v := float64((seed + i) % 60) // 0..59, scatters across all buckets
				h.Observe(v)
			}
		}(w)
	}

	wg.Wait()

	// Final sanity check: the totals match what we wrote.
	wantCount := int64(writers * iters)
	snap := h.Snapshot()
	if snap.Count != wantCount {
		t.Errorf("final count: expected %d, got %d", wantCount, snap.Count)
	}
}

// TestHistogramRestoreRejectsMismatchedBuckets is the BM1 regression test:
// when the persisted snapshot's bucket LEs don't exactly match the live
// histogram's schema (e.g. a deploy changed boundaries), the restore must
// refuse rather than partially replay counts. A partial replay would leave
// the total count larger than the largest cumulative bucket, breaking
// Prometheus monotonicity and failing the /metrics scrape.
func TestHistogramRestoreRejectsMismatchedBuckets(t *testing.T) {
	// Live schema: [1, 5, 10]. Persisted snapshot claims [1, 5, 50].
	live := NewHistogram([]float64{1, 5, 10})
	ps := &ProductStats{VotesPerSession: live}

	before := live.Snapshot()
	ps.Restore(ProductStatsSnapshot{
		VotesPerSession: HistogramSnapshot{
			Count: 7,
			Sum:   42,
			Buckets: []HistogramBucket{
				{LE: 1, Count: 2},
				{LE: 5, Count: 5},
				{LE: 50, Count: 7}, // mismatched LE
			},
		},
	})
	after := live.Snapshot()

	if after.Count != before.Count {
		t.Errorf("mismatched-bucket restore must be a no-op: count %d → %d",
			before.Count, after.Count)
	}
	if after.Sum != before.Sum {
		t.Errorf("mismatched-bucket restore must be a no-op: sum %v → %v",
			before.Sum, after.Sum)
	}
	for i := range after.Buckets {
		if after.Buckets[i].Count != before.Buckets[i].Count {
			t.Errorf("bucket le=%v mutated: %d → %d",
				after.Buckets[i].LE, before.Buckets[i].Count, after.Buckets[i].Count)
		}
	}
}

// TestHistogramRestoreAcceptsMatchingBuckets verifies the happy path of BM1:
// an exact bucket-set match replays as before.
func TestHistogramRestoreAcceptsMatchingBuckets(t *testing.T) {
	live := NewHistogram([]float64{1, 5, 10})
	ps := &ProductStats{VotesPerSession: live}

	ps.Restore(ProductStatsSnapshot{
		VotesPerSession: HistogramSnapshot{
			Count: 4,
			Sum:   12,
			Buckets: []HistogramBucket{
				{LE: 1, Count: 1},
				{LE: 5, Count: 3},
				{LE: 10, Count: 4},
			},
		},
	})

	// New observation must accumulate on top of the restored totals,
	// proving addLocked ran.
	live.Observe(7) // lands in le=10 only

	snap := live.Snapshot()
	if snap.Count != 5 {
		t.Errorf("expected count 5 after restore+observe, got %d", snap.Count)
	}
	want := []struct {
		le    float64
		count int64
	}{
		{1, 1}, {5, 3}, {10, 5},
	}
	for i, w := range want {
		if snap.Buckets[i].LE != w.le || snap.Buckets[i].Count != w.count {
			t.Errorf("bucket %d: expected le=%v count=%d, got le=%v count=%d",
				i, w.le, w.count, snap.Buckets[i].LE, snap.Buckets[i].Count)
		}
	}
}

// TestHistogramRestoreEmptySnapshotNoop guards the early return: a zero
// snapshot must not perturb live state.
func TestHistogramRestoreEmptySnapshotNoop(t *testing.T) {
	live := NewHistogram([]float64{1, 5})
	live.Observe(2)
	live.Observe(4)
	before := live.Snapshot()

	ps := &ProductStats{VotesPerSession: live}
	ps.Restore(ProductStatsSnapshot{
		VotesPerSession: HistogramSnapshot{Count: 0, Buckets: []HistogramBucket{{LE: 1}, {LE: 5}}},
	})

	after := live.Snapshot()
	if after.Count != before.Count || after.Sum != before.Sum {
		t.Errorf("empty restore mutated state: %+v → %+v", before, after)
	}
}

// TestHistogramRestoreLengthMismatch covers the trivial schema-mismatch
// case (different bucket count).
func TestHistogramRestoreLengthMismatch(t *testing.T) {
	live := NewHistogram([]float64{1, 5, 10})
	before := live.Snapshot()
	ps := &ProductStats{VotesPerSession: live}

	ps.Restore(ProductStatsSnapshot{
		VotesPerSession: HistogramSnapshot{
			Count: 3,
			Buckets: []HistogramBucket{
				{LE: 1, Count: 1},
				{LE: 5, Count: 3}, // missing le=10
			},
		},
	})

	after := live.Snapshot()
	if after.Count != before.Count {
		t.Errorf("length-mismatch restore must be a no-op: %d → %d",
			before.Count, after.Count)
	}
}

// TestRestoreIsolatedAcrossHistograms checks that a schema mismatch on one
// histogram doesn't block the others (BM1 isolation).
func TestRestoreIsolatedAcrossHistograms(t *testing.T) {
	stats := NewProductStats()
	durationBefore := stats.SessionDurationSecs.Snapshot()
	votesBefore := stats.VotesPerSession.Snapshot()

	stats.Restore(ProductStatsSnapshot{
		SessionsCreated: 11,
		VotesPerSession: HistogramSnapshot{
			Count: 4,
			Buckets: []HistogramBucket{
				{LE: 0, Count: 1},
				{LE: 1, Count: 2},
				{LE: 2, Count: 3},
				{LE: 3, Count: 4},
				{LE: 5, Count: 4},
				{LE: 10, Count: 4},
				{LE: 20, Count: 4},
				{LE: 50, Count: 4},
			},
		},
		// SessionDuration with the wrong schema — must NOT restore.
		SessionDuration: HistogramSnapshot{
			Count: 9,
			Buckets: []HistogramBucket{
				{LE: 1, Count: 9}, // wrong LE
				{LE: 999, Count: 9},
			},
		},
	})

	if got := stats.SessionsCreated.Value(); got != 11 {
		t.Errorf("counter restore: expected 11, got %d", got)
	}
	votesAfter := stats.VotesPerSession.Snapshot()
	if votesAfter.Count != votesBefore.Count+4 {
		t.Errorf("matching-bucket restore must apply: %d → %d (wanted +4)",
			votesBefore.Count, votesAfter.Count)
	}
	durationAfter := stats.SessionDurationSecs.Snapshot()
	if durationAfter.Count != durationBefore.Count {
		t.Errorf("mismatched-bucket histogram must be skipped: %d → %d",
			durationBefore.Count, durationAfter.Count)
	}
}

// TestHistogramBucketsMatchLocked is the focused unit test for the
// schema-match predicate that gates BM1.
func TestHistogramBucketsMatchLocked(t *testing.T) {
	h := NewHistogram([]float64{1, 5, 10})
	cases := []struct {
		name string
		snap []HistogramBucket
		want bool
	}{
		{"exact", []HistogramBucket{{LE: 1}, {LE: 5}, {LE: 10}}, true},
		{"reordered", []HistogramBucket{{LE: 5}, {LE: 1}, {LE: 10}}, false},
		{"truncated", []HistogramBucket{{LE: 1}, {LE: 5}}, false},
		{"extra", []HistogramBucket{{LE: 1}, {LE: 5}, {LE: 10}, {LE: 50}}, false},
		{"different_values", []HistogramBucket{{LE: 1}, {LE: 5}, {LE: 50}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h.mu.RLock()
			defer h.mu.RUnlock()
			got := h.bucketsMatchLocked(HistogramSnapshot{Buckets: tc.snap})
			if got != tc.want {
				t.Errorf("bucketsMatchLocked(%v): expected %v, got %v",
					tc.snap, tc.want, got)
			}
		})
	}
}

// TestCounterConcurrentAdd exercises the atomic-backed Counter (B1) to
// confirm -race is happy and the count is exact under contention.
func TestCounterConcurrentAdd(t *testing.T) {
	const goroutines = 16
	const perG = 1000
	var c Counter
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				c.Inc()
			}
		}()
	}
	wg.Wait()
	if want := int64(goroutines * perG); c.Value() != want {
		t.Errorf("expected %d, got %d", want, c.Value())
	}
}
