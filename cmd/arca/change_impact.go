package main

import (
	"fmt"
	"strings"

	grpcclient "github.com/akam1o/arca-router/internal/northbound/grpc"
)

type changeImpactPreview struct {
	addedLines             int
	removedLines           int
	interfaces             changeImpactLineCount
	interfaceChanges       []changeImpactInterfaceChange
	staticRoutes           changeImpactLineCount
	staticRouteChanges     []changeImpactRouteChange
	policyOptions          changeImpactLineCount
	policyChanges          []changeImpactPolicyChange
	bgpPolicyBindings      []changeImpactBGPPolicyBinding
	bgp                    changeImpactLineCount
	ospf                   changeImpactLineCount
	bfd                    changeImpactLineCount
	evpn                   changeImpactLineCount
	routingInstances       changeImpactLineCount
	classOfService         changeImpactLineCount
	interfaceAddressChange bool
	defaultRouteChange     bool
}

type changeImpactLineCount struct {
	added   int
	removed int
}

type changeImpactInterfaceChange struct {
	sign      byte
	name      string
	operation string
	value     string
}

type changeImpactRouteChange struct {
	sign            byte
	prefix          string
	nextHop         string
	routingInstance string
}

type changeImpactPolicyChange struct {
	sign   byte
	kind   string
	name   string
	term   string
	prefix string
}

type changeImpactBGPPolicyBinding struct {
	sign      byte
	groupName string
	direction string
	policy    string
}

func formatChangeImpactPreview(diffText string, hasChanges bool) []string {
	if !hasChanges || strings.TrimSpace(diffText) == "" {
		return []string{"change impact preview: no candidate changes"}
	}

	preview := analyzeChangeImpact(diffText)
	lines := []string{
		"change impact preview:",
		fmt.Sprintf("  changed lines: +%d -%d", preview.addedLines, preview.removedLines),
	}
	lines = appendChangeImpactLine(lines, "interfaces", preview.interfaces)
	lines = appendInterfaceImpactLines(lines, preview.interfaceChanges)
	lines = appendChangeImpactLine(lines, "static routes", preview.staticRoutes)
	lines = appendStaticRouteImpactLines(lines, preview.staticRouteChanges)
	lines = appendChangeImpactLine(lines, "policy-options", preview.policyOptions)
	lines = appendPolicyImpactLines(lines, preview.policyChanges, preview.bgpPolicyBindings)
	lines = appendRoutePolicyDryRunLines(lines, preview.policyChanges, preview.bgpPolicyBindings)
	lines = appendChangeImpactLine(lines, "bgp", preview.bgp)
	lines = appendChangeImpactLine(lines, "ospf", preview.ospf)
	lines = appendChangeImpactLine(lines, "bfd", preview.bfd)
	lines = appendChangeImpactLine(lines, "evpn", preview.evpn)
	lines = appendChangeImpactLine(lines, "routing-instances", preview.routingInstances)
	lines = appendChangeImpactLine(lines, "class-of-service", preview.classOfService)
	if preview.defaultRouteChange {
		lines = append(lines, "  warning: default route changes can affect all unmatched traffic")
	}
	if preview.interfaces.hasChanges() {
		lines = append(lines, "  warning: interface changes can affect link state or attached services")
	}
	if preview.interfaceAddressChange {
		lines = append(lines, "  warning: interface address changes can alter connected route reachability")
	}
	if preview.staticRoutes.removed > 0 {
		lines = append(lines, "  warning: static route removals can withdraw forwarding entries")
	}
	if preview.policyOptions.hasChanges() {
		lines = append(lines, "  warning: policy-options changes can regenerate FRR route-maps")
	}
	if preview.bgp.hasChanges() {
		lines = append(lines, "  warning: BGP changes can reset sessions or change route advertisements")
	}
	if preview.ospf.hasChanges() {
		lines = append(lines, "  warning: OSPF changes can trigger adjacency updates or SPF recalculation")
	}
	if preview.bfd.hasChanges() {
		lines = append(lines, "  warning: BFD changes can affect fast failure detection")
	}
	if preview.evpn.hasChanges() {
		lines = append(lines, "  warning: EVPN changes can alter overlay VNI reachability")
	}
	if preview.routingInstances.hasChanges() {
		lines = append(lines, "  warning: routing-instance changes can move interfaces or VRF routing state")
	}
	if preview.classOfService.hasChanges() {
		lines = append(lines, "  warning: class-of-service changes can alter traffic treatment")
	}
	return lines
}

