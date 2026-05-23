package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	sbfrr "github.com/akam1o/arca-router/internal/southbound/frr"
)

func newWebStatus(metrics routerMetrics) webStatus {
	return webStatus{
		Version:         Version,
		Commit:          Commit,
		BuildDate:       BuildDate,
		UptimeSeconds:   metrics.UptimeSeconds,
		ConfigVersion:   metrics.ConfigVersion,
		RunningHostname: metrics.RunningHostname,
		Datastore: webDatastore{
			Backend:       metrics.DatastoreBackend,
			EtcdEndpoints: metrics.DatastoreEtcdEndpoints,
		},
		ConfigSync: webConfigSync{
			Enabled:         metrics.ConfigSyncEnabled,
			Healthy:         metrics.ConfigSyncHealthy,
			EtcdRevision:    metrics.ConfigSyncEtcdRevision,
			RunningRevision: metrics.ConfigSyncRunningRevision,
			RunningCommitID: metrics.ConfigSyncCommitID,
			LastCheck:       formatWebOptionalTime(metrics.ConfigSyncLastCheck),
			LastApply:       formatWebOptionalTime(metrics.ConfigSyncLastApply),
			LastError:       metrics.ConfigSyncLastError,
		},
		Cluster: webCluster{
			Enabled:            metrics.ClusterEnabled,
			NodeCount:          metrics.ClusterNodeCount,
			EtcdSyncConfigured: metrics.ClusterEtcdSync,
			EtcdEndpoints:      metrics.ClusterEtcdEndpoints,
			SyncAligned:        metrics.ClusterSyncAligned,
		},
		Overlay: webOverlayStats{
			EVPN: webEVPNStats{
				Configured:    metrics.OverlayEVPNConfigured,
				VNIs:          metrics.OverlayEVPNVNIs,
				L2VNIs:        metrics.OverlayEVPNL2VNIs,
				L3VNIs:        metrics.OverlayEVPNL3VNIs,
				MulticastVNIs: metrics.OverlayEVPNMulticastVNIs,
			},
		},
		HA: webHAStats{
			Configured: metrics.HAConfigured,
			Converged:  metrics.HAConverged,
			VRRPGroups: metrics.HAVRPGroups,
			IssueCount: len(metrics.HAIssues),
			Issues:     append([]string(nil), metrics.HAIssues...),
		},
		ClassOfService: webCoSStats{
			Configured:             metrics.ClassOfServiceConfigured,
			EnforcementStatus:      metrics.ClassOfServiceStatus,
			ForwardingClasses:      metrics.ClassOfServiceClasses,
			TrafficControlProfiles: metrics.ClassOfServiceProfiles,
			InterfaceBindings:      metrics.ClassOfServiceBindings,
			IntentOnly:             metrics.ClassOfServiceIntentOnly,
			Capabilities: webCoSCapabilities{
				LastCheck:                formatWebOptionalTime(metrics.ClassOfServiceCapabilityLastCheck),
				MetadataBindingSupported: metrics.ClassOfServiceMetadataBindingSupported,
				QueueSchedulerSupported:  metrics.ClassOfServiceQueueSchedulerSupported,
				PolicerSupported:         metrics.ClassOfServicePolicerSupported,
				CountersSupported:        metrics.ClassOfServiceCountersSupported,
				Diagnostics:              append([]string(nil), metrics.ClassOfServiceCapabilityDiagnostics...),
				LastError:                metrics.ClassOfServiceCapabilityError,
			},
		},
		FRR: webFRRStats{
			VRRP: webVRRPStats{
				LastCheck:        formatWebOptionalTime(metrics.FRRVRRPLastRun),
				ConfiguredGroups: metrics.FRRVRRPConfiguredGroups,
				ObservedGroups:   metrics.FRRVRRPObservedGroups,
				ActiveGroups:     metrics.FRRVRRPActiveGroups,
				Groups:           webVRRPGroups(metrics.FRRVRRPGroups),
				IssueCount:       len(metrics.FRRVRRPIssues),
				Issues:           append([]string(nil), metrics.FRRVRRPIssues...),
				LastError:        metrics.FRRVRRPError,
			},
			BFD: webBFDStats{
				LastCheck:         formatWebOptionalTime(metrics.FRRBFDLastRun),
				ConfiguredPeers:   metrics.FRRBFDConfiguredPeers,
				ObservedPeers:     metrics.FRRBFDObservedPeers,
				UpPeers:           metrics.FRRBFDUpPeers,
				DownPeers:         metrics.FRRBFDDownPeers,
				SessionDownEvents: metrics.FRRBFDSessionDownEvents,
				RxFailPackets:     metrics.FRRBFDRxFailPackets,
				Peers:             webBFDPeers(metrics.FRRBFDPeers),
				IssueCount:        len(metrics.FRRBFDIssues),
				Issues:            append([]string(nil), metrics.FRRBFDIssues...),
				LastError:         metrics.FRRBFDError,
			},
		},
		VPP: webVPPStats{
			LCP: webLCPSyncStats{
				LastReconcile:      formatWebOptionalTime(metrics.VPPLCPReconcileLastRun),
				PairCount:          metrics.VPPLCPPairs,
				InconsistencyCount: len(metrics.VPPLCPInconsistencies),
				Inconsistencies:    metrics.VPPLCPInconsistencies,
				LastError:          metrics.VPPLCPReconcileError,
			},
		},
		NETCONF: webNETCONFStats{
			Listening:         metrics.NETCONFListening,
			ActiveSessions:    metrics.NETCONFActiveSessions,
			ActiveConnections: metrics.NETCONFActiveConns,
			TotalConnections:  metrics.NETCONFTotalConns,
			SuccessfulAuth:    metrics.NETCONFSuccess,
			FailedAuth:        metrics.NETCONFFailures,
		},
	}
}

