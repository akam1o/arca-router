package netconf

import (
	"testing"
	"time"
)

func TestRateLimiterIPLockout(t *testing.T) {
	config := &SSHConfig{
		IPFailureLimit:  3,
		IPLockoutWindow: 5 * time.Minute,
		LockoutDuration: 15 * time.Minute,
	}

	rl := NewRateLimiter(config)
	defer rl.Stop()

	ip := "192.168.1.100"
	username := "test-user"

	// First 2 failures - should not lock
	for i := 0; i < 2; i++ {
		ipLocked, _ := rl.RecordFailure(ip, username)
		if ipLocked {
			t.Errorf("IP should not be locked after %d failures", i+1)
		}

		allowed, _ := rl.CheckIP(ip)
		if !allowed {
			t.Errorf("IP should be allowed after %d failures", i+1)
		}
	}

	// 3rd failure - should lock
	ipLocked, _ := rl.RecordFailure(ip, username)
	if !ipLocked {
		t.Error("IP should be locked after 3 failures")
	}

	// Check that IP is now locked out
	allowed, unlockAt := rl.CheckIP(ip)
	if allowed {
		t.Error("IP should be locked out")
	}
	if unlockAt.IsZero() {
		t.Error("UnlockAt should be set")
	}

	// Verify unlockAt is in the future
	if time.Until(unlockAt) < 0 {
		t.Error("UnlockAt should be in the future")
	}
}

func TestRateLimiterUserLockout(t *testing.T) {
	config := &SSHConfig{
		UserFailureLimit:  5,
		UserLockoutWindow: 10 * time.Minute,
		LockoutDuration:   15 * time.Minute,
	}

	rl := NewRateLimiter(config)
	defer rl.Stop()

	ip := "192.168.1.100"
	username := "test-user"

	// First 4 failures - should not lock
	for i := 0; i < 4; i++ {
		_, userLocked := rl.RecordFailure(ip, username)
		if userLocked {
			t.Errorf("User should not be locked after %d failures", i+1)
		}

		allowed, _ := rl.CheckUser(username)
		if !allowed {
			t.Errorf("User should be allowed after %d failures", i+1)
		}
	}

	// 5th failure - should lock
	_, userLocked := rl.RecordFailure(ip, username)
	if !userLocked {
		t.Error("User should be locked after 5 failures")
	}

	// Check that user is now locked out
	allowed, unlockAt := rl.CheckUser(username)
	if allowed {
		t.Error("User should be locked out")
	}
	if unlockAt.IsZero() {
		t.Error("UnlockAt should be set")
	}
}

func TestRateLimiterSuccessClearsFailures(t *testing.T) {
	config := &SSHConfig{
		IPFailureLimit:    3,
		UserFailureLimit:  5,
		IPLockoutWindow:   5 * time.Minute,
		UserLockoutWindow: 10 * time.Minute,
		LockoutDuration:   15 * time.Minute,
	}

	rl := NewRateLimiter(config)
	defer rl.Stop()

	ip := "192.168.1.100"
	username := "test-user"

	// Record 2 failures
	rl.RecordFailure(ip, username)
	rl.RecordFailure(ip, username)

	// Verify not locked yet
	if allowed, _ := rl.CheckIP(ip); !allowed {
		t.Error("IP should not be locked after 2 failures")
	}

	// Record success - should clear failures
	rl.RecordSuccess(ip, username)

	// Record 2 more failures
	rl.RecordFailure(ip, username)
	rl.RecordFailure(ip, username)

	// Should still not be locked (failures were reset)
	if allowed, _ := rl.CheckIP(ip); !allowed {
		t.Error("IP should not be locked - failures should have been reset by success")
	}
}

func TestRateLimiterExpiredFailures(t *testing.T) {
	config := &SSHConfig{
		IPFailureLimit:    3,
		IPLockoutWindow:   100 * time.Millisecond, // Short window for testing
		UserFailureLimit:  5,
		UserLockoutWindow: 100 * time.Millisecond,
		LockoutDuration:   200 * time.Millisecond,
	}

	rl := NewRateLimiter(config)
	defer rl.Stop()

	ip := "192.168.1.100"
	username := "test-user"

	// Record 2 failures
	rl.RecordFailure(ip, username)
	rl.RecordFailure(ip, username)

	// Wait for failures to expire
	time.Sleep(150 * time.Millisecond)

	// Record 2 more failures - should not lock (old ones expired)
	ipLocked, _ := rl.RecordFailure(ip, username)
	if ipLocked {
		t.Error("IP should not be locked - old failures should have expired")
	}
	ipLocked, _ = rl.RecordFailure(ip, username)
	if ipLocked {
		t.Error("IP should not be locked with only 2 recent failures")
	}
}

