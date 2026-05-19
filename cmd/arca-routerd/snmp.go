package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/akam1o/arca-router/internal/model"
	"github.com/gosnmp/gosnmp"
	snmpserver "github.com/slayercat/GoSNMPServer"

	"github.com/akam1o/arca-router/pkg/logger"
)

const (
	arcaSNMPBaseOID = "1.3.6.1.3.9950.1"

	snmpOIDSysDescr          = "1.3.6.1.2.1.1.1.0"
	snmpOIDSysObjectID       = "1.3.6.1.2.1.1.2.0"
	snmpOIDSysUpTime         = "1.3.6.1.2.1.1.3.0"
	snmpOIDSysName           = "1.3.6.1.2.1.1.5.0"
	snmpOIDDaemonUp          = arcaSNMPBaseOID + ".1.0"
	snmpOIDDaemonUptime      = arcaSNMPBaseOID + ".2.0"
	snmpOIDConfigVersion     = arcaSNMPBaseOID + ".3.0"
	snmpOIDNETCONFListen     = arcaSNMPBaseOID + ".4.0"
	snmpOIDNETCONFSessions   = arcaSNMPBaseOID + ".5.0"
	snmpOIDNETCONFConns      = arcaSNMPBaseOID + ".6.0"
	snmpOIDNETCONFTotal      = arcaSNMPBaseOID + ".7.0"
	snmpOIDNETCONFSuccess    = arcaSNMPBaseOID + ".8.0"
	snmpOIDNETCONFFailures   = arcaSNMPBaseOID + ".9.0"
	snmpOIDDaemonVersion     = arcaSNMPBaseOID + ".10.0"
	snmpOIDVPPLCPPairs       = arcaSNMPBaseOID + ".11.0"
	snmpOIDVPPLCPMismatch    = arcaSNMPBaseOID + ".12.0"
	snmpOIDVPPLCPError       = arcaSNMPBaseOID + ".13.0"
	snmpOIDVPPLCPLastRun     = arcaSNMPBaseOID + ".14.0"
	snmpOIDHAConfigured      = arcaSNMPBaseOID + ".15.0"
	snmpOIDHAConverged       = arcaSNMPBaseOID + ".16.0"
	snmpOIDHAVRPGroups       = arcaSNMPBaseOID + ".17.0"
	snmpOIDHAIssues          = arcaSNMPBaseOID + ".18.0"
	snmpOIDFRRVRRPConfigured = arcaSNMPBaseOID + ".19.0"
	snmpOIDFRRVRRPObserved   = arcaSNMPBaseOID + ".20.0"
	snmpOIDFRRVRRPActive     = arcaSNMPBaseOID + ".21.0"
	snmpOIDFRRVRRPIssues     = arcaSNMPBaseOID + ".22.0"
	snmpOIDFRRVRRPError      = arcaSNMPBaseOID + ".23.0"
	snmpOIDFRRVRRPLastRun    = arcaSNMPBaseOID + ".24.0"
	snmpOIDCoSConfigured     = arcaSNMPBaseOID + ".25.0"
	snmpOIDCoSClasses        = arcaSNMPBaseOID + ".26.0"
	snmpOIDCoSProfiles       = arcaSNMPBaseOID + ".27.0"
	snmpOIDCoSBindings       = arcaSNMPBaseOID + ".28.0"
	snmpOIDCoSIntentOnly     = arcaSNMPBaseOID + ".29.0"
	snmpOIDFRRBFDConfigured  = arcaSNMPBaseOID + ".30.0"
	snmpOIDFRRBFDObserved    = arcaSNMPBaseOID + ".31.0"
	snmpOIDFRRBFDUp          = arcaSNMPBaseOID + ".32.0"
	snmpOIDFRRBFDDownPeers   = arcaSNMPBaseOID + ".33.0"
	snmpOIDFRRBFDSessionDown = arcaSNMPBaseOID + ".34.0"
	snmpOIDFRRBFDRxFail      = arcaSNMPBaseOID + ".35.0"
	snmpOIDFRRBFDIssues      = arcaSNMPBaseOID + ".36.0"
	snmpOIDFRRBFDError       = arcaSNMPBaseOID + ".37.0"
	snmpOIDFRRBFDLastRun     = arcaSNMPBaseOID + ".38.0"
	snmpOIDEVPNConfigured    = arcaSNMPBaseOID + ".39.0"
	snmpOIDEVPNVNIs          = arcaSNMPBaseOID + ".40.0"
	snmpOIDEVPNL2VNIs        = arcaSNMPBaseOID + ".41.0"
	snmpOIDEVPNL3VNIs        = arcaSNMPBaseOID + ".42.0"
	snmpOIDEVPNMulticastVNIs = arcaSNMPBaseOID + ".43.0"
	snmpOIDCoSMetadata       = arcaSNMPBaseOID + ".44.0"
	snmpOIDCoSScheduler      = arcaSNMPBaseOID + ".45.0"
	snmpOIDCoSPolicer        = arcaSNMPBaseOID + ".46.0"
	snmpOIDCoSCounters       = arcaSNMPBaseOID + ".47.0"
	snmpOIDCoSCapabilityErr  = arcaSNMPBaseOID + ".48.0"
	snmpOIDCoSCapabilityLast = arcaSNMPBaseOID + ".49.0"

	defaultSNMPPort = 161
)

