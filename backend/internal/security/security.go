package security

import (
	"context"
	"crypto/rand"
	"math/big"
	"sync"
	"time"
    "log/slog"
)

const (
	MaxFailedAttempts        = 3
	BaseBackoffMs            = 100
	MaxBackoffMs             = 30000
	MaxMessagesPerSecond     = 10
	MaxBurstMessages         = 20
	RateLimitCleanupInterval = 5 * time.Minute
	FailedAttemptWindow      = 10 * time.Minute
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
	failedJoins  map[string]*FailedJoinAttempt
	messageRates map[string]*MessageRateLimiter
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
}

func NewSecurity(parentCtx context.Context) *Security {
	ctx, cancel := context.WithCancel(parentCtx)
	s := &Security{
		failedJoins:  make(map[string]*FailedJoinAttempt),
		messageRates: make(map[string]*MessageRateLimiter),
		ctx:          ctx,
		cancel:       cancel,
	}
	go s.cleanupLoop()
	return s
}

func (s *Security) Shutdown() {
	s.cancel()
}

func (s *Security) CheckJoinRateLimit(ip string) (bool, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	attempt, exists := s.failedJoins[ip]

	if !exists {
		s.failedJoins[ip] = &FailedJoinAttempt{
			Count:       0,
			LastAttempt: now,
		}
		return true, 0
	}

	if now.Before(attempt.LastBackoffUntil) {
		backoffMs := int(attempt.LastBackoffUntil.Sub(now).Milliseconds())
		return false, backoffMs
	}

	if now.Sub(attempt.LastAttempt) > FailedAttemptWindow {
		attempt.Count = 0
	}

	return true, 0
}

func (s *Security) RecordFailedJoin(ip string) int {
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
		attempt.LastBackoffUntil = now.Add(time.Duration(backoffMs) * time.Millisecond)
		return backoffMs
	}

	return 0
}

func (s *Security) ClearFailedJoin(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failedJoins, ip)
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
}

func GenerateID() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	charsetLen := big.NewInt(int64(len(charset)))

	for i := range b {
		n, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
            slog.Error("Error generating random ID", "error", err)
			return generateTimestampID()
		}
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

func generateTimestampID() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	nano := time.Now().UnixNano()
	b := make([]byte, 12)
	for i := range b {
		b[i] = charset[(i+int(nano))%len(charset)]
		nano = nano >> 4
	}
	return string(b)
}
