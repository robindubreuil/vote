package vote

import (
	"math"
	"sync/atomic"
	"time"
)

// Counter is a monotonically increasing counter, safe for concurrent use.
// Counters are restored from counters.json on boot via Restore so they read
// as all-time monotonic across process restarts.
type Counter struct {
	value atomic.Int64
}

func (c *Counter) Inc()         { c.value.Add(1) }
func (c *Counter) Add(n int64)  { c.value.Add(n) }
func (c *Counter) Value() int64 { return c.value.Load() }

// Histogram tracks the distribution of observations across fixed buckets in
// Prometheus cumulative-histogram format. Each bucket counts the number of
// observations that fell at or below its upper bound (le = "less than or
// equal"). Bucket boundaries are immutable for the process lifetime so the
// exposition stays consistent for scrapers.
type Histogram struct {
	count   atomic.Int64
	sumBits atomic.Uint64
	buckets []float64
	counts  []atomic.Int64
}

func NewHistogram(buckets []float64) *Histogram {
	b := make([]float64, len(buckets))
	copy(b, buckets)
	return &Histogram{
		buckets: b,
		counts:  make([]atomic.Int64, len(b)),
	}
}

func (h *Histogram) Observe(v float64) {
	h.count.Add(1)
	addFloat(&h.sumBits, v)
	for i, le := range h.buckets {
		if v <= le {
			h.counts[i].Add(1)
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
	snap := HistogramSnapshot{
		Count:   h.count.Load(),
		Sum:     math.Float64frombits(h.sumBits.Load()),
		Buckets: make([]HistogramBucket, len(h.buckets)),
	}
	for i, le := range h.buckets {
		snap.Buckets[i] = HistogramBucket{LE: le, Count: h.counts[i].Load()}
	}
	return snap
}

// addFloat atomically adds v to the float64 whose bits live in bits. Uses a
// CAS loop because this toolchain lacks atomic.Float64.
func addFloat(bits *atomic.Uint64, v float64) {
	for {
		old := bits.Load()
		new := math.Float64bits(math.Float64frombits(old) + v)
		if bits.CompareAndSwap(old, new) {
			return
		}
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

// Restore seeds the cumulative counters with a persisted base so they read as
// all-time monotonic across process restarts. Called once on boot before the
// server accepts traffic. Histograms are intentionally not restored — they
// describe the current run's distribution only.
func (s *ProductStats) Restore(snap ProductStatsSnapshot) {
	s.SessionsCreated.Add(snap.SessionsCreated)
	s.VotesStarted.Add(snap.VotesStarted)
	s.VotesCast.Add(snap.VotesCast)
	s.TraineesJoined.Add(snap.TraineesJoined)
	s.GameEnabledVotes.Add(snap.GameEnabledVotes)
	s.MultipleChoiceVotes.Add(snap.MultipleChoiceVotes)
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
