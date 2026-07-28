package vote

import (
	"log/slog"
	"sync"
	"time"
)

// Counter is a monotonically increasing counter, safe for concurrent use.
// Counters are restored from counters.json on boot via Restore so they read
// as all-time monotonic across process restarts.
type Counter struct {
	value int64
	mu    sync.RWMutex
}

func (c *Counter) Inc() {
	c.mu.Lock()
	c.value++
	c.mu.Unlock()
}

func (c *Counter) Add(n int64) {
	c.mu.Lock()
	c.value += n
	c.mu.Unlock()
}

func (c *Counter) Value() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.value
}

// Histogram tracks the distribution of observations across fixed buckets in
// Prometheus cumulative-histogram format. Each bucket counts the number of
// observations that fell at or below its upper bound (le = "less than or
// equal"). Bucket boundaries are immutable for the process lifetime so the
// exposition stays consistent for scrapers.
//
// All mutation and read-back is serialised by a single RWMutex (BM3): a
// concurrent Snapshot previously captured torn state — count incremented
// while buckets were only partially updated — which violates the Prometheus
// invariant that cumulative buckets are monotonically non-decreasing and
// that the implicit +Inf bucket equals the total count. A snapshot taken
// under the read-lock now always sees a coherent view.
type Histogram struct {
	mu      sync.RWMutex
	count   int64
	sum     float64
	buckets []float64
	counts  []int64
}

func NewHistogram(buckets []float64) *Histogram {
	b := make([]float64, len(buckets))
	copy(b, buckets)
	return &Histogram{
		buckets: b,
		counts:  make([]int64, len(b)),
	}
}

// Observe records a single observation. The bucket loop runs from the
// highest LE down so that, even under a racy reader that bypassed the
// mutex, an in-flight Observe would leave the lower (cumulatively-larger)
// buckets already incremented — belt-and-suspenders alongside the lock.
func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count++
	h.sum += v
	for i := len(h.buckets) - 1; i >= 0; i-- {
		if v <= h.buckets[i] {
			h.counts[i]++
		}
	}
}

type HistogramSnapshot struct {
	Count   int64
	Sum     float64
	Buckets []HistogramBucket
}

type HistogramBucket struct {
	LE    float64
	Count int64
}

func (h *Histogram) Snapshot() HistogramSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	snap := HistogramSnapshot{
		Count:   h.count,
		Sum:     h.sum,
		Buckets: make([]HistogramBucket, len(h.buckets)),
	}
	for i, le := range h.buckets {
		snap.Buckets[i] = HistogramBucket{LE: le, Count: h.counts[i]}
	}
	return snap
}

// bucketsMatchLocked reports whether the live histogram's bucket boundaries
// are exactly the same (same LE values, same order) as the persisted
// snapshot's. Caller holds h.mu (BM1).
func (h *Histogram) bucketsMatchLocked(snap HistogramSnapshot) bool {
	if len(h.buckets) != len(snap.Buckets) {
		return false
	}
	for i, le := range h.buckets {
		if snap.Buckets[i].LE != le {
			return false
		}
	}
	return true
}

// addLocked replays a persisted cumulative snapshot into the live histogram
// by adding count/sum/bucket totals in bulk. Caller holds h.mu.
func (h *Histogram) addLocked(snap HistogramSnapshot) {
	h.count += snap.Count
	h.sum += snap.Sum
	for i, b := range snap.Buckets {
		h.counts[i] += b.Count
	}
}

// ProductStats holds aggregate usage metrics that describe how the app is
// used: how many sessions, votes, trainees, and which features see adoption.
// All fields are safe for concurrent access.
type ProductStats struct {
	SessionsCreated     Counter
	VotesStarted        Counter
	VotesCast           Counter
	TraineesJoined      Counter
	GameEnabledVotes    Counter
	MultipleChoiceVotes Counter
	SessionDurationSecs *Histogram
	VotesPerSession     *Histogram
	TraineesPerSession  *Histogram
}

func NewProductStats() *ProductStats {
	return &ProductStats{
		SessionDurationSecs: NewHistogram([]float64{
			1 * 60, 5 * 60, 15 * 60, 30 * 60, 60 * 60, 2 * 60 * 60, 4 * 60 * 60,
		}),
		VotesPerSession:    NewHistogram([]float64{0, 1, 2, 3, 5, 10, 20, 50}),
		TraineesPerSession: NewHistogram([]float64{0, 1, 5, 10, 15, 20, 30, 50}),
	}
}

