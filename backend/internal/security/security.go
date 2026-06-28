package security

import (
	"context"
	"crypto/rand"
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
		backoffMs := BaseBackoffMs * (1 << backoffExponent)
		if backoffMs > MaxBackoffMs {
			backoffMs = MaxBackoffMs
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

func GenerateID() string {
	b := make([]byte, clientIDLength)
	charsetLen := big.NewInt(int64(len(clientIDCharset)))

	for i := range b {
		n, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			slog.Error("Error generating random ID", "error", err)
			return generateTimestampID()
		}
		b[i] = clientIDCharset[n.Int64()]
	}
	return string(b)
}

func generateTimestampID() string {
	nano := time.Now().UnixNano()
	b := make([]byte, clientIDLength)
	for i := range b {
		b[i] = clientIDCharset[(i+int(nano))%len(clientIDCharset)]
		nano >>= 4
	}
	return string(b)
}