func effectiveSNMPListen(flagValue string, snapshot *model.ConfigSnapshot) string {
	if listen := strings.TrimSpace(flagValue); listen != "" {
		return listen
	}
	snmp := snapshotSNMPConfig(snapshot)
	if snmp == nil || !snmp.Enabled {
		return ""
	}
	addr := strings.TrimSpace(snmp.ListenAddress)
	if addr == "" {
		addr = "127.0.0.1"
	}
	port := snmp.Port
	if port == 0 {
		port = defaultSNMPPort
	}
	return net.JoinHostPort(addr, strconv.Itoa(port))
}

func effectiveSNMPCommunity(flagValue string, snapshot *model.ConfigSnapshot) string {
	if community := strings.TrimSpace(flagValue); community != "" {
		return community
	}
	if snmp := snapshotSNMPConfig(snapshot); snmp != nil && strings.TrimSpace(snmp.Community) != "" {
		return strings.TrimSpace(snmp.Community)
	}
	return ""
}

func snapshotSNMPConfig(snapshot *model.ConfigSnapshot) *model.SNMPConfig {
	if snapshot == nil || snapshot.Config == nil || snapshot.Config.System == nil ||
		snapshot.Config.System.Services == nil {
		return nil
	}
	return snapshot.Config.System.Services.SNMP
}

func startSNMPServer(ctx context.Context, listenAddr, community string, source metricsSource, log *logger.Logger) (<-chan error, error) {
	if community == "" {
		return nil, fmt.Errorf("SNMP community must not be empty")
	}

	server := newSNMPServer(source, community)
	if err := server.ListenUDP("udp", listenAddr); err != nil {
		return nil, fmt.Errorf("listen SNMP endpoint: %w", err)
	}

	errCh := make(chan error, 1)
	go func() {
		if log != nil {
			log.Info("SNMP endpoint started", slog.String("listen", server.Address().String()))
		}
		errCh <- server.ServeForever()
	}()

	go func() {
		<-ctx.Done()
		server.Shutdown()
	}()

	return errCh, nil
}

func newSNMPServer(source metricsSource, community string) *snmpserver.SNMPServer {
	master := snmpserver.MasterAgent{
		Logger: snmpserver.NewDiscardLogger(),
		SubAgents: []*snmpserver.SubAgent{
			{
				CommunityIDs: []string{community},
				OIDs:         snmpOIDs(source),
			},
		},
	}
	return snmpserver.NewSNMPServer(master)
}

