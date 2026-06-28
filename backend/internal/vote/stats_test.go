package vote

import (
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
	m.JoinStagiaire("ABC", id1, "Alice")
	m.JoinStagiaire("ABC", id2, "Bob")
	if err := m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false); err != nil {
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
	if err := m.StartVote("ABC", "trainer1", []string{"rouge", "bleu"}, true, nil, true); err != nil {
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
	_ = m.StartVote("NOPE", "trainer1", []string{"rouge"}, false, nil, false)
	snap := m.Stats().Snapshot()
	if snap.VotesStarted != 0 {
		t.Errorf("VotesStarted should be 0 after failed start, got %d", snap.VotesStarted)
	}
}
