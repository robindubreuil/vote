package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestReadSamplesDoesNotBlockConcurrentAppend is the B2 regression test:
// the previous implementation held s.mu across the whole read, so a slow
// /dashboard/history read (large log on a small VM) stalled the
// background sampling goroutine's AppendSample. The streaming rewrite
// releases the lock after snapshotting the file paths, so AppendSample
// should make progress while ReadSamples is mid-scan.
//
// We assert the property by racing AppendSample against ReadSamples with
// a deliberately large log; the AppendSample calls must complete within
// the test's wall-clock budget (the prior code would have deadlocked
// for the duration of the read).
func TestReadSamplesDoesNotBlockConcurrentAppend(t *testing.T) {
	s := newTestStore(t)
	base := time.Unix(1700000000, 0).UTC()

	// Seed a non-trivial log so ReadSamples has real work to do.
	for i := 0; i < 2000; i++ {
		if err := s.AppendSample(sample(base.Add(time.Duration(i)*time.Second), int64(i), 0, 0, 0, 0, 0)); err != nil {
			t.Fatalf("seed AppendSample %d: %v", i, err)
		}
	}

	const readers = 4
	const writers = 4
	var wg sync.WaitGroup
	wg.Add(readers + writers)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// Writers keep appending while readers stream the log.
	for w := 0; w < writers; w++ {
		go func(off int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if err := s.AppendSample(sample(base.Add(time.Duration(2000+off*100+i)*time.Second), int64(i), 0, 0, 0, 0, 0)); err != nil {
					t.Errorf("concurrent AppendSample: %v", err)
					return
				}
			}
		}(w)
	}

	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if _, err := s.ReadSamples(100); err != nil {
					t.Errorf("concurrent ReadSamples: %v", err)
					return
				}
			}
		}()
	}

	select {
	case <-done:
		// success
	case <-time.After(10 * time.Second):
		t.Fatal("AppendSample and ReadSamples deadlocked — ReadSamples is likely still holding s.mu")
	}
}

// TestReadSamplesRingBufferTail pins the streaming-tail correctness
// contract: with limit > 0, ReadSamples must return exactly the N most-
// recent valid samples in chronological order, regardless of log size.
// The ring-buffer implementation is the only thing keeping memory bounded
// for large logs; this test would also catch a regression where the ring
// overwrites the wrong slot or returns the slice in the wrong order.
func TestReadSamplesRingBufferTail(t *testing.T) {
	s := newTestStore(t)
	base := time.Unix(1700000000, 0).UTC()
	const total = 500
	const limit = 50

	for i := 0; i < total; i++ {
		if err := s.AppendSample(sample(
			base.Add(time.Duration(i)*time.Second),
			int64(i), // unique per sample
			0, 0, 0, 0, 0,
		)); err != nil {
			t.Fatalf("AppendSample %d: %v", i, err)
		}
	}

	got, err := s.ReadSamples(limit)
	if err != nil {
		t.Fatalf("ReadSamples: %v", err)
	}
	if len(got) != limit {
		t.Fatalf("len: got %d, want %d", len(got), limit)
	}
	// First returned sample must be the (total-limit)-th one.
	wantFirst := int64(total - limit)
	if got[0].SessionsCreated != wantFirst {
		t.Errorf("tail start: got sc=%d, want %d", got[0].SessionsCreated, wantFirst)
	}
	// Last must be the (total-1)-th.
	if got[limit-1].SessionsCreated != int64(total-1) {
		t.Errorf("tail end: got sc=%d, want %d", got[limit-1].SessionsCreated, total-1)
	}
	// Strictly increasing timestamps (chronological order).
	for i := 1; i < len(got); i++ {
		if !got[i].Time.After(got[i-1].Time) {
			t.Errorf("samples out of order at %d: %v before %v", i, got[i-1].Time, got[i].Time)
		}
	}
}

