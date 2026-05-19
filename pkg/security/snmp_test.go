package security

import (
	"strings"
	"testing"
)

func TestValidateSNMPCommunityAcceptsExplicitCommunity(t *testing.T) {
	if err := ValidateSNMPCommunity("monitoring-ro"); err != nil {
		t.Fatalf("ValidateSNMPCommunity() error = %v", err)
	}
}

func TestValidateSNMPCommunityRejectsEmptyCommunity(t *testing.T) {
	if err := ValidateSNMPCommunity(" \t "); err == nil {
		t.Fatal("ValidateSNMPCommunity() error = nil, want missing community error")
	}
}

func TestValidateSNMPCommunityRejectsWellKnownDefaults(t *testing.T) {
	for _, community := range []string{"public", "PUBLIC", " private "} {
		t.Run(strings.TrimSpace(community), func(t *testing.T) {
			err := ValidateSNMPCommunity(community)
			if err == nil {
				t.Fatal("ValidateSNMPCommunity() error = nil, want weak community error")
			}
			if strings.Contains(err.Error(), strings.TrimSpace(community)) {
				t.Fatalf("ValidateSNMPCommunity() error leaked community: %v", err)
			}
		})
	}
}
