package netconf

import (
	"time"

	"github.com/akam1o/arca-router/pkg/datastore"
)

// SSHConfig holds SSH server configuration
type SSHConfig struct {
	ListenAddr                  string // Default: ":830"
	HostKeyPath                 string // Default: "/var/lib/arca-router/ssh_host_ed25519_key"
	UserDBPath                  string // Default: "/var/lib/arca-router/users.db"
	DatastorePath               string // Default: "/var/lib/arca-router/config.db"
	DatastoreConfig             *datastore.Config
	SkipDatastoreStartupCleanup bool // For embedded servers whose parent owns datastore startup
	// AdvertiseStandardXPath controls standard :xpath capability advertisement.
	// It defaults to true for v0.10; set DisableStandardXPath to suppress it.
	AdvertiseStandardXPath bool
	DisableStandardXPath   bool
	IdleTimeout            time.Duration // Default: 30m (idle timeout)
	AbsoluteTimeout        time.Duration // Default: 24h (max session lifetime)
	MaxSessions            int           // Default: 100

	// Lockout configuration
	IPFailureLimit    int           // Default: 3 (IP-based lockout threshold)
	IPLockoutWindow   time.Duration // Default: 5m (IP failure tracking window)
	UserFailureLimit  int           // Default: 5 (User-based lockout threshold)
	UserLockoutWindow time.Duration // Default: 10m (User failure tracking window)
	LockoutDuration   time.Duration // Default: 15m (lockout duration for both IP and user)

	SSHCiphers      []string // Default: modern AEAD ciphers plus AES-CTR for NETCONF client interop
	SSHKeyExchanges []string // Default: ["curve25519-sha256", "ecdh-sha2-nistp256"]
	SSHMACs         []string // Default: ["hmac-sha2-256-etm@openssh.com", "hmac-sha2-512-etm@openssh.com"]
}

// DefaultSSHConfig returns default SSH server configuration
func DefaultSSHConfig() *SSHConfig {
	return &SSHConfig{
		ListenAddr:             ":830",
		HostKeyPath:            "/var/lib/arca-router/ssh_host_ed25519_key",
		UserDBPath:             "/var/lib/arca-router/users.db",
		DatastorePath:          "/var/lib/arca-router/config.db",
		IdleTimeout:            30 * time.Minute,
		AbsoluteTimeout:        24 * time.Hour,
		MaxSessions:            100,
		IPFailureLimit:         3,
		IPLockoutWindow:        5 * time.Minute,
		UserFailureLimit:       5,
		UserLockoutWindow:      10 * time.Minute,
		LockoutDuration:        15 * time.Minute,
		AdvertiseStandardXPath: true,
		SSHCiphers: []string{
			"chacha20-poly1305@openssh.com",
			"aes256-gcm@openssh.com",
			"aes128-gcm@openssh.com",
			"aes256-ctr",
			"aes128-ctr",
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

func sshConfigWithDefaults(config *SSHConfig) *SSHConfig {
	defaults := DefaultSSHConfig()
	if config == nil {
		return defaults
	}

	merged := *config
	if merged.ListenAddr == "" {
		merged.ListenAddr = defaults.ListenAddr
	}
	if merged.HostKeyPath == "" {
		merged.HostKeyPath = defaults.HostKeyPath
	}
	if merged.UserDBPath == "" {
		merged.UserDBPath = defaults.UserDBPath
	}
	if merged.DatastorePath == "" {
		merged.DatastorePath = defaults.DatastorePath
	}
	if merged.DatastoreConfig != nil {
		datastoreConfig := *merged.DatastoreConfig
		merged.DatastoreConfig = &datastoreConfig
	}
	if merged.DisableStandardXPath {
		merged.AdvertiseStandardXPath = false
	} else if !merged.AdvertiseStandardXPath {
		merged.AdvertiseStandardXPath = defaults.AdvertiseStandardXPath
	}
	if merged.IdleTimeout <= 0 {
		merged.IdleTimeout = defaults.IdleTimeout
	}
	if merged.AbsoluteTimeout <= 0 {
		merged.AbsoluteTimeout = defaults.AbsoluteTimeout
	}
	if merged.MaxSessions <= 0 {
		merged.MaxSessions = defaults.MaxSessions
	}
	if merged.IPFailureLimit <= 0 {
		merged.IPFailureLimit = defaults.IPFailureLimit
	}
	if merged.UserFailureLimit <= 0 {
		merged.UserFailureLimit = defaults.UserFailureLimit
	}
	if merged.IPLockoutWindow <= 0 {
		merged.IPLockoutWindow = defaults.IPLockoutWindow
	}
	if merged.UserLockoutWindow <= 0 {
		merged.UserLockoutWindow = defaults.UserLockoutWindow
	}
	if merged.LockoutDuration <= 0 {
		merged.LockoutDuration = defaults.LockoutDuration
	}
	if len(merged.SSHCiphers) == 0 {
		merged.SSHCiphers = append([]string(nil), defaults.SSHCiphers...)
	} else {
		merged.SSHCiphers = append([]string(nil), merged.SSHCiphers...)
	}
	if len(merged.SSHKeyExchanges) == 0 {
		merged.SSHKeyExchanges = append([]string(nil), defaults.SSHKeyExchanges...)
	} else {
		merged.SSHKeyExchanges = append([]string(nil), merged.SSHKeyExchanges...)
	}
	if len(merged.SSHMACs) == 0 {
		merged.SSHMACs = append([]string(nil), defaults.SSHMACs...)
	} else {
		merged.SSHMACs = append([]string(nil), merged.SSHMACs...)
	}
	return &merged
}