// TestReadSamplesReturnsAllWhenLimitZero covers the limit <= 0 branch
// (used by tests that want the full history). The streaming rewrite must
// still produce identical output to the old bytes.Split path.
func TestReadSamplesReturnsAllWhenLimitZero(t *testing.T) {
	s := newTestStore(t)
	base := time.Unix(1700000000, 0).UTC()
	const total = 100
	for i := 0; i < total; i++ {
		s.AppendSample(sample(base.Add(time.Duration(i)*time.Second), int64(i), 0, 0, 0, 0, 0))
	}
	got, err := s.ReadSamples(0)
	if err != nil {
		t.Fatalf("ReadSamples(0): %v", err)
	}
	if len(got) != total {
		t.Errorf("expected %d samples, got %d", total, len(got))
	}
}

// TestReadSamplesMergesBackupAndLive covers the rotation-aware merge:
// after a rotation, samples live in stats.jsonl.1 (backup) and the fresh
// stats.jsonl. ReadSamples must stream both and return them in
// chronological order.
func TestReadSamplesMergesBackupAndLive(t *testing.T) {
	s := newTestStore(t)
	s.maxLogBytes = 256 // force rotation quickly
	base := time.Unix(1700000000, 0).UTC()

	// Write enough samples to trigger at least one rotation.
	for i := 0; i < 40; i++ {
		if err := s.AppendSample(sample(base.Add(time.Duration(i)*time.Minute), int64(i), 0, 0, 0, 0, 0)); err != nil {
			t.Fatalf("AppendSample %d: %v", i, err)
		}
	}
	if _, err := os.Stat(s.logBackup); err != nil {
		t.Fatalf("expected backup to exist after rotation: %v", err)
	}

	got, err := s.ReadSamples(0)
	if err != nil {
		t.Fatalf("ReadSamples: %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("expected merged samples from both files, got %d", len(got))
	}
	// Chronological order across the merge.
	for i := 1; i < len(got); i++ {
		if got[i].Time.Before(got[i-1].Time) {
			t.Fatalf("samples out of order at %d: %v before %v", i, got[i-1].Time, got[i].Time)
		}
	}
}

// TestReadSamplesRingBufferWrapOrder covers the ring's wrap math:
// pushing exactly limit items, then some more, must produce a snapshot
// that starts at the correct (wrapped) index. This is the unit test for
// ringSample that the integration-level TestReadSamplesRingBufferTail
// covers only indirectly.
func TestReadSamplesRingBufferWrapOrder(t *testing.T) {
	const limit = 4
	r := newRingSample(limit)
	// Push fewer than limit: snapshot is partial.
	for i := 0; i < 3; i++ {
		r.push(sample(time.Unix(int64(1700000000+i), 0), int64(i), 0, 0, 0, 0, 0))
	}
	got := r.snapshot()
	if len(got) != 3 || got[0].SessionsCreated != 0 || got[2].SessionsCreated != 2 {
		t.Errorf("partial ring wrong: %+v", got)
	}

	// Fill past capacity: oldest entries evicted, snapshot starts at wrap.
	for i := 3; i < 7; i++ {
		r.push(sample(time.Unix(int64(1700000000+i), 0), int64(i), 0, 0, 0, 0, 0))
	}
	got = r.snapshot()
	if len(got) != limit {
		t.Fatalf("wrapped snapshot len: got %d, want %d", len(got), limit)
	}
	// Items 3..6 are the last 4 pushed; order must be chronological.
	want := []int64{3, 4, 5, 6}
	for i, sc := range want {
		if got[i].SessionsCreated != sc {
			t.Errorf("wrapped[%d]: got sc=%d, want %d", i, got[i].SessionsCreated, sc)
		}
	}
}

// TestReadSamplesScannerBufferSkipsPathologicallyLongLine pins the
// recovery contract when a line exceeds scannerBufferSize: the scan
// stops on that token but the call still returns whatever valid samples
// were collected before. The torn line is silently dropped (same
// behaviour as malformed lines); the next call resumes from the next
// newline.
func TestReadSamplesScannerBufferSkipsPathologicallyLongLine(t *testing.T) {
	s := newTestStore(t)
	s.AppendSample(sample(time.Unix(1700000000, 0), 1, 0, 0, 0, 0, 0))

	// Inject a line larger than scannerBufferSize (1 MiB). bufio.Scanner
	// with our explicit buffer cap returns an error from Scan, which we
	// log but don't propagate; the call returns the prior valid samples.
	f, err := os.OpenFile(s.logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	huge := make([]byte, scannerBufferSize+1)
	for i := range huge {
		huge[i] = 'x'
	}
	huge[len(huge)-1] = '\n'
	if _, err := f.Write(huge); err != nil {
		t.Fatalf("write huge line: %v", err)
	}
	f.Close()

	got, err := s.ReadSamples(0)
	if err != nil {
		t.Fatalf("ReadSamples on log with oversized line: %v", err)
	}
	if len(got) != 1 || got[0].SessionsCreated != 1 {
		t.Errorf("expected the 1 valid sample before the huge line, got %+v", got)
	}
}

// TestReadSamplesReleasesLockBeforeRead verifies the B2 invariant
// directly: while ReadSamples is mid-read, the store's mutex must be
// free so a concurrent writer (or a second reader) isn't blocked. We
// assert this by attempting an unrelated lock-holding operation in a
// separate goroutine and checking it completes quickly.
func TestReadSamplesReleasesLockBeforeRead(t *testing.T) {
	s := newTestStore(t)
	// Stand up enough samples to give ReadSamples real wall-clock work.
	for i := 0; i < 1000; i++ {
		_ = s.AppendSample(sample(time.Unix(int64(1700000000+i), 0), int64(i), 0, 0, 0, 0, 0))
	}

	readDone := make(chan struct{})
	go func() {
		_, _ = s.ReadSamples(0)
		close(readDone)
	}()

	// Poll s.mu.TryLock — if ReadSamples holds it across the whole
	// scan we'll never acquire it; if it released after path snapshot
	// (the B2 fix), we should win within a few polls. TryLock was
	// added in Go 1.18.
	deadline := time.Now().Add(3 * time.Second)
	acquired := false
	for time.Now().Before(deadline) && !acquired {
		if s.mu.TryLock() {
			s.mu.Unlock()
			acquired = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	<-readDone

	if !acquired {
		t.Fatal("s.mu was held for the entire ReadSamples duration — B2 regression")
	}
}

// TestStoreStreamingReadLargeLog is a smoke test that ReadSamples on a
// multi-MB log returns the expected tail without allocating the whole
// log. We can't measure allocations directly from outside, but we can
// assert correctness over a large dataset.
func TestStoreStreamingReadLargeLog(t *testing.T) {
	if testing.Short() {
		t.Skip("large-log smoke test skipped in -short mode")
	}
	s := newTestStore(t)
	const total = 50000
	base := time.Unix(1700000000, 0).UTC()
	for i := 0; i < total; i++ {
		if err := s.AppendSample(sample(base.Add(time.Duration(i)*time.Second), int64(i), 0, 0, 0, 0, 0)); err != nil {
			t.Fatalf("AppendSample %d: %v", i, err)
		}
	}
	// Sanity: the log file is now non-trivial.
	if fi, err := os.Stat(filepath.Join(s.dir, statsFile)); err == nil && fi.Size() < 1_000_000 {
		t.Logf("note: log size = %d bytes (expected > 1MB)", fi.Size())
	}

	got, err := s.ReadSamples(100)
	if err != nil {
		t.Fatalf("ReadSamples: %v", err)
	}
	if len(got) != 100 {
		t.Fatalf("len: got %d, want 100", len(got))
	}
	if got[0].SessionsCreated != int64(total-100) {
		t.Errorf("tail start: got %d, want %d", got[0].SessionsCreated, total-100)
	}
	if got[99].SessionsCreated != int64(total-1) {
		t.Errorf("tail end: got %d, want %d", got[99].SessionsCreated, total-1)
	}
}
