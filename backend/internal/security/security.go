package security

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"time"
)

const (
	MaxFailedAttempts        = 3
	BaseBackoffMs            = 1000   // 1 second base backoff
	MaxBackoffMs             = 300000 // 5 minutes max backoff
	BackoffJitter            = 0.25   // ±25% jitter to prevent timing attacks
	MaxMessagesPerSecond     = 10
	MaxBurstMessages         = 20
	RateLimitCleanupInterval = 5 * time.Minute
	FailedAttemptWindow      = 10 * time.Minute

	// Per-IP session creation limit. Generous enough for a building with
	// multiple trainers running concurrent classes, tight enough to block
	// mass-creation floods. Sliding window — old entries age out.
	MaxSessionCreationsPerHour = 20
	SessionCreationWindow      = time.Hour

	clientIDCharset = "abcdefghijklmnopqrstuvwxyz0123456789"
	clientIDLength  = 12

	// trainerTokenBytes is the entropy budget for per-session trainer tokens.
	// 32 bytes (256 bits) is the same as a CSRF token; base64url encodes it
	// to ~43 chars. The token is never shown to stagiaires (the QR carries
	// only the public session code), so guess-resistance far exceeds the
	// code space.
	trainerTokenBytes = 32
)

type FailedJoinAttempt struct {
	Count            int
	LastAttempt      time.Time
	LastBackoffUntil time.Time
}

type MessageRateLimiter struct {
	lastMessage  time.Time
	messageCount int
	windowStart  time.Time
}

type Security struct {
	failedJoins         map[string]*FailedJoinAttempt
	messageRates        map[string]*MessageRateLimiter
	sessionCreations    map[string][]time.Time
	maxSessionCreations int
	mu                  sync.RWMutex
	ctx                 context.Context
	cancel              context.CancelFunc
}

func NewSecurity(parentCtx context.Context, maxSessionCreations int) *Security {
	if maxSessionCreations <= 0 {
		maxSessionCreations = MaxSessionCreationsPerHour
	}
	ctx, cancel := context.WithCancel(parentCtx)
	s := &Security{
		failedJoins:         make(map[string]*FailedJoinAttempt),
		messageRates:        make(map[string]*MessageRateLimiter),
		sessionCreations:    make(map[string][]time.Time),
		maxSessionCreations: maxSessionCreations,
		ctx:                 ctx,
		cancel:              cancel,
	}
	go s.cleanupLoop()
	return s
}

func (s *Security) Shutdown() {
	s.cancel()
}

func (s *Security) CheckJoinRateLimit(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	attempt, exists := s.failedJoins[ip]

	if !exists {
		s.failedJoins[ip] = &FailedJoinAttempt{
			Count:       0,
			LastAttempt: now,
		}
		return true
	}

	if now.Before(attempt.LastBackoffUntil) {
		return false
	}

	if now.Sub(attempt.LastAttempt) > FailedAttemptWindow {
		attempt.Count = 0
	}

	return true
}

func (s *Security) RecordFailedJoin(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	attempt := s.failedJoins[ip]

	if attempt == nil {
		attempt = &FailedJoinAttempt{
			Count:       0,
			LastAttempt: now,
		}
		s.failedJoins[ip] = attempt
	}

	attempt.Count++
	attempt.LastAttempt = now

	if attempt.Count >= MaxFailedAttempts {
		backoffExponent := attempt.Count - MaxFailedAttempts
		// Cap the exponent to prevent int64 overflow around Count=56,
		// which would wrap negative and bypass the MaxBackoffMs ceiling.
		const maxBackoffExponent = 30
		var backoffMs int
		if backoffExponent > maxBackoffExponent {
			backoffMs = MaxBackoffMs
		} else {
			backoffMs = BaseBackoffMs * (1 << backoffExponent)
			if backoffMs > MaxBackoffMs {
				backoffMs = MaxBackoffMs
			}
		}
		// Add jitter to prevent timing attacks: ±25% randomization
		jitterRange := int(float64(backoffMs) * BackoffJitter)
		jitterOffset, err := rand.Int(rand.Reader, big.NewInt(int64(jitterRange*2+1)))
		if err == nil {
			jitter := int(jitterOffset.Int64()) - jitterRange
			backoffMs += jitter
		}
		// Ensure backoff is at least 100ms and not negative
		if backoffMs < 100 {
			backoffMs = 100
		}
		attempt.LastBackoffUntil = now.Add(time.Duration(backoffMs) * time.Millisecond)
	}
}

