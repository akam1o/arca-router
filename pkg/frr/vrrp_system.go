package frr

import (
	"context"
	"fmt"
	"hash/crc32"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

const (
	vrrpIPv4Family = 4
	vrrpIPv6Family = 6
)

// VRRPSystemPreparer prepares Linux interfaces required by FRR vrrpd.
type VRRPSystemPreparer interface {
	Prepare(ctx context.Context, cfg *Config) error
}

// IPCommandRunner executes one iproute2 command.
type IPCommandRunner func(ctx context.Context, args ...string) ([]byte, error)

// IPVRRPSystemPreparer reconciles arca-owned macvlan interfaces for VRRP.
type IPVRRPSystemPreparer struct {
	run        IPCommandRunner
	knownNames map[string]bool
}

type vrrpSystemInterface struct {
	Name    string
	Parent  string
	MAC     string
	Address string
}

// NewIPVRRPSystemPreparer creates an iproute2-backed VRRP system preparer.
func NewIPVRRPSystemPreparer(run IPCommandRunner) *IPVRRPSystemPreparer {
	if run == nil {
		run = runIPCommand
	}
	return &IPVRRPSystemPreparer{run: run, knownNames: make(map[string]bool)}
}

// Prepare creates or updates arca-owned macvlan interfaces used by FRR VRRP.
func (p *IPVRRPSystemPreparer) Prepare(ctx context.Context, cfg *Config) error {
	if p.run == nil {
		p.run = runIPCommand
	}
	if p.knownNames == nil {
		p.knownNames = make(map[string]bool)
	}
	var vrrpConfig *VRRPConfig
	if cfg != nil {
		vrrpConfig = cfg.VRRP
	}
	interfaces, err := vrrpSystemInterfaces(vrrpConfig)
	if err != nil {
		return err
	}
	desired := make(map[string]bool, len(interfaces))
	for _, iface := range interfaces {
		desired[iface.Name] = true
	}
	if len(desired) == 0 && len(p.knownNames) == 0 {
		return nil
	}
	if err := p.deleteStaleInterfaces(ctx, desired); err != nil {
		return err
	}
	for _, iface := range interfaces {
		if err := p.ensureInterface(ctx, iface); err != nil {
			return err
		}
	}
	p.knownNames = desired
	return nil
}

func (p *IPVRRPSystemPreparer) deleteStaleInterfaces(ctx context.Context, desired map[string]bool) error {
	output, err := p.run(ctx, "link", "show")
	if err != nil {
		return NewApplyError("list Linux interfaces for VRRP reconciliation", err)
	}
	for _, name := range arcaVRRPInterfaceNames(string(output)) {
		if desired[name] {
			continue
		}
		if _, err := p.run(ctx, "link", "delete", name); err != nil {
			return NewApplyError(fmt.Sprintf("delete stale VRRP macvlan %s", name), err)
		}
	}
	return nil
}

func (p *IPVRRPSystemPreparer) ensureInterface(ctx context.Context, iface vrrpSystemInterface) error {
	if _, err := p.run(ctx, "link", "show", "dev", iface.Name); err != nil {
		if _, addErr := p.run(ctx, "link", "add", iface.Name, "link", iface.Parent, "addrgenmode", "random", "type", "macvlan", "mode", "bridge"); addErr != nil {
			return NewApplyError(fmt.Sprintf("create VRRP macvlan %s", iface.Name), addErr)
		}
	}
	commands := [][]string{
		{"link", "set", "dev", iface.Name, "address", iface.MAC},
		{"addr", "replace", iface.Address, "dev", iface.Name},
		{"link", "set", "dev", iface.Name, "up"},
	}
	for _, args := range commands {
		if _, err := p.run(ctx, args...); err != nil {
			return NewApplyError(fmt.Sprintf("prepare VRRP macvlan %s", iface.Name), err)
		}
	}
	return nil
}

func vrrpSystemInterfaces(cfg *VRRPConfig) ([]vrrpSystemInterface, error) {
	if cfg == nil || len(cfg.Groups) == 0 {
		return nil, nil
	}
	groups := append([]VRRPGroup(nil), cfg.Groups...)
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Interface != groups[j].Interface {
			return groups[i].Interface < groups[j].Interface
		}
		return groups[i].ID < groups[j].ID
	})

	interfaces := make([]vrrpSystemInterface, 0, len(groups))
	for _, group := range groups {
		ip := net.ParseIP(group.VirtualAddress)
		if group.Interface == "" {
			return nil, NewInvalidConfigError(fmt.Sprintf("VRRP group %d missing interface", group.ID))
		}
		if group.ID < 1 || group.ID > 255 {
			return nil, NewInvalidConfigError(fmt.Sprintf("VRRP group id must be 1-255, got %d", group.ID))
		}
		if ip == nil {
			return nil, NewInvalidConfigError(fmt.Sprintf("VRRP group %d has invalid virtual address %q", group.ID, group.VirtualAddress))
		}
		family := vrrpIPv4Family
		prefixLen := "32"
		if ip.To4() == nil {
			family = vrrpIPv6Family
			prefixLen = "128"
		}
		interfaces = append(interfaces, vrrpSystemInterface{
			Name:    vrrpMacvlanName(family, group.Interface, group.ID),
			Parent:  group.Interface,
			MAC:     vrrpVirtualMAC(family, group.ID),
			Address: ip.String() + "/" + prefixLen,
		})
	}
	return interfaces, nil
}

func vrrpMacvlanName(family int, parent string, id int) string {
	hash := crc32.ChecksumIEEE([]byte(parent)) & 0xffff
	return fmt.Sprintf("arv%d-%03d-%04x", family, id, hash)
}

func vrrpVirtualMAC(family, id int) string {
	familyByte := "01"
	if family == vrrpIPv6Family {
		familyByte = "02"
	}
	return fmt.Sprintf("00:00:5e:00:%s:%02x", familyByte, id)
}

func arcaVRRPInterfaceNames(output string) []string {
	var names []string
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[1])
		if at := strings.IndexByte(name, '@'); at >= 0 {
			name = name[:at]
		}
		if isArcaVRRPInterfaceName(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func isArcaVRRPInterfaceName(name string) bool {
	if !(strings.HasPrefix(name, "arv4-") || strings.HasPrefix(name, "arv6-")) {
		return false
	}
	parts := strings.Split(name, "-")
	if len(parts) != 3 || len(parts[1]) != 3 || len(parts[2]) != 4 {
		return false
	}
	if _, err := strconv.Atoi(parts[1]); err != nil {
		return false
	}
	_, err := strconv.ParseUint(parts[2], 16, 16)
	return err == nil
}

func runIPCommand(ctx context.Context, args ...string) ([]byte, error) {
	ipPath, err := exec.LookPath("ip")
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, ipPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return output, nil
}
