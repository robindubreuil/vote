package store

import (
	"encoding/json"
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

func TestSaveLoadCountersRoundTrip(t *testing.T) {
	s := newTestStore(t)
	want := sample(time.Unix(1700000000, 0).UTC(), 10, 20, 200, 150, 5, 3)
	if err := s.SaveCounters(want); err != nil {
		t.Fatalf("SaveCounters: %v", err)
	}
	got, err := s.LoadCounters()
	if err != nil {
		t.Fatalf("LoadCounters: %v", err)
	}
	if got != want {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestLoadCountersMissingIsZero(t *testing.T) {
	s := newTestStore(t)
	got, err := s.LoadCounters()
	if err != nil {
		t.Fatalf("LoadCounters on missing file: %v", err)
	}
	if got != (Sample{}) {
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
	if got != (Sample{}) {
		t.Errorf("corrupt file should yield zero, got %+v", got)
	}
}

func TestLoadCountersNegativeRejected(t *testing.T) {
	s := newTestStore(t)
	bad := sample(time.Now(), -1, 0, 0, 0, 0, 0)
	data, _ := json.Marshal(bad)
	if err := os.WriteFile(s.countersPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := s.LoadCounters()
	if got != (Sample{}) {
		t.Errorf("negative counters must be rejected, got %+v", got)
	}
}

func TestLoadCountersFeatureOverflowRejected(t *testing.T) {
	s := newTestStore(t)
	// game-enabled (5) cannot exceed votes-started (3)
	bad := sample(time.Now(), 1, 3, 10, 5, 5, 0)
	data, _ := json.Marshal(bad)
	os.WriteFile(s.countersPath, data, 0o600)
	if got, _ := s.LoadCounters(); got != (Sample{}) {
		t.Errorf("game > started must be rejected, got %+v", got)
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
	s.SaveCounters(sample(time.Now(), 1, 0, 0, 0, 0, 0))
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
	s.SaveCounters(sample(time.Unix(1700000000, 0), 5, 0, 0, 0, 0, 0))
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
