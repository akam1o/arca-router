package model

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
)

// RoutingInstanceTablePlan describes the deterministic VPP table plan for a routing instance.
type RoutingInstanceTablePlan struct {
	Name       string
	TableID    uint32
	Interfaces []string
}

// RoutingInstanceTablePlans derives VPP table plans for routing instances.
func RoutingInstanceTablePlans(instances map[string]*RoutingInstance) (map[string]RoutingInstanceTablePlan, error) {
	plans := make(map[string]RoutingInstanceTablePlan)
	if len(instances) == 0 {
		return plans, nil
	}

	names := make([]string, 0, len(instances))
	for name := range instances {
		names = append(names, name)
	}
	sort.Strings(names)

	usedTables := make(map[uint32]string)
	for _, name := range names {
		instance := instances[name]
		if instance == nil {
			continue
		}
		if instance.InstanceType != "" && instance.InstanceType != "vrf" {
			return nil, fmt.Errorf("routing-instance %s: unsupported instance-type %q", name, instance.InstanceType)
		}
		tableID, explicit, err := RoutingInstanceTableID(name, instance)
		if err != nil {
			return nil, fmt.Errorf("routing-instance %s: %w", name, err)
		}
		for {
			if owner, exists := usedTables[tableID]; exists {
				if explicit {
					return nil, fmt.Errorf("routing-instance %s: table ID %d collides with %s", name, tableID, owner)
				}
				tableID++
				if tableID == 0 {
					return nil, fmt.Errorf("routing-instance %s: no usable VPP table ID", name)
				}
				continue
			}
			break
		}
		usedTables[tableID] = name
		plans[name] = RoutingInstanceTablePlan{
			Name:       name,
			TableID:    tableID,
			Interfaces: uniqueSortedStrings(instance.Interfaces),
		}
	}
	return plans, nil
}

// RoutingInstanceTableID derives the base VPP table ID for one routing instance.
func RoutingInstanceTableID(name string, instance *RoutingInstance) (uint32, bool, error) {
	if instance != nil && instance.RouteDistinguisher != "" {
		parts := strings.Split(instance.RouteDistinguisher, ":")
		if len(parts) != 2 {
			return 0, true, fmt.Errorf("route-distinguisher %q must use ASN:number format", instance.RouteDistinguisher)
		}
		value, err := strconv.ParseUint(parts[1], 10, 32)
		if err != nil || value == 0 {
			return 0, true, fmt.Errorf("route-distinguisher %q does not provide a usable VPP table ID", instance.RouteDistinguisher)
		}
		return uint32(value), true, nil
	}

	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))
	return 100000 + hash.Sum32()%900000, false, nil
}

func uniqueSortedStrings(values []string) []string {
	set := make(map[string]bool)
	for _, value := range values {
		if value != "" {
			set[value] = true
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
