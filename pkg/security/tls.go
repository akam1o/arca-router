package security

import "crypto/tls"

// MinimumTLSVersion is the lowest TLS version accepted by arca-router services.
const MinimumTLSVersion uint16 = tls.VersionTLS12

// ApplyTLSPolicy returns a TLS config with the project-wide transport security
// defaults applied. The input config is cloned before mutation.
func ApplyTLSPolicy(cfg *tls.Config) *tls.Config {
	if cfg == nil {
		cfg = &tls.Config{}
	} else {
		cfg = cfg.Clone()
	}
	if cfg.MinVersion == 0 || cfg.MinVersion < MinimumTLSVersion {
		cfg.MinVersion = MinimumTLSVersion
	}
	return cfg
}
