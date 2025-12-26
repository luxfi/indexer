// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package account

import (
	"context"
	"sync"
	"time"
)

// RateLimiter provides API key rate limiting
type RateLimiter struct {
	mu           sync.RWMutex
	limits       map[string]*keyLimit
	defaultLimit *Limit
	cleanupTick  time.Duration
}

// Limit defines rate limit parameters
type Limit struct {
	RequestsPerSecond int64
	RequestsPerDay    int64
	BurstSize         int64
}

// keyLimit tracks usage for a single API key
type keyLimit struct {
	mu              sync.Mutex
	limit           *Limit
	secondTokens    float64
	dayTokens       float64
	lastSecondReset time.Time
	lastDayReset    time.Time
}

// DefaultLimit returns sensible rate limit defaults
func DefaultLimit() *Limit {
	return &Limit{
		RequestsPerSecond: 10,
		RequestsPerDay:    100000,
		BurstSize:         20,
	}
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(defaultLimit *Limit) *RateLimiter {
	if defaultLimit == nil {
		defaultLimit = DefaultLimit()
	}

	rl := &RateLimiter{
		limits:       make(map[string]*keyLimit),
		defaultLimit: defaultLimit,
		cleanupTick:  time.Minute,
	}

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// Allow checks if a request should be allowed for the given API key
func (r *RateLimiter) Allow(apiKey string) bool {
	r.mu.RLock()
	kl, exists := r.limits[apiKey]
	r.mu.RUnlock()

	if !exists {
		r.mu.Lock()
		kl = &keyLimit{
			limit:           r.defaultLimit,
			secondTokens:    float64(r.defaultLimit.BurstSize),
			dayTokens:       float64(r.defaultLimit.RequestsPerDay),
			lastSecondReset: time.Now(),
			lastDayReset:    time.Now(),
		}
		r.limits[apiKey] = kl
		r.mu.Unlock()
	}

	return kl.allow()
}

// SetKeyLimit sets a custom limit for an API key
func (r *RateLimiter) SetKeyLimit(apiKey string, limit *Limit) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if kl, exists := r.limits[apiKey]; exists {
		kl.mu.Lock()
		kl.limit = limit
		kl.mu.Unlock()
	} else {
		r.limits[apiKey] = &keyLimit{
			limit:           limit,
			secondTokens:    float64(limit.BurstSize),
			dayTokens:       float64(limit.RequestsPerDay),
			lastSecondReset: time.Now(),
			lastDayReset:    time.Now(),
		}
	}
}

// GetUsage returns current usage statistics for an API key
func (r *RateLimiter) GetUsage(apiKey string) (secondsRemaining, dayRemaining int64) {
	r.mu.RLock()
	kl, exists := r.limits[apiKey]
	r.mu.RUnlock()

	if !exists {
		return r.defaultLimit.RequestsPerSecond, r.defaultLimit.RequestsPerDay
	}

	kl.mu.Lock()
	defer kl.mu.Unlock()

	return int64(kl.secondTokens), int64(kl.dayTokens)
}

// RemoveKey removes rate limit tracking for an API key
func (r *RateLimiter) RemoveKey(apiKey string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.limits, apiKey)
}

// allow checks and updates tokens for a single request
func (kl *keyLimit) allow() bool {
	kl.mu.Lock()
	defer kl.mu.Unlock()

	now := time.Now()

	// Refill second tokens
	secondsElapsed := now.Sub(kl.lastSecondReset).Seconds()
	if secondsElapsed >= 1.0 {
		kl.secondTokens = float64(kl.limit.BurstSize)
		kl.lastSecondReset = now
	} else {
		// Gradual refill
		refill := secondsElapsed * float64(kl.limit.RequestsPerSecond)
		kl.secondTokens = min(kl.secondTokens+refill, float64(kl.limit.BurstSize))
	}

	// Check daily reset
	if now.Sub(kl.lastDayReset) >= 24*time.Hour {
		kl.dayTokens = float64(kl.limit.RequestsPerDay)
		kl.lastDayReset = now
	}

	// Check if request is allowed
	if kl.secondTokens < 1 || kl.dayTokens < 1 {
		return false
	}

	kl.secondTokens--
	kl.dayTokens--
	return true
}

// cleanupLoop periodically removes stale entries
func (r *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(r.cleanupTick)
	for range ticker.C {
		r.cleanup()
	}
}

// cleanup removes entries that haven't been used recently
func (r *RateLimiter) cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	staleThreshold := time.Now().Add(-24 * time.Hour)

	for key, kl := range r.limits {
		kl.mu.Lock()
		if kl.lastSecondReset.Before(staleThreshold) && kl.lastDayReset.Before(staleThreshold) {
			delete(r.limits, key)
		}
		kl.mu.Unlock()
	}
}

// RateLimitMiddleware provides HTTP middleware for rate limiting
type RateLimitMiddleware struct {
	limiter        *RateLimiter
	accountService *Service
}

// NewRateLimitMiddleware creates a new rate limit middleware
func NewRateLimitMiddleware(limiter *RateLimiter, accountService *Service) *RateLimitMiddleware {
	return &RateLimitMiddleware{
		limiter:        limiter,
		accountService: accountService,
	}
}

// CheckRateLimit checks if a request should be rate limited
func (m *RateLimitMiddleware) CheckRateLimit(ctx context.Context, apiKey string) error {
	// Validate API key exists
	key, err := m.accountService.GetAPIKey(ctx, apiKey)
	if err != nil {
		return ErrInvalidAPIKey
	}

	// Check rate limit
	if !m.limiter.Allow(apiKey) {
		return ErrRateLimited
	}

	// Update usage stats
	m.accountService.UpdateAPIKeyUsage(ctx, key.Value)

	return nil
}

// GetRemainingRequests returns remaining request quota
func (m *RateLimitMiddleware) GetRemainingRequests(apiKey string) (perSecond, perDay int64) {
	return m.limiter.GetUsage(apiKey)
}

// min returns the smaller of two float64 values
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
