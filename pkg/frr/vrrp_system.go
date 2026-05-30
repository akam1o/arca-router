package frr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	vrrpIPv4Family          = 4
	vrrpIPv6Family          = 6
	defaultVRRPStatePath    = "/var/lib/arca-router/vrrp-interfaces.json"
	defaultVRRPStatePerm    = 0600
	defaultVRRPStateDirPerm = 0750
)

// VRRPSystemPreparer prepares Linux interfaces required by FRR vrrpd.
type VRRPSystemPreparer interface {
	Prepare(ctx context.Context, cfg *Config) error
}

// IPCommandRunner executes one iproute2 command.
type IPCommandRunner func(ctx context.Context, args ...string) ([]byte, error)

// IPVRRPSystemPreparer reconciles arca-owned macvlan interfaces for VRRP.
type IPVRRPSystemPreparer struct {
	mu          sync.Mutex
	run         IPCommandRunner
	statePath   string
	stateLoaded bool
	knownNames  map[string]bool
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
		return NewIPVRRPSystemPreparerWithState(run, defaultVRRPStatePath)
	}
	return &IPVRRPSystemPreparer{run: run, knownNames: make(map[string]bool)}
}

// NewIPVRRPSystemPreparerWithState creates an iproute2-backed preparer with state persistence.
func NewIPVRRPSystemPreparerWithState(run IPCommandRunner, statePath string) *IPVRRPSystemPreparer {
	if run == nil {
		run = runIPCommand
	}
	return &IPVRRPSystemPreparer{
		run:        run,
		statePath:  statePath,
		knownNames: make(map[string]bool),
	}
}

// Prepare creates or updates arca-owned macvlan interfaces used by FRR VRRP.
func (p *IPVRRPSystemPreparer) Prepare(ctx context.Context, cfg *Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.run == nil {
		p.run = runIPCommand
	}
	if p.knownNames == nil {
		p.knownNames = make(map[string]bool)
	}
	if err := p.loadState(); err != nil {
		return NewApplyError("load VRRP interface state", err)
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
	if err := p.saveState(); err != nil {
		return NewApplyError("save VRRP interface state", err)
	}
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

func (p *IPVRRPSystemPreparer) loadState() error {
	if p.stateLoaded || p.statePath == "" {
		p.stateLoaded = true
		return nil
	}
	data, err := os.ReadFile(p.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			p.stateLoaded = true
			return nil
		}
		return err
	}
	var names []string
	if len(data) > 0 {
		if err := json.Unmarshal(data, &names); err != nil {
			return err
		}
	}
	for _, name := range names {
		if isArcaVRRPInterfaceName(name) {
			p.knownNames[name] = true
		}
	}
	p.stateLoaded = true
	return nil
}

func (p *IPVRRPSystemPreparer) saveState() error {
	if p.statePath == "" {
		return nil
	}
	if len(p.knownNames) == 0 {
		if err := os.Remove(p.statePath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	names := make([]string, 0, len(p.knownNames))
	for name := range p.knownNames {
		names = append(names, name)
	}
	sort.Strings(names)
	data, err := json.MarshalIndent(names, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(p.statePath), defaultVRRPStateDirPerm); err != nil {
		return err
	}
	return os.WriteFile(p.statePath, data, defaultVRRPStatePerm)
}

func vrrpSystemInterfaces(cfg *VRRPConfig) ([]vrrpSystemInterface, error) {
	if cfg == nil || len(cfg.Groups) == 0 {
		return nil, nil
	}
	if err := validateVRRPConfig(cfg); err != nil {
		return nil, err
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
	if !strings.HasPrefix(name, "arv4-") && !strings.HasPrefix(name, "arv6-") {
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
	ipPath, err := lookupIPPath()
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return nil, NewPermissionDeniedError("find ip", err)
		}
		return nil, NewToolNotFoundError("ip")
	}
	cmd := exec.CommandContext(ctx, ipPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return output, nil
}