func webVRRPGroups(groups []sbfrr.VRRPGroupOperationalStatus) []webVRRPGroupStats {
	result := make([]webVRRPGroupStats, 0, len(groups))
	for _, group := range groups {
		result = append(result, webVRRPGroupStats{
			Interface:      group.Interface,
			ID:             group.ID,
			VirtualAddress: group.VirtualAddress,
			State:          group.State,
			Observed:       group.Observed,
			Active:         group.Active,
		})
	}
	return result
}

func webBFDPeers(peers []sbfrr.BFDPeerOperationalStatus) []webBFDPeerStats {
	result := make([]webBFDPeerStats, 0, len(peers))
	for _, peer := range peers {
		result = append(result, webBFDPeerStats{
			Peer:              peer.Peer,
			LocalAddress:      peer.LocalAddress,
			Interface:         peer.Interface,
			VRF:               peer.VRF,
			Status:            peer.Status,
			Diagnostic:        peer.Diagnostic,
			RemoteDiagnostic:  peer.RemoteDiagnostic,
			Observed:          peer.Observed,
			Up:                peer.Up,
			SessionDownEvents: peer.SessionDownEvents,
			RxFailPackets:     peer.RxFailPackets,
		})
	}
	return result
}

func newWebIndexData(status webStatus, now time.Time, runningConfig string, history []webCommitEntry) webIndexData {
	state := "Stopped"
	stateClass := "warn"
	if status.NETCONF.Listening {
		state = "Listening"
		stateClass = "ok"
	}
	clusterState := "Disabled"
	clusterStateClass := "neutral"
	if status.Cluster.Enabled {
		clusterState = "Enabled"
		clusterStateClass = "ok"
	}
	clusterSyncState := "Not configured"
	clusterSyncAlignment := "Not applicable"
	if status.Cluster.EtcdSyncConfigured {
		clusterSyncState = "etcd"
		clusterSyncAlignment = "Aligned"
		if !status.Cluster.SyncAligned {
			clusterSyncAlignment = "Mismatch"
		}
	}
	configSyncState := "Disabled"
	configSyncStateClass := "neutral"
	if status.ConfigSync.Enabled {
		configSyncState = "Healthy"
		configSyncStateClass = "ok"
		if status.ConfigSync.LastError != "" {
			configSyncState = "Error"
			configSyncStateClass = "warn"
		} else if !status.ConfigSync.Healthy {
			configSyncState = "Unknown"
			configSyncStateClass = "neutral"
		}
	}
	configSyncRevision := "n/a"
	if status.ConfigSync.RunningRevision > 0 {
		configSyncRevision = strconv.FormatInt(status.ConfigSync.RunningRevision, 10)
	}
	haState := "Not configured"
	haStateClass := "neutral"
	if status.HA.Configured {
		haState = "Converged"
		haStateClass = "ok"
		if !status.HA.Converged {
			haState = "Issues"
			haStateClass = "warn"
		}
	}
	cosState := "Not configured"
	cosStateClass := "neutral"
	if status.ClassOfService.Configured {
		cosState = status.ClassOfService.EnforcementStatus
		cosStateClass = "ok"
		if status.ClassOfService.IntentOnly {
			cosStateClass = "neutral"
		}
	}
	frrVRRPState := "Not configured"
	frrVRRPStateClass := "neutral"
	if status.FRR.VRRP.ConfiguredGroups > 0 {
		frrVRRPState = "Converged"
		frrVRRPStateClass = "ok"
		if status.FRR.VRRP.LastError != "" || status.FRR.VRRP.IssueCount > 0 ||
			status.FRR.VRRP.ActiveGroups < status.FRR.VRRP.ConfiguredGroups {
			frrVRRPState = "Issues"
			frrVRRPStateClass = "warn"
		} else if status.FRR.VRRP.LastCheck == "" {
			frrVRRPState = "Unknown"
			frrVRRPStateClass = "neutral"
		}
	}
	frrBFDState := "Not configured"
	frrBFDStateClass := "neutral"
	if status.FRR.BFD.ConfiguredPeers > 0 || status.FRR.BFD.ObservedPeers > 0 {
		frrBFDState = "Converged"
		frrBFDStateClass = "ok"
		if status.FRR.BFD.LastError != "" || status.FRR.BFD.IssueCount > 0 ||
			status.FRR.BFD.DownPeers > 0 || status.FRR.BFD.UpPeers < status.FRR.BFD.ConfiguredPeers {
			frrBFDState = "Issues"
			frrBFDStateClass = "warn"
		} else if status.FRR.BFD.LastCheck == "" {
			frrBFDState = "Unknown"
			frrBFDStateClass = "neutral"
		}
	}
	vppLCPState := "Consistent"
	vppLCPStateClass := "ok"
	if status.VPP.LCP.LastError != "" {
		vppLCPState = "Check failed"
		vppLCPStateClass = "warn"
	} else if status.VPP.LCP.InconsistencyCount > 0 {
		vppLCPState = "Mismatch"
		vppLCPStateClass = "warn"
	} else if status.VPP.LCP.LastReconcile == "" {
		vppLCPState = "Unknown"
		vppLCPStateClass = "neutral"
	}

	return webIndexData{
		Status:                   status,
		Uptime:                   formatWebUptime(status.UptimeSeconds),
		NETCONFState:             state,
		NETCONFStateClass:        stateClass,
		NETCONFConnections:       strconv.FormatUint(status.NETCONF.TotalConnections, 10),
		ClusterState:             clusterState,
		ClusterStateClass:        clusterStateClass,
		ClusterSyncState:         clusterSyncState,
		ClusterSyncAlignment:     clusterSyncAlignment,
		ClusterNodeCount:         strconv.Itoa(status.Cluster.NodeCount),
		ConfigSyncState:          configSyncState,
		ConfigSyncStateClass:     configSyncStateClass,
		ConfigSyncRevision:       configSyncRevision,
		ConfigSyncLastApply:      formatWebOptionalDisplayTime(status.ConfigSync.LastApply),
		HAState:                  haState,
		HAStateClass:             haStateClass,
		HAVRPGroups:              strconv.Itoa(status.HA.VRRPGroups),
		HAIssues:                 strconv.Itoa(status.HA.IssueCount),
		ClassOfServiceState:      cosState,
		ClassOfServiceClass:      cosStateClass,
		ClassOfServiceProfiles:   strconv.Itoa(status.ClassOfService.TrafficControlProfiles),
		ClassOfServiceBindings:   strconv.Itoa(status.ClassOfService.InterfaceBindings),
		ClassOfServiceClasses:    strconv.Itoa(status.ClassOfService.ForwardingClasses),
		ClassOfServiceScheduler:  webSupportedStatus(status.ClassOfService.Capabilities.QueueSchedulerSupported),
		ClassOfServicePolicer:    webSupportedStatus(status.ClassOfService.Capabilities.PolicerSupported),
		ClassOfServiceCounters:   webSupportedStatus(status.ClassOfService.Capabilities.CountersSupported),
		ClassOfServiceDiagnostic: webCoSDiagnosticText(status.ClassOfService.Capabilities),
		FRRVRRPState:             frrVRRPState,
		FRRVRRPStateClass:        frrVRRPStateClass,
		FRRVRRPActiveGroups:      fmt.Sprintf("%d/%d", status.FRR.VRRP.ActiveGroups, status.FRR.VRRP.ConfiguredGroups),
		FRRVRRPGroups:            webVRRPGroupViews(status.FRR.VRRP.Groups),
		FRRBFDState:              frrBFDState,
		FRRBFDStateClass:         frrBFDStateClass,
		FRRBFDUpPeers:            webBFDPeerRatio(status.FRR.BFD),
		FRRBFDSessionDownEvents:  strconv.Itoa(status.FRR.BFD.SessionDownEvents),
		FRRBFDRxFailPackets:      strconv.Itoa(status.FRR.BFD.RxFailPackets),
		FRRBFDPeers:              webBFDPeerViews(status.FRR.BFD.Peers),
		VPPLCPState:              vppLCPState,
		VPPLCPStateClass:         vppLCPStateClass,
		VPPLCPPairs:              strconv.Itoa(status.VPP.LCP.PairCount),
		VPPLCPInconsistencies:    strconv.Itoa(status.VPP.LCP.InconsistencyCount),
		VPPLCPLastReconcile:      formatWebOptionalDisplayTime(status.VPP.LCP.LastReconcile),
		DatastoreBackend:         status.Datastore.Backend,
		GeneratedAt:              now.UTC().Format(time.RFC3339),
		ConfigVersionString:      strconv.FormatUint(status.ConfigVersion, 10),
		RunningConfig:            runningConfig,
		History:                  history,
	}
}

