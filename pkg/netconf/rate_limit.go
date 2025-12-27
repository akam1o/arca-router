package netconf

import (
	"fmt"
	"sync"
	"time"
)

// RateLimiter tracks authentication failures and enforces lockouts
type RateLimiter struct {
	config *SSHConfig

	// IP-based tracking
	ipFailures map[string]*failureTracker
	ipMu       sync.RWMutex

	// User-based tracking
	userFailures map[string]*failureTracker
	userMu       sync.RWMutex

	// Cleanup ticker
	ticker *time.Ticker
	done   chan struct{}
}

// failureTracker tracks failures for an IP or user
type failureTracker struct {
	failures  []time.Time // Timestamps of recent failures
	lockedOut bool        // Whether currently locked out
	lockoutAt time.Time   // When lockout started
}

// NewRateLimiter creates a new rate limiter with the given configuration
func NewRateLimiter(config *SSHConfig) *RateLimiter {
	rl := &RateLimiter{
		config:       config,
		ipFailures:   make(map[string]*failureTracker),
		userFailures: make(map[string]*failureTracker),
		done:         make(chan struct{}),
	}

	// Start cleanup goroutine (runs every minute)
	rl.ticker = time.NewTicker(time.Minute)
	go rl.cleanupLoop()

	return rl
}

// CheckIP checks if an IP address is currently locked out
// Returns true if allowed, false if locked out
func (rl *RateLimiter) CheckIP(ip string) (bool, time.Time) {
	rl.ipMu.RLock()
	defer rl.ipMu.RUnlock()

	tracker, exists := rl.ipFailures[ip]
	if !exists {
		return true, time.Time{}
	}

	// Check if lockout has expired
	if tracker.lockedOut {
		if time.Since(tracker.lockoutAt) < rl.config.LockoutDuration {
			// Still locked out
			unlockAt := tracker.lockoutAt.Add(rl.config.LockoutDuration)
			return false, unlockAt
		}
		// Lockout expired - will be cleaned up in next iteration
	}

	return true, time.Time{}
}

// CheckUser checks if a user is currently locked out
// Returns true if allowed, false if locked out
func (rl *RateLimiter) CheckUser(username string) (bool, time.Time) {
	rl.userMu.RLock()
	defer rl.userMu.RUnlock()

	tracker, exists := rl.userFailures[username]
	if !exists {
		return true, time.Time{}
	}

	// Check if lockout has expired
	if tracker.lockedOut {
		if time.Since(tracker.lockoutAt) < rl.config.LockoutDuration {
			// Still locked out
			unlockAt := tracker.lockoutAt.Add(rl.config.LockoutDuration)
			return false, unlockAt
		}
		// Lockout expired - will be cleaned up in next iteration
	}

	return true, time.Time{}
}

// RecordFailure records an authentication failure for an IP and user
// Returns true if the IP or user should be locked out
func (rl *RateLimiter) RecordFailure(ip, username string) (ipLocked, userLocked bool) {
	now := time.Now()

	// Record IP failure
	rl.ipMu.Lock()
	ipTracker := rl.getOrCreateTracker(rl.ipFailures, ip)
	ipTracker.failures = append(ipTracker.failures, now)
	ipTracker.failures = rl.removeExpiredFailures(ipTracker.failures, rl.config.IPLockoutWindow)

	if len(ipTracker.failures) >= rl.config.IPFailureLimit && !ipTracker.lockedOut {
		ipTracker.lockedOut = true
		ipTracker.lockoutAt = now
		ipLocked = true
	}
	rl.ipMu.Unlock()

	// Record user failure
	rl.userMu.Lock()
	userTracker := rl.getOrCreateTracker(rl.userFailures, username)
	userTracker.failures = append(userTracker.failures, now)
	userTracker.failures = rl.removeExpiredFailures(userTracker.failures, rl.config.UserLockoutWindow)

	if len(userTracker.failures) >= rl.config.UserFailureLimit && !userTracker.lockedOut {
		userTracker.lockedOut = true
		userTracker.lockoutAt = now
		userLocked = true
	}
	rl.userMu.Unlock()

	return ipLocked, userLocked
}