func TestRateLimiterLockoutExpiration(t *testing.T) {
	config := &SSHConfig{
		IPFailureLimit:  3,
		IPLockoutWindow: 5 * time.Minute,
		LockoutDuration: 100 * time.Millisecond, // Short lockout for testing
	}

	rl := NewRateLimiter(config)
	defer rl.Stop()

	ip := "192.168.1.100"
	username := "test-user"

	// Trigger lockout
	for i := 0; i < 3; i++ {
		rl.RecordFailure(ip, username)
	}

	// Verify locked
	if allowed, _ := rl.CheckIP(ip); allowed {
		t.Error("IP should be locked")
	}

	// Wait for lockout to expire
	time.Sleep(150 * time.Millisecond)

	// Should be allowed again
	if allowed, _ := rl.CheckIP(ip); !allowed {
		t.Error("IP lockout should have expired")
	}
}

func TestRateLimiterSeparateIPAndUser(t *testing.T) {
	config := &SSHConfig{
		IPFailureLimit:    3,
		UserFailureLimit:  5,
		IPLockoutWindow:   5 * time.Minute,
		UserLockoutWindow: 10 * time.Minute,
		LockoutDuration:   15 * time.Minute,
	}

	rl := NewRateLimiter(config)
	defer rl.Stop()

	ip1 := "192.168.1.100"
	ip2 := "192.168.1.101"
	user1 := "alice"
	user2 := "bob"

	// Lock IP1 with user1
	for i := 0; i < 3; i++ {
		rl.RecordFailure(ip1, user1)
	}

	// IP1 should be locked
	if allowed, _ := rl.CheckIP(ip1); allowed {
		t.Error("IP1 should be locked")
	}

	// IP2 should still be allowed
	if allowed, _ := rl.CheckIP(ip2); !allowed {
		t.Error("IP2 should be allowed")
	}

	// user1 should not be locked yet (needs 5 failures)
	if allowed, _ := rl.CheckUser(user1); !allowed {
		t.Error("user1 should not be locked with only 3 failures")
	}

	// user2 should be allowed
	if allowed, _ := rl.CheckUser(user2); !allowed {
		t.Error("user2 should be allowed")
	}
}

func TestRateLimiterStats(t *testing.T) {
	config := &SSHConfig{
		IPFailureLimit:    3,
		UserFailureLimit:  5,
		IPLockoutWindow:   5 * time.Minute,
		UserLockoutWindow: 10 * time.Minute,
		LockoutDuration:   15 * time.Minute,
	}

	rl := NewRateLimiter(config)
	defer rl.Stop()

	// Initial stats
	stats := rl.GetStats()
	if stats.IPsTracked != 0 || stats.IPsLocked != 0 {
		t.Error("Initial stats should be zero")
	}

	// Record failures for 2 IPs
	rl.RecordFailure("192.168.1.100", "alice")
	rl.RecordFailure("192.168.1.101", "bob")

	stats = rl.GetStats()
	if stats.IPsTracked != 2 {
		t.Errorf("Expected 2 IPs tracked, got %d", stats.IPsTracked)
	}

	// Lock one IP
	rl.RecordFailure("192.168.1.100", "alice")
	rl.RecordFailure("192.168.1.100", "alice")

	stats = rl.GetStats()
	if stats.IPsLocked != 1 {
		t.Errorf("Expected 1 IP locked, got %d", stats.IPsLocked)
	}
}

func TestRateLimiterCleanup(t *testing.T) {
	config := &SSHConfig{
		IPFailureLimit:    3,
		IPLockoutWindow:   50 * time.Millisecond,
		UserFailureLimit:  5,
		UserLockoutWindow: 50 * time.Millisecond,
		LockoutDuration:   100 * time.Millisecond,
	}

	rl := NewRateLimiter(config)
	defer rl.Stop()

	// Record failures
	rl.RecordFailure("192.168.1.100", "alice")
	rl.RecordFailure("192.168.1.101", "bob")

	// Trigger manual cleanup
	time.Sleep(200 * time.Millisecond)
	rl.cleanup()

	// Stats should show cleanup happened
	stats := rl.GetStats()
	if stats.IPsTracked != 0 {
		t.Errorf("Expected 0 IPs tracked after cleanup, got %d", stats.IPsTracked)
	}
}

func TestFormatLockoutError(t *testing.T) {
	tests := []struct {
		name     string
		unlockAt time.Time
		wantText string
	}{
		{
			name:     "future unlock",
			unlockAt: time.Now().Add(5 * time.Minute),
			wantText: "account locked out, try again in",
		},
		{
			name:     "past unlock",
			unlockAt: time.Now().Add(-1 * time.Minute),
			wantText: "account locked out, try again in 0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := FormatLockoutError(tt.unlockAt)
			if err == nil {
				t.Error("Expected error, got nil")
			}
			if err != nil && len(err.Error()) == 0 {
				t.Error("Error message should not be empty")
			}
			// Just verify it returns a non-empty error message
		})
	}
}