func snmpOIDs(source metricsSource) []*snmpserver.PDUValueControlItem {
	return []*snmpserver.PDUValueControlItem{
		{
			OID:      snmpOIDSysDescr,
			Type:     gosnmp.OctetString,
			OnGet:    func() (interface{}, error) { return snmpserver.Asn1OctetStringWrap("arca-routerd " + Version), nil },
			Document: "sysDescr",
		},
		{
			OID:      snmpOIDSysObjectID,
			Type:     gosnmp.ObjectIdentifier,
			OnGet:    func() (interface{}, error) { return snmpserver.Asn1ObjectIdentifierWrap(arcaSNMPBaseOID), nil },
			Document: "sysObjectID",
		},
		{
			OID:  snmpOIDSysUpTime,
			Type: gosnmp.TimeTicks,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1TimeTicksWrap(snmpUptimeTicks(source.startedAt)), nil
			},
			Document: "sysUpTime",
		},
		{
			OID:  snmpOIDSysName,
			Type: gosnmp.OctetString,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1OctetStringWrap(source.snapshot(time.Now()).RunningHostname), nil
			},
			Document: "sysName",
		},
		{
			OID:      snmpOIDDaemonUp,
			Type:     gosnmp.Integer,
			OnGet:    func() (interface{}, error) { return snmpserver.Asn1IntegerWrap(1), nil },
			Document: "arcaRouterdUp",
		},
		{
			OID:  snmpOIDDaemonUptime,
			Type: gosnmp.TimeTicks,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1TimeTicksWrap(snmpUptimeTicks(source.startedAt)), nil
			},
			Document: "arcaRouterdUptime",
		},
		{
			OID:  snmpOIDConfigVersion,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(clampUint32(source.snapshot(time.Now()).ConfigVersion)), nil
			},
			Document: "arcaRouterConfigVersion",
		},
		{
			OID:  snmpOIDNETCONFListen,
			Type: gosnmp.Integer,
			OnGet: func() (interface{}, error) {
				if source.snapshot(time.Now()).NETCONFListening {
					return snmpserver.Asn1IntegerWrap(1), nil
				}
				return snmpserver.Asn1IntegerWrap(0), nil
			},
			Document: "arcaRouterNetconfListening",
		},
		{
			OID:  snmpOIDNETCONFSessions,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).NETCONFActiveSessions)), nil
			},
			Document: "arcaRouterNetconfActiveSessions",
		},
		{
			OID:  snmpOIDNETCONFConns,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).NETCONFActiveConns)), nil
			},
			Document: "arcaRouterNetconfActiveConnections",
		},
		{
			OID:  snmpOIDNETCONFTotal,
			Type: gosnmp.Counter64,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Counter64Wrap(source.snapshot(time.Now()).NETCONFTotalConns), nil
			},
			Document: "arcaRouterNetconfTotalConnections",
		},
		{
			OID:  snmpOIDNETCONFSuccess,
			Type: gosnmp.Counter64,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Counter64Wrap(source.snapshot(time.Now()).NETCONFSuccess), nil
			},
			Document: "arcaRouterNetconfSuccessfulHandshakes",
		},
		{
			OID:  snmpOIDNETCONFFailures,
			Type: gosnmp.Counter64,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Counter64Wrap(source.snapshot(time.Now()).NETCONFFailures), nil
			},
			Document: "arcaRouterNetconfFailedHandshakes",
		},
		{
			OID:      snmpOIDDaemonVersion,
			Type:     gosnmp.OctetString,
			OnGet:    func() (interface{}, error) { return snmpserver.Asn1OctetStringWrap(Version), nil },
			Document: "arcaRouterdVersion",
		},
		{
			OID:  snmpOIDVPPLCPPairs,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).VPPLCPPairs)), nil
			},
			Document: "arcaRouterVppLcpPairs",
		},
		{
			OID:  snmpOIDVPPLCPMismatch,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(len(source.snapshot(time.Now()).VPPLCPInconsistencies))), nil
			},
			Document: "arcaRouterVppLcpInconsistencies",
		},
		{
			OID:  snmpOIDVPPLCPError,
			Type: gosnmp.Integer,
			OnGet: func() (interface{}, error) {
				if source.snapshot(time.Now()).VPPLCPReconcileError != "" {
					return snmpserver.Asn1IntegerWrap(1), nil
				}
				return snmpserver.Asn1IntegerWrap(0), nil
			},
			Document: "arcaRouterVppLcpReconcileError",
		},
		{
			OID:  snmpOIDVPPLCPLastRun,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(unixTimestampSeconds(source.snapshot(time.Now()).VPPLCPReconcileLastRun))), nil
			},
			Document: "arcaRouterVppLcpLastReconcile",
		},
		{
			OID:  snmpOIDHAConfigured,
			Type: gosnmp.Integer,
			OnGet: func() (interface{}, error) {
				if source.snapshot(time.Now()).HAConfigured {
					return snmpserver.Asn1IntegerWrap(1), nil
				}
				return snmpserver.Asn1IntegerWrap(0), nil
			},
			Document: "arcaRouterHaConfigured",
		},
		{
			OID:  snmpOIDHAConverged,
			Type: gosnmp.Integer,
			OnGet: func() (interface{}, error) {
				if source.snapshot(time.Now()).HAConverged {
					return snmpserver.Asn1IntegerWrap(1), nil
				}
				return snmpserver.Asn1IntegerWrap(0), nil
			},
			Document: "arcaRouterHaConverged",
		},
		{
			OID:  snmpOIDHAVRPGroups,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).HAVRPGroups)), nil
			},
			Document: "arcaRouterHaVrrpGroups",
		},
		{
			OID:  snmpOIDHAIssues,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(len(source.snapshot(time.Now()).HAIssues))), nil
			},
			Document: "arcaRouterHaConvergenceIssues",
		},
		{
			OID:  snmpOIDFRRVRRPConfigured,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).FRRVRRPConfiguredGroups)), nil
			},
			Document: "arcaRouterFrrVrrpConfiguredGroups",
		},
		{
			OID:  snmpOIDFRRVRRPObserved,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).FRRVRRPObservedGroups)), nil
			},
			Document: "arcaRouterFrrVrrpObservedGroups",
		},
		{
			OID:  snmpOIDFRRVRRPActive,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).FRRVRRPActiveGroups)), nil
			},
			Document: "arcaRouterFrrVrrpActiveGroups",
		},
		{
			OID:  snmpOIDFRRVRRPIssues,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(len(source.snapshot(time.Now()).FRRVRRPIssues))), nil
			},
			Document: "arcaRouterFrrVrrpConvergenceIssues",
		},
		{
			OID:  snmpOIDFRRVRRPError,
			Type: gosnmp.Integer,
			OnGet: func() (interface{}, error) {
				if source.snapshot(time.Now()).FRRVRRPError != "" {
					return snmpserver.Asn1IntegerWrap(1), nil
				}
				return snmpserver.Asn1IntegerWrap(0), nil
			},
			Document: "arcaRouterFrrVrrpStatusError",
		},
		{
			OID:  snmpOIDFRRVRRPLastRun,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(unixTimestampSeconds(source.snapshot(time.Now()).FRRVRRPLastRun))), nil
			},
			Document: "arcaRouterFrrVrrpLastCheck",
		},
		{
			OID:  snmpOIDCoSConfigured,
			Type: gosnmp.Integer,
			OnGet: func() (interface{}, error) {
				if source.snapshot(time.Now()).ClassOfServiceConfigured {
					return snmpserver.Asn1IntegerWrap(1), nil
				}
				return snmpserver.Asn1IntegerWrap(0), nil
			},
			Document: "arcaRouterClassOfServiceConfigured",
		},
		{
			OID:  snmpOIDCoSClasses,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).ClassOfServiceClasses)), nil
			},
			Document: "arcaRouterClassOfServiceForwardingClasses",
		},
		{
			OID:  snmpOIDCoSProfiles,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).ClassOfServiceProfiles)), nil
			},
			Document: "arcaRouterClassOfServiceTrafficControlProfiles",
		},
		{
			OID:  snmpOIDCoSBindings,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).ClassOfServiceBindings)), nil
			},
			Document: "arcaRouterClassOfServiceInterfaceBindings",
		},
		{
			OID:  snmpOIDCoSIntentOnly,
			Type: gosnmp.Integer,
			OnGet: func() (interface{}, error) {
				if source.snapshot(time.Now()).ClassOfServiceIntentOnly {
					return snmpserver.Asn1IntegerWrap(1), nil
				}
				return snmpserver.Asn1IntegerWrap(0), nil
			},
			Document: "arcaRouterClassOfServiceIntentOnly",
		},
		{
			OID:  snmpOIDFRRBFDConfigured,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).FRRBFDConfiguredPeers)), nil
			},
			Document: "arcaRouterFrrBfdConfiguredPeers",
		},
		{
			OID:  snmpOIDFRRBFDObserved,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).FRRBFDObservedPeers)), nil
			},
			Document: "arcaRouterFrrBfdObservedPeers",
		},
		{
			OID:  snmpOIDFRRBFDUp,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).FRRBFDUpPeers)), nil
			},
			Document: "arcaRouterFrrBfdUpPeers",
		},
		{
			OID:  snmpOIDFRRBFDDownPeers,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).FRRBFDDownPeers)), nil
			},
			Document: "arcaRouterFrrBfdDownPeers",
		},
		{
			OID:  snmpOIDFRRBFDSessionDown,
			Type: gosnmp.Counter64,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Counter64Wrap(uint64(source.snapshot(time.Now()).FRRBFDSessionDownEvents)), nil
			},
			Document: "arcaRouterFrrBfdSessionDownEvents",
		},
		{
			OID:  snmpOIDFRRBFDRxFail,
			Type: gosnmp.Counter64,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Counter64Wrap(uint64(source.snapshot(time.Now()).FRRBFDRxFailPackets)), nil
			},
			Document: "arcaRouterFrrBfdRxFailPackets",
		},
		{
			OID:  snmpOIDFRRBFDIssues,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(len(source.snapshot(time.Now()).FRRBFDIssues))), nil
			},
			Document: "arcaRouterFrrBfdConvergenceIssues",
		},
		{
			OID:  snmpOIDFRRBFDError,
			Type: gosnmp.Integer,
			OnGet: func() (interface{}, error) {
				if source.snapshot(time.Now()).FRRBFDError != "" {
					return snmpserver.Asn1IntegerWrap(1), nil
				}
				return snmpserver.Asn1IntegerWrap(0), nil
			},
			Document: "arcaRouterFrrBfdStatusError",
		},
		{
			OID:  snmpOIDFRRBFDLastRun,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(unixTimestampSeconds(source.snapshot(time.Now()).FRRBFDLastRun))), nil
			},
			Document: "arcaRouterFrrBfdLastCheck",
		},
		{
			OID:  snmpOIDEVPNConfigured,
			Type: gosnmp.Integer,
			OnGet: func() (interface{}, error) {
				if source.snapshot(time.Now()).OverlayEVPNConfigured {
					return snmpserver.Asn1IntegerWrap(1), nil
				}
				return snmpserver.Asn1IntegerWrap(0), nil
			},
			Document: "arcaRouterOverlayEvpnConfigured",
		},
		{
			OID:  snmpOIDEVPNVNIs,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).OverlayEVPNVNIs)), nil
			},
			Document: "arcaRouterOverlayEvpnVnis",
		},
		{
			OID:  snmpOIDEVPNL2VNIs,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).OverlayEVPNL2VNIs)), nil
			},
			Document: "arcaRouterOverlayEvpnL2Vnis",
		},
		{
			OID:  snmpOIDEVPNL3VNIs,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).OverlayEVPNL3VNIs)), nil
			},
			Document: "arcaRouterOverlayEvpnL3Vnis",
		},
		{
			OID:  snmpOIDEVPNMulticastVNIs,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(source.snapshot(time.Now()).OverlayEVPNMulticastVNIs)), nil
			},
			Document: "arcaRouterOverlayEvpnMulticastVnis",
		},
		{
			OID:  snmpOIDCoSMetadata,
			Type: gosnmp.Integer,
			OnGet: func() (interface{}, error) {
				if source.snapshot(time.Now()).ClassOfServiceMetadataBindingSupported {
					return snmpserver.Asn1IntegerWrap(1), nil
				}
				return snmpserver.Asn1IntegerWrap(0), nil
			},
			Document: "arcaRouterClassOfServiceMetadataBindingSupported",
		},
		{
			OID:  snmpOIDCoSScheduler,
			Type: gosnmp.Integer,
			OnGet: func() (interface{}, error) {
				if source.snapshot(time.Now()).ClassOfServiceQueueSchedulerSupported {
					return snmpserver.Asn1IntegerWrap(1), nil
				}
				return snmpserver.Asn1IntegerWrap(0), nil
			},
			Document: "arcaRouterClassOfServiceQueueSchedulerSupported",
		},
		{
			OID:  snmpOIDCoSPolicer,
			Type: gosnmp.Integer,
			OnGet: func() (interface{}, error) {
				if source.snapshot(time.Now()).ClassOfServicePolicerSupported {
					return snmpserver.Asn1IntegerWrap(1), nil
				}
				return snmpserver.Asn1IntegerWrap(0), nil
			},
			Document: "arcaRouterClassOfServicePolicerSupported",
		},
		{
			OID:  snmpOIDCoSCounters,
			Type: gosnmp.Integer,
			OnGet: func() (interface{}, error) {
				if source.snapshot(time.Now()).ClassOfServiceCountersSupported {
					return snmpserver.Asn1IntegerWrap(1), nil
				}
				return snmpserver.Asn1IntegerWrap(0), nil
			},
			Document: "arcaRouterClassOfServiceCountersSupported",
		},
		{
			OID:  snmpOIDCoSCapabilityErr,
			Type: gosnmp.Integer,
			OnGet: func() (interface{}, error) {
				if source.snapshot(time.Now()).ClassOfServiceCapabilityError != "" {
					return snmpserver.Asn1IntegerWrap(1), nil
				}
				return snmpserver.Asn1IntegerWrap(0), nil
			},
			Document: "arcaRouterClassOfServiceCapabilityError",
		},
		{
			OID:  snmpOIDCoSCapabilityLast,
			Type: gosnmp.Gauge32,
			OnGet: func() (interface{}, error) {
				return snmpserver.Asn1Gauge32Wrap(uint(unixTimestampSeconds(source.snapshot(time.Now()).ClassOfServiceCapabilityLastCheck))), nil
			},
			Document: "arcaRouterClassOfServiceCapabilityLastCheck",
		},
	}
}

func snmpUptimeTicks(startedAt time.Time) uint32 {
	if startedAt.IsZero() {
		return 0
	}
	ticks := time.Since(startedAt) / (10 * time.Millisecond)
	if ticks < 0 {
		return 0
	}
	if ticks > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(ticks)
}

func clampUint32(value uint64) uint {
	if value > math.MaxUint32 {
		return uint(math.MaxUint32)
	}
	return uint(value)
}