func appendRoutePolicyDryRunLines(lines []string, policyChanges []changeImpactPolicyChange, bgpBindings []changeImpactBGPPolicyBinding) []string {
	if len(policyChanges) == 0 && len(bgpBindings) == 0 {
		return lines
	}
	prefixLists := 0
	routeMaps := 0
	for _, change := range policyChanges {
		switch change.kind {
		case "prefix-list":
			prefixLists++
		case "route-map":
			routeMaps++
		}
	}
	lines = append(lines, "  route-policy dry-run:")
	if prefixLists > 0 {
		lines = append(lines, fmt.Sprintf("    prefix-list updates: %d", prefixLists))
	}
	if routeMaps > 0 {
		lines = append(lines, fmt.Sprintf("    route-map regeneration planned: %d policy statement changes", routeMaps))
	}
	if len(bgpBindings) > 0 {
		lines = append(lines, fmt.Sprintf("    bgp policy bindings updated: %d", len(bgpBindings)))
	}
	lines = append(lines, "    validation scope: candidate policy syntax and referenced BGP bindings")
	return lines
}

func appendInterfaceImpactLines(lines []string, changes []changeImpactInterfaceChange) []string {
	if len(changes) == 0 {
		return lines
	}
	lines = append(lines, "  interface diff:")
	limit := len(changes)
	if limit > maxChangeImpactInterfaceDetails {
		limit = maxChangeImpactInterfaceDetails
	}
	for _, change := range changes[:limit] {
		lines = append(lines, "    "+change.summary())
	}
	if remaining := len(changes) - limit; remaining > 0 {
		lines = append(lines, fmt.Sprintf("    ... %d more interface changes", remaining))
	}
	return lines
}

func appendPolicyImpactLines(lines []string, policyChanges []changeImpactPolicyChange, bgpBindings []changeImpactBGPPolicyBinding) []string {
	if len(policyChanges) == 0 && len(bgpBindings) == 0 {
		return lines
	}
	lines = append(lines, "  policy diff:")
	remainingSlots := maxChangeImpactPolicyDetails
	for _, change := range policyChanges {
		if remainingSlots == 0 {
			break
		}
		lines = append(lines, "    "+change.summary())
		remainingSlots--
	}
	for _, binding := range bgpBindings {
		if remainingSlots == 0 {
			break
		}
		lines = append(lines, "    "+binding.summary())
		remainingSlots--
	}
	if remaining := len(policyChanges) + len(bgpBindings) - maxChangeImpactPolicyDetails; remaining > 0 {
		lines = append(lines, fmt.Sprintf("    ... %d more policy changes", remaining))
	}
	return lines
}

func appendStaticRouteImpactLines(lines []string, changes []changeImpactRouteChange) []string {
	if len(changes) == 0 {
		return lines
	}
	lines = append(lines, "  route diff:")
	limit := len(changes)
	if limit > maxChangeImpactRouteDetails {
		limit = maxChangeImpactRouteDetails
	}
	for _, change := range changes[:limit] {
		lines = append(lines, "    "+change.summary())
	}
	if remaining := len(changes) - limit; remaining > 0 {
		lines = append(lines, fmt.Sprintf("    ... %d more static route changes", remaining))
	}
	return lines
}

func appendChangeImpactLine(lines []string, label string, count changeImpactLineCount) []string {
	if !count.hasChanges() {
		return lines
	}
	return append(lines, fmt.Sprintf("  %s: +%d -%d", label, count.added, count.removed))
}

func (c changeImpactLineCount) hasChanges() bool {
	return c.added > 0 || c.removed > 0
}

func (c *changeImpactLineCount) add(sign byte) {
	if sign == '+' {
		c.added++
	} else {
		c.removed++
	}
}

func (c changeImpactInterfaceChange) summary() string {
	action := "add"
	if c.sign == '-' {
		action = "remove"
	}
	if c.value == "" {
		return fmt.Sprintf("%s interface %s %s", action, c.name, c.operation)
	}
	return fmt.Sprintf("%s interface %s %s %s", action, c.name, c.operation, c.value)
}

func (c changeImpactRouteChange) summary() string {
	action := "add"
	if c.sign == '-' {
		action = "remove"
	}
	target := c.prefix
	if c.routingInstance != "" {
		target = fmt.Sprintf("routing-instance %s %s", c.routingInstance, target)
	}
	if c.nextHop == "" {
		return fmt.Sprintf("%s %s", action, target)
	}
	return fmt.Sprintf("%s %s via %s", action, target, c.nextHop)
}

func (c changeImpactPolicyChange) summary() string {
	action := "add"
	if c.sign == '-' {
		action = "remove"
	}
	switch c.kind {
	case "prefix-list":
		if c.prefix != "" {
			return fmt.Sprintf("%s prefix-list %s %s", action, c.name, c.prefix)
		}
		return fmt.Sprintf("%s prefix-list %s", action, c.name)
	case "route-map":
		if c.term != "" {
			return fmt.Sprintf("%s route-map %s term %s", action, c.name, c.term)
		}
		return fmt.Sprintf("%s route-map %s", action, c.name)
	default:
		return fmt.Sprintf("%s policy %s", action, c.name)
	}
}

