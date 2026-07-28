package security

import (
	"strings"
	"testing"
)

// TestGenerateIDProducesValidChars is the B4 happy-path regression: the
// batched rand.Read rewrite must still emit IDs of the documented length,
// drawing only from clientIDCharset. A regression here (e.g. mis-sized
// buffer, wrong modulo bound) would either shorten IDs or introduce
// out-of-charset bytes that downstream validation rejects.
func TestGenerateIDProducesValidChars(t *testing.T) {
	const n = 1000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := GenerateID()
		if len(id) != clientIDLength {
			t.Fatalf("ID length: got %d, want %d (id=%q)", len(id), clientIDLength, id)
		}
		for _, r := range id {
			if !strings.ContainsRune(clientIDCharset, r) {
				t.Fatalf("ID %q contains byte %q not in charset", id, r)
			}
		}
		seen[id] = struct{}{}
	}
	// With 36^12 ≈ 4.7e18 space and 1000 draws, collisions are
	// astronomically unlikely (birthday bound ~ 1e-13). Any collision
	// here indicates a broken RNG path or a fixed-seed fallback.
	if len(seen) != n {
		t.Errorf("expected %d unique IDs, got %d (RNG may be broken)", n, len(seen))
	}
}

// TestGenerateIDCharsetUniformity pins the rejection-sampling contract:
// each byte drawn is biased only by the [0, charsetBiasBound) range, so
// every charset index should appear with roughly equal frequency over a
// large sample. A regression in the modulo bound would skew the
// distribution visibly (the original naive byte%36 mapping biased the
// first 4 chars by ~1.6%; a broken bound could be much worse).
//
// We use a generous tolerance because chi-square rigour isn't the point —
// the test exists to catch gross regressions like "always returns the
// same character" or "off-by-one drops half the charset".
func TestGenerateIDCharsetUniformity(t *testing.T) {
	const samples = 60000 // 5000 IDs × 12 chars
	const buckets = len(clientIDCharset)
	counts := make([]int, buckets)
	for i := 0; i < samples/clientIDLength; i++ {
		for _, r := range GenerateID() {
			idx := strings.IndexRune(clientIDCharset, r)
			if idx < 0 {
				t.Fatalf("charset index lookup failed for %q", r)
			}
			counts[idx]++
		}
	}
	expected := float64(samples) / float64(buckets)
	lo := int(expected * 0.85) // ±15% tolerance
	hi := int(expected * 1.15)
	for i, c := range counts {
		if c < lo || c > hi {
			t.Errorf("charset[%d]=%q count %d outside [%d,%d] (expected ~%.0f)",
				i, clientIDCharset[i], c, lo, hi, expected)
		}
	}
}

// TestGenerateIDRecoverableFromEntropyFailure pins the B4 fallback: if
// the system CSPRNG returns an error, GenerateID falls back to
// generateTimestampID rather than returning a zero-value or panicking.
// We can't easily force crypto/rand.Read to fail without an injection
// seam, so we cover the equivalent fallback by exercising
// generateTimestampID directly — the contract is that GenerateID returns
// it verbatim on rand failure.
func TestGenerateIDFallbackProducesValidID(t *testing.T) {
	id := generateTimestampID()
	if len(id) != clientIDLength {
		t.Fatalf("fallback ID length: got %d, want %d", len(id), clientIDLength)
	}
	for _, r := range id {
		if !strings.ContainsRune(clientIDCharset, r) {
			t.Fatalf("fallback ID %q has out-of-charset byte %q", id, r)
		}
	}
}

// BenchmarkGenerateID compares the B4 batched rand.Read against the prior
// 12-call-per-ID implementation. The win is one syscall per ID instead
// of twelve, plus the elimination of 12 big.Int allocations per call.
// Run with: go test -bench=GenerateID -benchmem ./internal/security/
func BenchmarkGenerateID(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = GenerateID()
	}
}

// BenchmarkGenerateIDParallel exercises the path under contention (many
// goroutines hitting crypto/rand concurrently, as a reconnect storm
// would). The batched read also reduces lock contention on the kernel
// CSPRNG.
func BenchmarkGenerateIDParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = GenerateID()
		}
	})
}