func (s *Security) ClearFailedJoin(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failedJoins, ip)
}

// CheckSessionCreateRate reports whether the given IP may create a new
// session right now. Uses a sliding window of recent creation timestamps.
// Does NOT record the creation — call RecordSessionCreation once the
// session is actually created so that aborted attempts don't count
// against the limit.
func (s *Security) CheckSessionCreateRate(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-SessionCreationWindow)
	stamps := s.sessionCreations[ip]

	// Drop stale entries in-place.
	keep := stamps[:0]
	for _, t := range stamps {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	s.sessionCreations[ip] = keep

	return len(keep) < s.maxSessionCreations
}

// RecordSessionCreation notes a successful creation for rate-limiting
// purposes. Calling this without a prior CheckSessionCreateRate is allowed
// (the check will fail on the next call).
func (s *Security) RecordSessionCreation(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-SessionCreationWindow)
	stamps := s.sessionCreations[ip]

	keep := stamps[:0]
	for _, t := range stamps {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	s.sessionCreations[ip] = append(keep, now)
}

// RemoveSessionCreation removes the most recent creation stamp for an IP.
// Used to roll back a recorded creation when registration later fails, so
// the trainer's quota isn't consumed by a transient server error.
func (s *Security) RemoveSessionCreation(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stamps := s.sessionCreations[ip]
	if len(stamps) == 0 {
		return
	}
	// Drop the newest entry.
	s.sessionCreations[ip] = stamps[:len(stamps)-1]
}

// CountSessionCreations returns the number of creation timestamps currently
// recorded for the IP within the sliding window. Test-only helper.
func (s *Security) CountSessionCreations(ip string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cutoff := time.Now().Add(-SessionCreationWindow)
	n := 0
	for _, t := range s.sessionCreations[ip] {
		if t.After(cutoff) {
			n++
		}
	}
	return n
}

// FailedJoinCount returns the number of failed-join attempts currently
// recorded for the IP within the sliding window. Test-only helper used to
// verify that the "Session introuvable" branch of trainer_join records its
// failures against the per-IP backoff (S2).
func (s *Security) FailedJoinCount(ip string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.failedJoins[ip]
	if !ok {
		return 0
	}
	if time.Since(a.LastAttempt) > FailedAttemptWindow {
		return 0
	}
	return a.Count
}

func (s *Security) CheckMessageRate(clientID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	limiter, exists := s.messageRates[clientID]

	if !exists {
		s.messageRates[clientID] = &MessageRateLimiter{
			lastMessage:  now,
			messageCount: 1,
			windowStart:  now,
		}
		return true
	}

	if now.Sub(limiter.windowStart) >= time.Second {
		limiter.windowStart = now
		limiter.messageCount = 1
		limiter.lastMessage = now
		return true
	}

	if limiter.messageCount >= MaxBurstMessages {
		return false
	}

	if now.Sub(limiter.lastMessage) < time.Second/MaxMessagesPerSecond && limiter.messageCount >= MaxMessagesPerSecond {
		return false
	}

	limiter.messageCount++
	limiter.lastMessage = now
	return true
}

func (s *Security) RemoveMessageRate(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.messageRates, clientID)
}

func (s *Security) cleanupLoop() {
	ticker := time.NewTicker(RateLimitCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanup()
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Security) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-SessionCreationWindow)

	for ip, attempt := range s.failedJoins {
		if now.Sub(attempt.LastAttempt) > FailedAttemptWindow && now.After(attempt.LastBackoffUntil) {
			delete(s.failedJoins, ip)
		}
	}

	for clientID, limiter := range s.messageRates {
		if now.Sub(limiter.lastMessage) > time.Minute {
			delete(s.messageRates, clientID)
		}
	}

	for ip, stamps := range s.sessionCreations {
		keep := stamps[:0]
		for _, t := range stamps {
			if t.After(cutoff) {
				keep = append(keep, t)
			}
		}
		if len(keep) == 0 {
			delete(s.sessionCreations, ip)
		} else {
			s.sessionCreations[ip] = keep
		}
	}
}

// randRead is a package-level seam over crypto/rand.Read so the
// CSPRNG-failure paths (which now panic, B14) can be exercised in tests
// without provoking a real kernel-entropy fault. Production code never
// reassigns it; the only writer besides tests is the init below.
var randRead = rand.Read

