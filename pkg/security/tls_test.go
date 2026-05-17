package security

import (
	"crypto/tls"
	"testing"
)

func TestApplyTLSPolicySetsMinimumVersion(t *testing.T) {
	cfg := ApplyTLSPolicy(&tls.Config{})
	if cfg.MinVersion != MinimumTLSVersion {
		t.Fatalf("MinVersion = %x, want %x", cfg.MinVersion, MinimumTLSVersion)
	}
}

func TestApplyTLSPolicyPreservesStrongerMinimumVersion(t *testing.T) {
	cfg := ApplyTLSPolicy(&tls.Config{MinVersion: tls.VersionTLS13})
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Fatalf("MinVersion = %x, want %x", cfg.MinVersion, tls.VersionTLS13)
	}
}

func TestApplyTLSPolicyClonesInput(t *testing.T) {
	input := &tls.Config{}
	cfg := ApplyTLSPolicy(input)
	if cfg == input {
		t.Fatal("ApplyTLSPolicy() returned the input config")
	}
	if input.MinVersion != 0 {
		t.Fatalf("input MinVersion = %x, want 0", input.MinVersion)
	}
}
