package netconf

import "time"

// SSHConfig holds SSH server configuration
type SSHConfig struct {
	ListenAddr      string        // Default: ":830"
	HostKeyPath     string        // Default: "/var/lib/arca-router/ssh_host_ed25519_key"
	UserDBPath      string        // Default: "/var/lib/arca-router/users.db"
	DatastorePath   string        // Default: "/var/lib/arca-router/config.db"
	IdleTimeout     time.Duration // Default: 30m (idle timeout)
	AbsoluteTimeout time.Duration // Default: 24h (max session lifetime)
	MaxSessions     int           // Default: 100

	// Lockout configuration
	IPFailureLimit    int           // Default: 3 (IP-based lockout threshold)
	IPLockoutWindow   time.Duration // Default: 5m (IP failure tracking window)
	UserFailureLimit  int           // Default: 5 (User-based lockout threshold)
	UserLockoutWindow time.Duration // Default: 10m (User failure tracking window)
	LockoutDuration   time.Duration // Default: 15m (lockout duration for both IP and user)

	SSHCiphers      []string // Default: ["chacha20-poly1305@openssh.com", "aes256-gcm@openssh.com"]
	SSHKeyExchanges []string // Default: ["curve25519-sha256", "ecdh-sha2-nistp256"]
	SSHMACs         []string // Default: ["hmac-sha2-256-etm@openssh.com", "hmac-sha2-512-etm@openssh.com"]
}

// DefaultSSHConfig returns default SSH server configuration
func DefaultSSHConfig() *SSHConfig {
	return &SSHConfig{
		ListenAddr:        ":830",
		HostKeyPath:       "/var/lib/arca-router/ssh_host_ed25519_key",
		UserDBPath:        "/var/lib/arca-router/users.db",
		DatastorePath:     "/var/lib/arca-router/config.db",
		IdleTimeout:       30 * time.Minute,
		AbsoluteTimeout:   24 * time.Hour,
		MaxSessions:       100,
		IPFailureLimit:    3,
		IPLockoutWindow:   5 * time.Minute,
		UserFailureLimit:  5,
		UserLockoutWindow: 10 * time.Minute,
		LockoutDuration:   15 * time.Minute,
		SSHCiphers: []string{
			"chacha20-poly1305@openssh.com",
			"aes256-gcm@openssh.com",
			"aes128-gcm@openssh.com",
		},
		SSHKeyExchanges: []string{
			"curve25519-sha256",
			"curve25519-sha256@libssh.org",
			"ecdh-sha2-nistp256",
		},
		SSHMACs: []string{
			"hmac-sha2-256-etm@openssh.com",
			"hmac-sha2-512-etm@openssh.com",
		},
	}
}