func (c changeImpactBGPPolicyBinding) summary() string {
	action := "add"
	if c.sign == '-' {
		action = "remove"
	}
	return fmt.Sprintf("%s bgp group %s %s route-map %s", action, c.groupName, c.direction, c.policy)
}

func formatClassOfServicePreflight(info *grpcclient.ClassOfServiceInfo) []string {
	if info == nil || info.Capabilities == nil {
		return []string{"qos preflight: capability snapshot unavailable"}
	}
	capabilities := info.Capabilities
	lines := []string{
		"qos preflight:",
		fmt.Sprintf("  metadata binding: %s", yesNo(capabilities.MetadataBindingSupported)),
		fmt.Sprintf("  queue scheduler: %s", yesNo(capabilities.QueueSchedulerSupported)),
		fmt.Sprintf("  policer: %s", yesNo(capabilities.PolicerSupported)),
		fmt.Sprintf("  counters: %s", yesNo(capabilities.CountersSupported)),
	}
	if capabilities.LastError != "" {
		lines = append(lines, "  warning: capability detection error: "+capabilities.LastError)
	}
	if !capabilities.MetadataBindingSupported {
		lines = append(lines, "  warning: metadata binding is unavailable; QoS intent may not persist on VPP interfaces")
	}
	if !capabilities.QueueSchedulerSupported {
		lines = append(lines, "  warning: queue scheduler is unavailable; output QoS remains intent-only")
	}
	if !capabilities.PolicerSupported {
		lines = append(lines, "  warning: policer is unavailable; traffic policing remains intent-only")
	}
	for _, diagnostic := range capabilities.Diagnostics {
		if diagnostic == "" {
			continue
		}
		lines = append(lines, "  diagnostic: "+diagnostic)
	}
	return lines
}

func formatClassOfServicePostCommit(info *grpcclient.ClassOfServiceInfo) []string {
	if info == nil {
		return []string{"qos post-commit diagnostics: class-of-service status unavailable"}
	}
	lines := []string{
		"qos post-commit diagnostics:",
		fmt.Sprintf("  enforcement status: %s", displayValue(info.EnforcementStatus, "unknown")),
		fmt.Sprintf("  bound interfaces: %d", len(info.Interfaces)),
	}
	if info.Capabilities == nil {
		return append(lines, "  warning: capability snapshot unavailable")
	}
	capabilities := info.Capabilities
	lines = append(lines,
		fmt.Sprintf("  metadata binding: %s", yesNo(capabilities.MetadataBindingSupported)),
		fmt.Sprintf("  queue scheduler: %s", yesNo(capabilities.QueueSchedulerSupported)),
		fmt.Sprintf("  policer: %s", yesNo(capabilities.PolicerSupported)),
		fmt.Sprintf("  counters: %s", yesNo(capabilities.CountersSupported)),
	)
	if capabilities.LastError != "" {
		lines = append(lines, "  warning: capability detection error: "+capabilities.LastError)
	}
	for _, diagnostic := range capabilities.Diagnostics {
		if diagnostic == "" {
			continue
		}
		lines = append(lines, "  diagnostic: "+diagnostic)
	}
	return lines
}

func displayValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func analyzeChangeImpact(diffText string) changeImpactPreview {
	var preview changeImpactPreview
	for _, rawLine := range strings.Split(diffText, "\n") {
		line := strings.TrimSpace(rawLine)
		if len(line) < 3 {
			continue
		}
		sign := line[0]
		if sign != '+' && sign != '-' {
			continue
		}
		configLine := strings.TrimSpace(line[1:])
		if configLine == "" {
			continue
		}
		if sign == '+' {
			preview.addedLines++
		} else {
			preview.removedLines++
		}
		if strings.HasPrefix(configLine, "set interfaces ") {
			preview.interfaces.add(sign)
			if change, ok := parseInterfaceImpactChange(sign, configLine); ok {
				preview.interfaceChanges = append(preview.interfaceChanges, change)
				if change.operation == "address" {
					preview.interfaceAddressChange = true
				}
			}
		}
		if isStaticRouteConfigLine(configLine) {
			preview.staticRoutes.add(sign)
			if change, ok := parseStaticRouteImpactChange(sign, configLine); ok {
				preview.staticRouteChanges = append(preview.staticRouteChanges, change)
			}
			if isDefaultRouteConfigLine(configLine) {
				preview.defaultRouteChange = true
			}
		}
		if strings.HasPrefix(configLine, "set policy-options ") {
			preview.policyOptions.add(sign)
			if change, ok := parsePolicyImpactChange(sign, configLine); ok {
				preview.policyChanges = append(preview.policyChanges, change)
			}
		}
		if strings.HasPrefix(configLine, "set protocols bgp ") {
			preview.bgp.add(sign)
			if binding, ok := parseBGPPolicyImpactBinding(sign, configLine); ok {
				preview.bgpPolicyBindings = append(preview.bgpPolicyBindings, binding)
			}
		}
		if strings.HasPrefix(configLine, "set protocols ospf ") || strings.HasPrefix(configLine, "set protocols ospf3 ") {
			preview.ospf.add(sign)
		}
		if strings.HasPrefix(configLine, "set protocols bfd ") {
			preview.bfd.add(sign)
		}
		if strings.HasPrefix(configLine, "set protocols evpn ") {
			preview.evpn.add(sign)
		}
		if strings.HasPrefix(configLine, "set routing-instances ") {
			preview.routingInstances.add(sign)
		}
		if strings.HasPrefix(configLine, "set class-of-service ") {
			preview.classOfService.add(sign)
		}
	}
	return preview
}

