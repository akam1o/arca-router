package security

import (
	"errors"
	"strings"
)

var weakSNMPCommunities = map[string]struct{}{
	"private": {},
	"public":  {},
}

// ValidateSNMPCommunity rejects empty and well-known default SNMPv2c
// communities. The supplied value is a shared secret and is intentionally not
// included in returned errors.
func ValidateSNMPCommunity(community string) error {
	normalized := strings.ToLower(strings.TrimSpace(community))
	if normalized == "" {
		return errors.New("SNMP community is required")
	}
	if _, weak := weakSNMPCommunities[normalized]; weak {
		return errors.New("SNMP community must not use a well-known default")
	}
	return nil
}