func webVRRPGroupViews(groups []webVRRPGroupStats) []webVRRPGroupView {
	result := make([]webVRRPGroupView, 0, len(groups))
	for _, group := range groups {
		state := group.State
		if state == "" {
			state = "unknown"
		}
		result = append(result, webVRRPGroupView{
			Label:      fmt.Sprintf("%s vrid %d", group.Interface, group.ID),
			State:      state,
			StateClass: webVRRPGroupStateClass(group),
		})
	}
	return result
}

func webVRRPGroupStateClass(group webVRRPGroupStats) string {
	if group.Active {
		return "ok"
	}
	return "warn"
}

func webBFDPeerViews(peers []webBFDPeerStats) []webBFDPeerView {
	result := make([]webBFDPeerView, 0, len(peers))
	for _, peer := range peers {
		state := peer.Status
		if state == "" {
			state = "unknown"
		}
		result = append(result, webBFDPeerView{
			Label:      webBFDPeerLabel(peer),
			State:      state,
			StateClass: webBFDPeerStateClass(peer),
			Counters:   webBFDCounterText(peer),
		})
	}
	return result
}

func webBFDPeerRatio(status webBFDStats) string {
	total := status.ConfiguredPeers
	if total == 0 {
		total = status.ObservedPeers
	}
	return fmt.Sprintf("%d/%d", status.UpPeers, total)
}

func webBFDPeerLabel(peer webBFDPeerStats) string {
	parts := []string{"bfd", peer.Peer}
	if peer.Interface != "" {
		parts = append(parts, peer.Interface)
	}
	if peer.VRF != "" {
		parts = append(parts, "vrf "+peer.VRF)
	}
	return strings.Join(parts, " ")
}

func webBFDPeerStateClass(peer webBFDPeerStats) string {
	if peer.Up {
		return "ok"
	}
	return "warn"
}

func webBFDCounterText(peer webBFDPeerStats) string {
	if peer.SessionDownEvents == 0 && peer.RxFailPackets == 0 {
		return ""
	}
	return fmt.Sprintf("down %d / rx-fail %d", peer.SessionDownEvents, peer.RxFailPackets)
}

func formatWebUptime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	duration := time.Duration(seconds) * time.Second
	days := duration / (24 * time.Hour)
	duration -= days * 24 * time.Hour
	hours := duration / time.Hour
	duration -= hours * time.Hour
	minutes := duration / time.Minute

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
