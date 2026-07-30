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

// TestGenerateIDPanicsOnCSPRNGFailure pins the B14 fail-closed contract.
// The previous implementation fell back to generateTimestampID on a
// rand.Read error, which silently degraded uniformity and (for
// GenerateToken) collapsed S1/S12 security. We now panic. The seam
// (randRead) is restored in a defer so a t.Fatal in one case doesn't
// poison the package-level variable for the rest of the suite.
func TestGenerateIDPanicsOnCSPRNGFailure(t *testing.T) {
	orig := randRead
	randRead = func(b []byte) (int, error) {
		return 0, errTestCSPRNG
	}
	defer func() { randRead = orig }()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("GenerateID should panic on CSPRNG failure")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "client ID") {
			t.Fatalf("panic should name the context, got: %v", r)
		}
	}()
	_ = GenerateID()
}

// TestGenerateTokenPanicsOnCSPRNGFailure pins the B14 fail-closed
// contract for security-critical tokens. The previous time-derived
// fallback was predictable within time.Now().UnixNano() resolution and
// silently enabled token forgery when /dev/urandom was unavailable.
func TestGenerateTokenPanicsOnCSPRNGFailure(t *testing.T) {
	orig := randRead
	randRead = func(b []byte) (int, error) {
		return 0, errTestCSPRNG
	}
	defer func() { randRead = orig }()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("GenerateToken should panic on CSPRNG failure")
		}
	}()
	_ = GenerateToken()
}

// TestGenerateTokenProducesURLEncodedSecret asserts the happy path still
// produces a base64url token of the documented entropy budget. A
// regression that changed the byte length would silently shrink the
// guess-resistance of every trainer/reclaim token.
func TestGenerateTokenProducesURLEncodedSecret(t *testing.T) {
	const n = 100
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		tok := GenerateToken()
		// base64url of 32 bytes = 43 chars (no padding). Reject any
		// other length or non-url-safe character.
		if len(tok) != 43 {
			t.Fatalf("token length: got %d, want 43 (32 bytes base64url)", len(tok))
		}
		for _, r := range tok {
			const urlsafe = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
			if !strings.ContainsRune(urlsafe, r) {
				t.Fatalf("token %q contains non-base64url char %q", tok, r)
			}
		}
		seen[tok] = struct{}{}
	}
	if len(seen) != n {
		t.Errorf("expected %d unique tokens, got %d (RNG may be broken)", n, len(seen))
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

// BenchmarkIDParallel exercises the path under contention (many
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
