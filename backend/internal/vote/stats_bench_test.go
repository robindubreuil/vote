package vote

import (
	"sync"
	"testing"
)

// BenchmarkCounterIncContended measures the throughput of Counter.Inc under
// heavy contention. The atomic.Int64 backing (B1) should outperform the
// previous sync.RWMutex implementation by an order of magnitude on multi-
// core machines; this benchmark exists to spot a regression. Run with
// `go test -bench=. -benchmem -cpu=1,2,4,8`.
func BenchmarkCounterIncContended(b *testing.B) {
	var c Counter
	var wg sync.WaitGroup
	perG := b.N
	// Parallels each get their own goroutine hammering the same counter.
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				c.Inc()
			}
		}()
	}
	wg.Wait()
	if got := c.Value(); got != int64(b.N*perG) {
		b.Fatalf("counter drift: got %d, want %d", got, b.N*perG)
	}
}

// BenchmarkCounterReadHeavy exercises Value() under heavy concurrent reads
// with a background of writers. The atomic Int64 read path is wait-free;
// the previous RWMutex implementation serialised readers against writers.
func BenchmarkCounterReadHeavy(b *testing.B) {
	var c Counter
	stop := make(chan struct{})
	// Background writer keeps the counter "live".
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				c.Inc()
			}
		}
	}()
	b.ResetTimer()
	var sink int64
	for i := 0; i < b.N; i++ {
		sink = c.Value()
	}
	b.StopTimer()
	close(stop)
	_ = sink
}
