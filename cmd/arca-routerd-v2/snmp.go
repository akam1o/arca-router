package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/gosnmp/gosnmp"
	snmpserver "github.com/slayercat/GoSNMPServer"

	"github.com/akam1o/arca-router/pkg/logger"
)

const (
	arcaSNMPBaseOID = "1.3.6.1.3.9950.1"

	snmpOIDSysDescr        = "1.3.6.1.2.1.1.1.0"
	snmpOIDSysObjectID     = "1.3.6.1.2.1.1.2.0"
	snmpOIDSysUpTime       = "1.3.6.1.2.1.1.3.0"
	snmpOIDSysName         = "1.3.6.1.2.1.1.5.0"
	snmpOIDDaemonUp        = arcaSNMPBaseOID + ".1.0"
	snmpOIDDaemonUptime    = arcaSNMPBaseOID + ".2.0"
	snmpOIDConfigVersion   = arcaSNMPBaseOID + ".3.0"
	snmpOIDNETCONFListen   = arcaSNMPBaseOID + ".4.0"
	snmpOIDNETCONFSessions = arcaSNMPBaseOID + ".5.0"
	snmpOIDNETCONFConns    = arcaSNMPBaseOID + ".6.0"
	snmpOIDNETCONFTotal    = arcaSNMPBaseOID + ".7.0"
	snmpOIDNETCONFSuccess  = arcaSNMPBaseOID + ".8.0"
	snmpOIDNETCONFFailures = arcaSNMPBaseOID + ".9.0"
	snmpOIDDaemonVersion   = arcaSNMPBaseOID + ".10.0"
)

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