// failCSPRNG panics with a distinct, greppable message when the kernel
// CSPRNG is unavailable. B14: the previous GenerateID/GenerateToken
// implementations fell back to a time-derived value when /dev/urandom
// was unavailable, which silently collapsed S1 (trainer-takeover token)
// and S6/S12 (stagiaire reclaim token) — an attacker who could predict
// the fallback could forge either token. A server that cannot draw
// secrets from the kernel CSPRNG is in a degenerate state that should
// not be papered over; fail loud so the operator sees it immediately
// rather than serving with forgeable tokens. On every supported target
// (Linux, macOS, Windows, BSD) /dev/urandom blocks only briefly at
// early boot and otherwise never fails, so this path is effectively
// unreachable in practice.
func failCSPRNG(context string, err error) {
	slog.Error("CSPRNG unavailable, refusing to serve with predictable secrets",
		"context", context, "error", err)
	panic(fmt.Sprintf("security: CSPRNG read failed for %s: %v", context, err))
}

// GenerateID returns a clientIDLength-character identifier drawn uniformly
// from clientIDCharset. The charset length (36) does not divide 256, so a
// naive byte%36 mapping introduces a tiny modulo bias toward the first
// (256 mod 36) = 4 characters. We use rejection sampling: each byte is
// drawn from the unbiased range [0, charsetBiasBound) where
// charsetBiasBound = 256 - (256 mod len(charset)) = 252. Any byte ≥
// charsetBiasBound is rejected and we keep consuming from a single
// batched rand.Read; if the batch is exhausted (extremely unlikely —
// expected rejects ≈ 1.6% per byte, so 12 chars need ≈ 12.2 bytes on
// average) we top up with one more read. IDs are public, non-secret
// correlation tokens; uniformity matters only enough to keep the
// collision space at the documented 36^12 ≈ 4.7e18.
//
// B4: the previous implementation called rand.Int(rand.Reader, big.NewInt(36))
// once per character — 12 syscalls + 12 big.Int allocations on every
// WebSocket connection. rand.Read batches those into ~1 syscall.
//
// B14: a CSPRNG read failure now panics rather than falling back to a
// predictable timestamp-derived ID. /dev/urandom failures are
// effectively unreachable on supported targets; when they do happen the
// process is in a state where serving — even non-secret IDs — would
// hide a serious kernel-level fault.
func GenerateID() string {
	// len(clientIDCharset) is 36 here; we spell the numeric invariants
	// out as literals so the const declarations stay untyped and the
	// byte comparison below compiles (constants declared inside a
	// function body via len(const-string) are typed int, which would
	// force every comparison back through int(b)).
	const charsetLen = 36
	// Largest multiple of charsetLen that fits in a byte — bytes below
	// this bound map uniformly onto charset indices via %charsetLen.
	// 256 - 256%36 = 252.
	const charsetBiasBound byte = 256 - 256%charsetLen

	// Read a few extra bytes up front to cover expected rejections so
	// the common path is a single syscall (12 chars, ~1.6% reject rate
	// per char → ~12.2 bytes needed → 16 is comfortable headroom).
	buf := make([]byte, clientIDLength+4)
	if _, err := randRead(buf); err != nil {
		failCSPRNG("client ID", err)
	}

	out := make([]byte, clientIDLength)
	bi := 0
	for i := 0; i < clientIDLength; i++ {
		for {
			if bi >= len(buf) {
				// Batched buffer exhausted (vanishingly rare). Top up
				// one byte at a time until we draw an unbiased value.
				topup := make([]byte, 1)
				if _, err := randRead(topup); err != nil {
					failCSPRNG("client ID topup", err)
				}
				buf = append(buf, topup[0])
			}
			b := buf[bi]
			bi++
			if b < charsetBiasBound {
				out[i] = clientIDCharset[int(b)%charsetLen]
				break
			}
		}
	}
	return string(out)
}

// GenerateToken returns a base64url-encoded cryptographically-random token.
// Used for per-session trainer tokens that gate takeover of an active
// trainer connection (S1) and for per-stagiaire reclaim tokens (S6/S12).
//
// B14: the previous implementation fell back to a time-derived value
// when the CSPRNG was unavailable. That fallback was predictable within
// the resolution of time.Now().UnixNano(), so an attacker who observed
// one token (the trainer token is sent in the session_created payload)
// could forge the other and bypass the takeover gate entirely. We now
// panic on CSPRNG failure — a server that cannot draw secrets from the
// kernel should not serve.
func GenerateToken() string {
	b := make([]byte, trainerTokenBytes)
	if _, err := randRead(b); err != nil {
		failCSPRNG("trainer token", err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