// RecordSuccess records a successful authentication (clears failure history)
func (rl *RateLimiter) RecordSuccess(ip, username string) {
	rl.ipMu.Lock()
	delete(rl.ipFailures, ip)
	rl.ipMu.Unlock()

	rl.userMu.Lock()
	delete(rl.userFailures, username)
	rl.userMu.Unlock()
}

// getOrCreateTracker gets or creates a failure tracker
// Must be called with lock held
func (rl *RateLimiter) getOrCreateTracker(m map[string]*failureTracker, key string) *failureTracker {
	tracker, exists := m[key]
	if !exists {
		tracker = &failureTracker{
			failures: make([]time.Time, 0),
		}
		m[key] = tracker
	}
	return tracker
}

// removeExpiredFailures removes failures outside the tracking window
func (rl *RateLimiter) removeExpiredFailures(failures []time.Time, window time.Duration) []time.Time {
	now := time.Now()
	cutoff := now.Add(-window)

	// Find first non-expired failure
	start := 0
	for start < len(failures) && failures[start].Before(cutoff) {
		start++
	}

	// Return slice of non-expired failures
	if start > 0 {
		return failures[start:]
	}
	return failures
}

// cleanupLoop periodically cleans up expired lockouts
func (rl *RateLimiter) cleanupLoop() {
	for {
		select {
		case <-rl.ticker.C:
			rl.cleanup()
		case <-rl.done:
			rl.ticker.Stop()
			return
		}
	}
}

// cleanup removes expired lockouts and old failure records
func (rl *RateLimiter) cleanup() {
	// Cleanup IP lockouts
	rl.ipMu.Lock()
	for ip, tracker := range rl.ipFailures {
		// Remove expired lockouts
		if tracker.lockedOut && time.Since(tracker.lockoutAt) >= rl.config.LockoutDuration {
			delete(rl.ipFailures, ip)
			continue
		}

		// Remove old failure records
		tracker.failures = rl.removeExpiredFailures(tracker.failures, rl.config.IPLockoutWindow)
		if len(tracker.failures) == 0 && !tracker.lockedOut {
			delete(rl.ipFailures, ip)
		}
	}
	rl.ipMu.Unlock()

	// Cleanup user lockouts
	rl.userMu.Lock()
	for username, tracker := range rl.userFailures {
		// Remove expired lockouts
		if tracker.lockedOut && time.Since(tracker.lockoutAt) >= rl.config.LockoutDuration {
			delete(rl.userFailures, username)
			continue
		}

		// Remove old failure records
		tracker.failures = rl.removeExpiredFailures(tracker.failures, rl.config.UserLockoutWindow)
		if len(tracker.failures) == 0 && !tracker.lockedOut {
			delete(rl.userFailures, username)
		}
	}
	rl.userMu.Unlock()
}

// Stop stops the rate limiter cleanup goroutine
func (rl *RateLimiter) Stop() {
	close(rl.done)
}

// GetStats returns current rate limiter statistics
func (rl *RateLimiter) GetStats() RateLimiterStats {
	rl.ipMu.RLock()
	ipLockedCount := 0
	ipTrackedCount := len(rl.ipFailures)
	for _, tracker := range rl.ipFailures {
		if tracker.lockedOut {
			ipLockedCount++
		}
	}
	rl.ipMu.RUnlock()

	rl.userMu.RLock()
	userLockedCount := 0
	userTrackedCount := len(rl.userFailures)
	for _, tracker := range rl.userFailures {
		if tracker.lockedOut {
			userLockedCount++
		}
	}
	rl.userMu.RUnlock()

	return RateLimiterStats{
		IPsTracked:    ipTrackedCount,
		IPsLocked:     ipLockedCount,
		UsersTracked:  userTrackedCount,
		UsersLocked:   userLockedCount,
		LockoutWindow: rl.config.LockoutDuration,
	}
}

// RateLimiterStats contains rate limiter statistics
type RateLimiterStats struct {
	IPsTracked     int
	IPsLocked      int
	UsersTracked   int
	UsersLocked    int
	LockoutWindow  time.Duration
}

// FormatLockoutError returns a user-friendly lockout error message
func FormatLockoutError(unlockAt time.Time) error {
	remaining := time.Until(unlockAt).Round(time.Second)
	if remaining < 0 {
		remaining = 0
	}
	return fmt.Errorf("account locked out, try again in %v", remaining)
}
