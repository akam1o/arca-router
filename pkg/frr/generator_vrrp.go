package frr

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// GenerateVRRPConfig generates FRR VRRP interface configuration.
func GenerateVRRPConfig(cfg *VRRPConfig) (string, error) {
	if cfg == nil || len(cfg.Groups) == 0 {
		return "", nil
	}
	if err := validateVRRPConfig(cfg); err != nil {
		return "", err
	}

	groupsByInterface := make(map[string][]VRRPGroup)
	for _, group := range cfg.Groups {
		groupsByInterface[group.Interface] = append(groupsByInterface[group.Interface], group)
	}

	interfaces := make([]string, 0, len(groupsByInterface))
	for ifName := range groupsByInterface {
		interfaces = append(interfaces, ifName)
	}
	sort.Strings(interfaces)

	var b strings.Builder
	for _, ifName := range interfaces {
		groups := groupsByInterface[ifName]
		sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })

		fmt.Fprintf(&b, "interface %s\n", ifName)
		for _, group := range groups {
			fmt.Fprintf(&b, " vrrp %d version 3\n", group.ID)
			if group.Priority != 0 {
				fmt.Fprintf(&b, " vrrp %d priority %d\n", group.ID, group.Priority)
			}
			if group.Preempt {
				fmt.Fprintf(&b, " vrrp %d preempt\n", group.ID)
			}
			if net.ParseIP(group.VirtualAddress).To4() == nil {
				fmt.Fprintf(&b, " vrrp %d ipv6 %s\n", group.ID, group.VirtualAddress)
			} else {
				fmt.Fprintf(&b, " vrrp %d ip %s\n", group.ID, group.VirtualAddress)
			}
		}
		b.WriteString("!\n")
	}
	return b.String(), nil
}

func validateVRRPConfig(cfg *VRRPConfig) error {
	if cfg == nil {
		return nil
	}
	seenGroups := make(map[string]struct{}, len(cfg.Groups))
	for _, group := range cfg.Groups {
		if group.Interface == "" {
			return NewInvalidConfigError(fmt.Sprintf("VRRP group %d missing interface", group.ID))
		}
		if group.ID < 1 || group.ID > 255 {
			return NewInvalidConfigError(fmt.Sprintf("VRRP group id must be 1-255, got %d", group.ID))
		}
		if group.VirtualAddress == "" || net.ParseIP(group.VirtualAddress) == nil {
			return NewInvalidConfigError(fmt.Sprintf("VRRP group %d has invalid virtual address %q", group.ID, group.VirtualAddress))
		}
		if group.Priority < 0 || group.Priority > 254 {
			return NewInvalidConfigError(fmt.Sprintf("VRRP group %d priority must be 1-254 when configured, got %d", group.ID, group.Priority))
		}
		key := fmt.Sprintf("%s\x00%d", group.Interface, group.ID)
		if _, ok := seenGroups[key]; ok {
			return NewInvalidConfigError(fmt.Sprintf("VRRP group %d on interface %s is duplicated", group.ID, group.Interface))
		}
		seenGroups[key] = struct{}{}
	}
	return nil
}
