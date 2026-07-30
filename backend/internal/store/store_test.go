package store

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sample(t time.Time, sc, vs, vc, tj, ge, mc int64) Sample {
	return Sample{Time: t, SessionsCreated: sc, VotesStarted: vs, VotesCast: vc, TraineesJoined: tj, GameEnabledVotes: ge, MultipleChoiceVotes: mc}
}

// counters wraps a Sample in a Counters with empty histograms. Used by tests
// that exercise the counter round-trip without caring about distributions.
func counters(t time.Time, sc, vs, vc, tj, ge, mc int64) Counters {
	return Counters{Sample: sample(t, sc, vs, vc, tj, ge, mc)}
}

// sampleEqual compares the time-series-counter portion of two Counters values
// (Histogram contains a slice and therefore can't be compared with ==).
func sampleEqual(a, b Counters) bool {
	return a.Sample == b.Sample &&
		histogramEqual(a.SessionDuration, b.SessionDuration) &&
		histogramEqual(a.VotesPerSession, b.VotesPerSession) &&
		histogramEqual(a.TraineesPerSession, b.TraineesPerSession)
}

func histogramEqual(a, b Histogram) bool {
	if a.Count != b.Count || a.Sum != b.Sum || len(a.Buckets) != len(b.Buckets) {
		return false
	}
	for i := range a.Buckets {
		if a.Buckets[i] != b.Buckets[i] {
			return false
		}
	}
	return true
}