func isStaticRouteConfigLine(line string) bool {
	return strings.HasPrefix(line, "set routing-options static route ") ||
		(strings.HasPrefix(line, "set routing-instances ") && strings.Contains(line, " routing-options static route "))
}

func isDefaultRouteConfigLine(line string) bool {
	return strings.Contains(line, " static route 0.0.0.0/0 ") ||
		strings.Contains(line, " static route ::/0 ")
}

func parseStaticRouteImpactChange(sign byte, line string) (changeImpactRouteChange, bool) {
	tokens := tokenize(line)
	if len(tokens) == 0 || tokens[0] != "set" {
		return changeImpactRouteChange{}, false
	}

	change := changeImpactRouteChange{sign: sign}
	if len(tokens) > 2 && tokens[1] == "routing-instances" {
		change.routingInstance = tokens[2]
	}

	for i := 1; i+3 < len(tokens); i++ {
		if tokens[i] != "static" || tokens[i+1] != "route" {
			continue
		}
		change.prefix = tokens[i+2]
		for j := i + 3; j+1 < len(tokens); j++ {
			if tokens[j] == "next-hop" {
				change.nextHop = tokens[j+1]
				break
			}
		}
		return change, change.prefix != ""
	}
	return changeImpactRouteChange{}, false
}

func parseInterfaceImpactChange(sign byte, line string) (changeImpactInterfaceChange, bool) {
	tokens := tokenize(line)
	if len(tokens) < 3 || tokens[0] != "set" || tokens[1] != "interfaces" {
		return changeImpactInterfaceChange{}, false
	}
	change := changeImpactInterfaceChange{
		sign:      sign,
		name:      tokens[2],
		operation: "configuration",
	}
	for i := 3; i < len(tokens); i++ {
		switch tokens[i] {
		case "address":
			change.operation = "address"
			if i+1 < len(tokens) {
				change.value = tokens[i+1]
			}
			return change, change.name != ""
		case "description", "mtu", "speed":
			change.operation = tokens[i]
			if i+1 < len(tokens) {
				change.value = tokens[i+1]
			}
			return change, change.name != ""
		case "disable":
			change.operation = "disable"
			return change, change.name != ""
		}
	}
	if len(tokens) > 3 {
		change.operation = strings.Join(tokens[3:], " ")
	}
	return change, change.name != ""
}

func parsePolicyImpactChange(sign byte, line string) (changeImpactPolicyChange, bool) {
	tokens := tokenize(line)
	if len(tokens) < 4 || tokens[0] != "set" || tokens[1] != "policy-options" {
		return changeImpactPolicyChange{}, false
	}
	switch tokens[2] {
	case "prefix-list":
		change := changeImpactPolicyChange{sign: sign, kind: "prefix-list", name: tokens[3]}
		if len(tokens) > 4 {
			change.prefix = tokens[4]
		}
		return change, change.name != ""
	case "policy-statement":
		change := changeImpactPolicyChange{sign: sign, kind: "route-map", name: tokens[3]}
		for i := 4; i+1 < len(tokens); i++ {
			if tokens[i] == "term" {
				change.term = tokens[i+1]
				break
			}
		}
		return change, change.name != ""
	default:
		return changeImpactPolicyChange{}, false
	}
}

func parseBGPPolicyImpactBinding(sign byte, line string) (changeImpactBGPPolicyBinding, bool) {
	tokens := tokenize(line)
	if len(tokens) < 7 || tokens[0] != "set" || tokens[1] != "protocols" || tokens[2] != "bgp" || tokens[3] != "group" {
		return changeImpactBGPPolicyBinding{}, false
	}
	direction := tokens[5]
	if direction != "import" && direction != "export" {
		return changeImpactBGPPolicyBinding{}, false
	}
	binding := changeImpactBGPPolicyBinding{
		sign:      sign,
		groupName: tokens[4],
		direction: direction,
		policy:    tokens[6],
	}
	return binding, binding.groupName != "" && binding.policy != ""
}