// ProductStatsSnapshot is a point-in-time, marshalling-friendly copy of
// ProductStats. Counters are deltas from process start.
type ProductStatsSnapshot struct {
	SessionsCreated     int64
	VotesStarted        int64
	VotesCast           int64
	TraineesJoined      int64
	GameEnabledVotes    int64
	MultipleChoiceVotes int64
	SessionDuration     HistogramSnapshot
	VotesPerSession     HistogramSnapshot
	TraineesPerSession  HistogramSnapshot
}

func (s *ProductStats) Snapshot() ProductStatsSnapshot {
	return ProductStatsSnapshot{
		SessionsCreated:     s.SessionsCreated.Value(),
		VotesStarted:        s.VotesStarted.Value(),
		VotesCast:           s.VotesCast.Value(),
		TraineesJoined:      s.TraineesJoined.Value(),
		GameEnabledVotes:    s.GameEnabledVotes.Value(),
		MultipleChoiceVotes: s.MultipleChoiceVotes.Value(),
		SessionDuration:     s.SessionDurationSecs.Snapshot(),
		VotesPerSession:     s.VotesPerSession.Snapshot(),
		TraineesPerSession:  s.TraineesPerSession.Snapshot(),
	}
}

// Restore seeds the cumulative counters and histograms with a persisted base
// so they read as all-time monotonic across process restarts. Called once on
// boot before the server accepts traffic. Histogram buckets are matched by
// their LE bound so a future change to the bucket schema doesn't corrupt the
// in-memory histogram — unmatched bounds in the snapshot are skipped, and
// unmatched bounds in the live histogram stay at zero.
func (s *ProductStats) Restore(snap ProductStatsSnapshot) {
	s.SessionsCreated.Add(snap.SessionsCreated)
	s.VotesStarted.Add(snap.VotesStarted)
	s.VotesCast.Add(snap.VotesCast)
	s.TraineesJoined.Add(snap.TraineesJoined)
	s.GameEnabledVotes.Add(snap.GameEnabledVotes)
	s.MultipleChoiceVotes.Add(snap.MultipleChoiceVotes)
	restoreHistogram(s.SessionDurationSecs, snap.SessionDuration)
	restoreHistogram(s.VotesPerSession, snap.VotesPerSession)
	restoreHistogram(s.TraineesPerSession, snap.TraineesPerSession)
}

// restoreHistogram replays a persisted snapshot into a live histogram.
//
// BM1: refuse-to-restore on schema mismatch. The snapshot's bucket set
// (LE values, in order) must exactly match the live histogram's. If it
// does not — e.g. a deploy changed bucket boundaries — replaying would
// either drop counts (snapshot has buckets the live view doesn't) or
// leave the total `count` larger than the largest cumulative bucket
// (live has buckets the snapshot doesn't), both of which trip
// Prometheus's monotonicity check and cause the /metrics scrape to
// fail. We log and skip the entire histogram in that case; counters
// and any other matching histograms are still restored.
func restoreHistogram(h *Histogram, snap HistogramSnapshot) {
	if snap.Count == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.bucketsMatchLocked(snap) {
		slog.Warn("Skipping histogram restore: bucket schema changed",
			"snapshot_buckets", bucketLEs(snap.Buckets),
			"live_buckets", h.buckets)
		return
	}
	h.addLocked(snap)
}

func bucketLEs(bs []HistogramBucket) []float64 {
	out := make([]float64, len(bs))
	for i, b := range bs {
		out[i] = b.LE
	}
	return out
}

// observeEndedSession records distribution metrics for a session that is being
// torn down. Called under the Manager's write lock from the removal paths.
func (s *ProductStats) observeEndedSession(createdAt int64, voteCount, traineeCount int) {
	if createdAt > 0 {
		s.SessionDurationSecs.Observe(time.Since(time.Unix(createdAt, 0)).Seconds())
	}
	s.VotesPerSession.Observe(float64(voteCount))
	s.TraineesPerSession.Observe(float64(traineeCount))
}