func TestSaveLoadCountersRoundTrip(t *testing.T) {
	s := newTestStore(t)
	want := counters(time.Unix(1700000000, 0).UTC(), 10, 20, 200, 150, 5, 3)
	if err := s.SaveCounters(want); err != nil {
		t.Fatalf("SaveCounters: %v", err)
	}
	got, err := s.LoadCounters()
	if err != nil {
		t.Fatalf("LoadCounters: %v", err)
	}
	if !sampleEqual(got, want) {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestSaveLoadCountersWithHistogramsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	want := Counters{
		Sample: sample(time.Unix(1700000000, 0).UTC(), 3, 5, 50, 12, 1, 0),
		VotesPerSession: Histogram{
			Count: 3,
			Sum:   30,
			Buckets: []HistogramBucket{
				{LE: 0, Count: 0},
				{LE: 5, Count: 2},
				{LE: 10, Count: 2},
				{LE: 50, Count: 3},
			},
		},
		TraineesPerSession: Histogram{Count: 3, Buckets: []HistogramBucket{{LE: 10, Count: 3}}},
	}
	if err := s.SaveCounters(want); err != nil {
		t.Fatalf("SaveCounters: %v", err)
	}
	got, err := s.LoadCounters()
	if err != nil {
		t.Fatalf("LoadCounters: %v", err)
	}
	if !sampleEqual(got, want) {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestLoadCountersMissingIsZero(t *testing.T) {
	s := newTestStore(t)
	got, err := s.LoadCounters()
	if err != nil {
		t.Fatalf("LoadCounters on missing file: %v", err)
	}
	if !sampleEqual(got, Counters{}) {
		t.Errorf("expected zero sample for missing file, got %+v", got)
	}
}

func TestLoadCountersCorruptIsZero(t *testing.T) {
	s := newTestStore(t)
	if err := os.WriteFile(s.countersPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadCounters()
	if err != nil {
		t.Fatalf("LoadCounters on corrupt file should not error: %v", err)
	}
	if !sampleEqual(got, Counters{}) {
		t.Errorf("corrupt file should yield zero, got %+v", got)
	}
}

func TestLoadCountersNegativeRejected(t *testing.T) {
	s := newTestStore(t)
	bad := sample(time.Now(), -1, 0, 0, 0, 0, 0)
	data, _ := json.Marshal(Counters{Sample: bad})
	if err := os.WriteFile(s.countersPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := s.LoadCounters()
	if !sampleEqual(got, Counters{}) {
		t.Errorf("negative counters must be rejected, got %+v", got)
	}
}

func TestLoadCountersFeatureOverflowRejected(t *testing.T) {
	s := newTestStore(t)
	// game-enabled (5) cannot exceed votes-started (3)
	bad := sample(time.Now(), 1, 3, 10, 5, 5, 0)
	data, _ := json.Marshal(Counters{Sample: bad})
	os.WriteFile(s.countersPath, data, 0o600)
	if got, _ := s.LoadCounters(); !sampleEqual(got, Counters{}) {
		t.Errorf("game > started must be rejected, got %+v", got)
	}
}

func TestLoadCountersRejectsNonCumulativeHistogram(t *testing.T) {
	s := newTestStore(t)
	// Buckets must be cumulative non-decreasing: this snapshot claims 5
	// observations ≤ 5 and only 3 ≤ 10, which is impossible.
	bad := Counters{
		Sample: sample(time.Now(), 1, 0, 0, 0, 0, 0),
		VotesPerSession: Histogram{
			Count:   5,
			Buckets: []HistogramBucket{{LE: 5, Count: 5}, {LE: 10, Count: 3}},
		},
	}
	data, _ := json.Marshal(bad)
	os.WriteFile(s.countersPath, data, 0o600)
	if got, _ := s.LoadCounters(); !sampleEqual(got, Counters{}) {
		t.Errorf("non-cumulative histogram must be rejected, got %+v", got)
	}
}

func TestLoadCountersRejectsHistogramBucketExceedsCount(t *testing.T) {
	s := newTestStore(t)
	// A single bucket claims 10 observations but the total count is 5.
	bad := Counters{
		Sample: sample(time.Now(), 1, 0, 0, 0, 0, 0),
		VotesPerSession: Histogram{
			Count:   5,
			Buckets: []HistogramBucket{{LE: 10, Count: 10}},
		},
	}
	data, _ := json.Marshal(bad)
	os.WriteFile(s.countersPath, data, 0o600)
	if got, _ := s.LoadCounters(); !sampleEqual(got, Counters{}) {
		t.Errorf("bucket > count must be rejected, got %+v", got)
	}
}

// TestValidHistogramRejectsNaNSum is the focused R20 unit test. Go's
// encoding/json rejects NaN at both marshal and unmarshal time, so a NaN
// sum cannot reach validHistogram through the LoadCounters JSON path today
// — but validHistogram is also called by validCounters (the pre-write gate
// in SaveCounters) and is the documented last-line guard should the JSON
// decoder ever change (e.g. jsoniter, sonic) or should Sum be set
// programmatically. Asserting the predicate directly attributes the
// rejection to the sum check rather than to a coincident bucket violation.
func TestValidHistogramRejectsNaNSum(t *testing.T) {
	h := Histogram{
		Count: 5,
		Sum:   math.NaN(),
		Buckets: []HistogramBucket{
			{LE: 0, Count: 2},
			{LE: 10, Count: 5},
		},
	}
	if validHistogram(h) {
		t.Errorf("validHistogram must reject NaN sum")
	}
}

// TestValidHistogramRejectsInfSum is the R20 regression for ±Inf — the
// other non-finite class that would surface as `..._sum +Inf` in /metrics.
// Both signs must be rejected.
func TestValidHistogramRejectsInfSum(t *testing.T) {
	for _, tc := range []struct {
		name string
		sum  float64
	}{
		{"posInf", math.Inf(1)},
		{"negInf", math.Inf(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := Histogram{
				Count: 5,
				Sum:   tc.sum,
				Buckets: []HistogramBucket{
					{LE: 0, Count: 2},
					{LE: 10, Count: 5},
				},
			}
			if validHistogram(h) {
				t.Errorf("validHistogram must reject %s sum", tc.name)
			}
		})
	}
}

// TestValidHistogramAcceptsFiniteSum confirms the R20 guard doesn't
// over-suppress: a legitimate finite sum (including a very large one and
// zero) must still pass.
func TestValidHistogramAcceptsFiniteSum(t *testing.T) {
	for _, tc := range []struct {
		name string
		sum  float64
	}{
		{"zero", 0},
		{"small", 3.5},
		{"large", 1e18},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := Histogram{
				Count: 5,
				Sum:   tc.sum,
				Buckets: []HistogramBucket{
					{LE: 0, Count: 2},
					{LE: 10, Count: 5},
				},
			}
			if !validHistogram(h) {
				t.Errorf("validHistogram must accept finite sum %v", tc.sum)
			}
		})
	}
}

// TestSaveCountersRejectsNaNSumBeforeWrite is the R20 pre-write gate:
// SaveCounters runs validCounters (→ validHistogram) before touching disk,
// so a NaN sum set programmatically (e.g. by a future caller that bypasses
// the JSON parser, or by accumulated floating-point drift) is rejected
// before it can poison counters.json. Go's stdlib json.Marshal also errors
// on NaN, so this is defense-in-depth; the test confirms SaveCounters
// surfaces a validation error rather than writing bad state.
func TestSaveCountersRejectsNaNSumBeforeWrite(t *testing.T) {
	s := newTestStore(t)
	bad := Counters{
		Sample: sample(time.Now(), 1, 0, 0, 0, 0, 0),
		VotesPerSession: Histogram{
			Count: 5,
			Sum:   math.NaN(),
			Buckets: []HistogramBucket{
				{LE: 0, Count: 2},
				{LE: 10, Count: 5},
			},
		},
	}
	if err := s.SaveCounters(bad); err == nil {
		t.Error("SaveCounters must reject NaN-sum histogram before write")
	}
	if _, err := os.Stat(s.countersPath); !os.IsNotExist(err) {
		t.Errorf("counters.json must not be written for invalid input: stat=%v", err)
	}
}

func TestLoadCountersBackwardCompatIgnoresAbsentHistograms(t *testing.T) {
	s := newTestStore(t)
	// counters.json written by an older binary lacks the histogram fields.
	// LoadCounters must accept it and leave histograms zero.
	legacy := sample(time.Unix(1700000000, 0).UTC(), 7, 14, 100, 25, 2, 1)
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(s.countersPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadCounters()
	if err != nil {
		t.Fatalf("LoadCounters: %v", err)
	}
	if got.SessionsCreated != 7 || got.VotesCast != 100 {
		t.Errorf("legacy counters not restored: %+v", got)
	}
	if got.SessionDuration.Count != 0 || got.VotesPerSession.Count != 0 || got.TraineesPerSession.Count != 0 {
		t.Errorf("absent histograms should be zero, got %+v", got)
	}
}

func TestAppendReadSamplesRoundTrip(t *testing.T) {
	s := newTestStore(t)
	base := time.Unix(1700000000, 0).UTC()
	for i := 0; i < 5; i++ {
		if err := s.AppendSample(sample(base.Add(time.Duration(i)*time.Minute), int64(i), int64(i*2), int64(i*10), int64(i), 0, 0)); err != nil {
			t.Fatalf("AppendSample %d: %v", i, err)
		}
	}
	got, err := s.ReadSamples(0)
	if err != nil {
		t.Fatalf("ReadSamples: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("expected 5 samples, got %d", len(got))
	}
	if !got[0].Time.Equal(base) {
		t.Errorf("first sample time wrong: %v", got[0].Time)
	}
	if got[4].VotesCast != 40 {
		t.Errorf("last sample vc: expected 40, got %d", got[4].VotesCast)
	}
}

func TestReadSamplesLimitReturnsTail(t *testing.T) {
	s := newTestStore(t)
	base := time.Unix(1700000000, 0).UTC()
	for i := 0; i < 10; i++ {
		s.AppendSample(sample(base.Add(time.Duration(i)*time.Minute), int64(i), 0, 0, 0, 0, 0))
	}
	got, _ := s.ReadSamples(3)
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	if got[0].SessionsCreated != 7 || got[2].SessionsCreated != 9 {
		t.Errorf("expected tail [7,8,9], got %v", []int64{got[0].SessionsCreated, got[1].SessionsCreated, got[2].SessionsCreated})
	}
}

func TestReadSamplesSkipsMalformedLines(t *testing.T) {
	s := newTestStore(t)
	// Manually craft a log with a torn line in the middle.
	s.AppendSample(sample(time.Now(), 1, 0, 0, 0, 0, 0))
	f, _ := os.OpenFile(s.logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("{torn line without newline\n")
	f.WriteString("not-json-at-all\n")
	f.Close()
	s.AppendSample(sample(time.Now().Add(time.Minute), 2, 0, 0, 0, 0, 0))
	got, _ := s.ReadSamples(0)
	if len(got) != 2 {
		t.Errorf("expected 2 valid samples (torn lines skipped), got %d", len(got))
	}
}

func TestRotationKeepsBackup(t *testing.T) {
	s := newTestStore(t)
	s.maxLogBytes = 64 // force rotation quickly
	for i := 0; i < 10; i++ {
		if err := s.AppendSample(sample(time.Unix(int64(1700000000+i), 0), int64(i), 0, 0, 0, 0, 0)); err != nil {
			t.Fatalf("AppendSample %d: %v", i, err)
		}
	}
	if _, err := os.Stat(s.logBackup); err != nil {
		t.Errorf("expected backup %s to exist after rotation: %v", s.logBackup, err)
	}
	// Both files readable and merged in order.
	got, _ := s.ReadSamples(0)
	if len(got) < 2 {
		t.Errorf("merged read should return samples from both files, got %d", len(got))
	}
	// Chronological order preserved across the merge.
	for i := 1; i < len(got); i++ {
		if got[i].Time.Before(got[i-1].Time) {
			t.Errorf("samples out of order at %d", i)
			break
		}
	}
}

func TestFilePermissions(t *testing.T) {
	s := newTestStore(t)
	s.SaveCounters(counters(time.Now(), 1, 0, 0, 0, 0, 0))
	s.AppendSample(sample(time.Now(), 1, 0, 0, 0, 0, 0))

	dirFi, _ := os.Stat(s.dir)
	if perm := dirFi.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir perm %o, want 0700 (umask may have altered it)", perm)
	}
	for _, p := range []string{s.countersPath, s.logPath} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s perm %o, want 0600", p, perm)
		}
	}
	if err := s.Permissions(); err != nil {
		t.Errorf("Permissions() self-check failed: %v", err)
	}
}

// TestAtomicCounterWriteNoPartialRead simulates a crash between write and
// rename: only the temp file exists, counters.json is untouched. A reader must
// see the previous (or absent) value, never a partial one.
func TestAtomicCounterWriteNoPartialRead(t *testing.T) {
	s := newTestStore(t)
	s.SaveCounters(counters(time.Unix(1700000000, 0), 5, 0, 0, 0, 0, 0))
	// Simulate an interrupted write: temp file present, counters.json stale.
	tmp := filepath.Join(s.dir, countersFile+".tmp")
	if err := os.WriteFile(tmp, []byte("{parti"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := s.LoadCounters()
	if got.SessionsCreated != 5 {
		t.Errorf("stale temp must not affect readers; expected 5, got %d", got.SessionsCreated)
	}
}

func TestNewRejectsEmptyDir(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Error("New(\"\") should error")
	}
}

func TestNewCreatesDirIfMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "vote")
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New should create nested dir: %v", err)
	}
	defer s.Close()
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Errorf("dir not created: %v", err)
	}
}

func TestReadSamplesOnMissingLogReturnsEmpty(t *testing.T) {
	s := newTestStore(t)
	os.Remove(s.logPath)
	got, err := s.ReadSamples(0)
	if err != nil {
		t.Fatalf("ReadSamples on missing log: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestPlatformPathNote(t *testing.T) {
	// The 0700/0600 perms are POSIX; on Windows file modes are not enforced
	// the same way. Document the expectation rather than fail the build.
	if runtime.GOOS == "windows" && !strings.HasPrefix(os.Getenv("GOOS"), "linux") {
		t.Skip("file-mode assertions are POSIX-only")
	}
}

// TestAppendSampleReopensAfterClose covers the B9 self-heal path: if
// Close() nils the handle (the graceful-shutdown path), a subsequent
// AppendSample reopens the log instead of writing to a dead fd. In normal
// operation Close is terminal and the sampler has stopped before Close
// runs; this test exercises the defensive recovery that keeps the store
// usable if a transient caller violates that contract.
func TestAppendSampleReopensAfterClose(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	if err := s.AppendSample(sample(now, 1, 0, 0, 0, 0, 0)); err != nil {
		t.Fatalf("first AppendSample: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// logFile is now nil. A subsequent AppendSample must self-heal.
	if err := s.AppendSample(sample(now.Add(time.Second), 2, 0, 0, 0, 0, 0)); err != nil {
		t.Fatalf("AppendSample after Close should self-heal: %v", err)
	}
	got, err := s.ReadSamples(0)
	if err != nil {
		t.Fatalf("ReadSamples: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 samples after reopen, got %d", len(got))
	}
}

// TestRotationReopenFailureLeavesNilHandle covers the B9 regression
// invariant: regardless of HOW s.logFile ends up nil (Close, a rotation
// whose reopen failed, or any other path), the next AppendSample must
// self-heal rather than operate on a stale handle. We force the nil state
// directly because reliably making rotation-rename succeed while reopen
// fails is not portable across filesystems — the invariant we care about
// is the recovery, not the exact failure mode.
//
// We use file perms (not dir perms) to block reopen because O_CREATE on an
// existing file in a read-only dir still succeeds on POSIX — only O_CREAT
// that actually creates needs the dir write bit. A read-only FILE (0444)
// reliably blocks O_WRONLY reopen.
func TestRotationReopenFailureLeavesNilHandle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based test is POSIX-only")
	}
	s := newTestStore(t)
	if err := s.AppendSample(sample(time.Unix(1700000000, 0), 1, 0, 0, 0, 0, 0)); err != nil {
		t.Fatalf("seed AppendSample: %v", err)
	}

	// Simulate "rotation succeeded but reopen failed": the fd is closed,
	// s.logFile is nil, but the on-disk file is untouched. AppendSample
	// should reopen on the next call.
	s.mu.Lock()
	if s.logFile != nil {
		_ = s.logFile.Close()
	}
	s.logFile = nil
	s.mu.Unlock()

	// Make the file read-only so the self-heal reopen FAILS (reopen uses
	// O_WRONLY). AppendSample must surface the error and keep s.logFile
	// nil (B9: NOT a stale handle that silently drops every subsequent
	// sample).
	if err := os.Chmod(s.logPath, 0o444); err != nil {
		t.Fatalf("chmod file read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(s.logPath, 0o600) })

	if err := s.AppendSample(sample(time.Unix(1700000001, 0), 2, 0, 0, 0, 0, 0)); err == nil {
		t.Fatal("expected AppendSample to fail when reopen fails on a nil handle")
	}
	s.mu.Lock()
	staleHandle := s.logFile
	s.mu.Unlock()
	if staleHandle != nil {
		t.Errorf("s.logFile must remain nil after failed reopen (B9), got non-nil handle")
	}

	// Restore perms: next AppendSample must succeed via the self-heal
	// path (s.logFile == nil → reopen → write).
	if err := os.Chmod(s.logPath, 0o600); err != nil {
		t.Fatalf("chmod restore: %v", err)
	}
	if err := s.AppendSample(sample(time.Unix(1700000002, 0), 3, 0, 0, 0, 0, 0)); err != nil {
		t.Errorf("AppendSample should self-heal after perms restored: %v", err)
	}

	got, err := s.ReadSamples(0)
	if err != nil {
		t.Fatalf("ReadSamples: %v", err)
	}
	if len(got) < 2 {
		t.Errorf("expected at least 2 samples after recovery, got %d", len(got))
	}
}

// TestAppendSampleRecoversFromExternalLogTruncation covers the B9 write-
// retry path: if an admin script (or FS unmount) truncates or removes the
// log out-of-band, the next AppendSample's Write fails. The store should
// reopen the log and retry the write rather than fail every subsequent
// sample on a dead fd.
func TestAppendSampleRecoversFromExternalLogTruncation(t *testing.T) {
	s := newTestStore(t)
	if err := s.AppendSample(sample(time.Unix(1700000000, 0), 1, 0, 0, 0, 0, 0)); err != nil {
		t.Fatalf("first AppendSample: %v", err)
	}

	// Simulate external tampering: close and remove the log file under
	// the store's feet, leaving s.logFile pointing at a deleted inode.
	s.mu.Lock()
	if s.logFile != nil {
		_ = s.logFile.Close()
	}
	s.mu.Unlock()
	_ = os.Remove(s.logPath)

	// The write fails on the closed fd; the store should reopen and
	// retry, succeeding overall.
	if err := s.AppendSample(sample(time.Unix(1700000001, 0), 2, 0, 0, 0, 0, 0)); err != nil {
		t.Errorf("AppendSample should recover from external tampering: %v", err)
	}

	// The reopened log should contain only the recovered sample (the
	// original was deleted).
	got, err := s.ReadSamples(0)
	if err != nil {
		t.Fatalf("ReadSamples: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 sample after recovery, got %d", len(got))
	}
	if got[0].SessionsCreated != 2 {
		t.Errorf("recovered sample value mismatch: got %d, want 2", got[0].SessionsCreated)
	}
}

// TestSaveCountersRemovesTempFile is the R21 structural assertion: after a
// successful SaveCounters, the temp file must be gone (the rename-and-fsync
// path ran end-to-end) and counters.json must hold the new payload. A leftover
// .tmp would mean the rename was skipped; combined with the durability-recipe
// doc assertion below, this guards against a regression that drops fsync.
func TestSaveCountersRemovesTempFile(t *testing.T) {
	s := newTestStore(t)
	want := counters(time.Unix(1700000000, 0).UTC(), 7, 14, 100, 25, 2, 1)
	if err := s.SaveCounters(want); err != nil {
		t.Fatalf("SaveCounters: %v", err)
	}
	tmp := filepath.Join(s.dir, countersFile+".tmp")
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("temp file must not remain after successful SaveCounters: stat=%v", err)
	}
	got, err := s.LoadCounters()
	if err != nil {
		t.Fatalf("LoadCounters: %v", err)
	}
	if !sampleEqual(got, want) {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, want)
	}
}

// TestSyncDirAndSyncPathHelpersRun confirms the R21 durability primitives
// don't error on the platform under test. fsync of a directory fd is the
// standard POSIX durability recipe (the post-rename metadata commit); on
// some filesystems / CI sandboxes fsync is a no-op, which is fine — the
// contract is "best-effort durability", and what we are asserting here is
// that the helpers are wired and callable, not that they physically flush.
func TestSyncDirAndSyncPathHelpersRun(t *testing.T) {
	dir := t.TempDir()
	if err := syncDir(dir); err != nil {
		t.Errorf("syncDir(%s): %v", dir, err)
	}
	f := filepath.Join(dir, "scratch")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syncPath(f); err != nil {
		t.Errorf("syncPath(%s): %v", f, err)
	}
}

// TestSaveCountersDocAssertsDurabilityRecipe is the R21 doc-contract guard.
// The package doc and SaveCounters doc must reference both fsync steps of
// the portable durability recipe (temp-file fsync + directory fsync) so a
// future refactor can't silently collapse them back to bare WriteFile +
// Rename — the comment is the load-bearing reminder of why the extra syscalls
// are there. If this test fails the recipe has been edited and the durability
// claim needs to be re-evaluated against the new code.
func TestSaveCountersDocAssertsDurabilityRecipe(t *testing.T) {
	pkgDoc, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatalf("read store.go: %v", err)
	}
	src := string(pkgDoc)
	for _, want := range []string{
		"fsync",             // durability primitive present in recipe
		"directory",         // post-rename dir fsync referenced
		"crash-durability",  // contract documented
		"atomic visibility", // distinguished from crash-durability
	} {
		if !strings.Contains(src, want) {
			t.Errorf("store.go doc contract missing %q — R21 durability recipe was edited; re-evaluate", want)
		}
	}
}
