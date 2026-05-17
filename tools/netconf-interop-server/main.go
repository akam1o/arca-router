package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/akam1o/arca-router/pkg/datastore"
	"github.com/akam1o/arca-router/pkg/netconf"
)

func main() {
	var (
		listenAddr        string
		hostKeyPath       string
		userDBPath        string
		datastorePath     string
		runningConfigPath string
		standardXPath     bool
	)

	flag.StringVar(&listenAddr, "listen", "127.0.0.1:830", "NETCONF SSH listen address")
	flag.StringVar(&hostKeyPath, "host-key", "", "path to SSH host key")
	flag.StringVar(&userDBPath, "user-db", "", "path to NETCONF user database")
	flag.StringVar(&datastorePath, "datastore", "", "path to NETCONF SQLite datastore")
	flag.StringVar(&runningConfigPath, "running-config", "", "path to initial running set-command config")
	flag.BoolVar(&standardXPath, "standard-xpath", true, "advertise the standard NETCONF :xpath capability (enabled by default; set false to suppress)")
	flag.Parse()

	if hostKeyPath == "" || userDBPath == "" || datastorePath == "" || runningConfigPath == "" {
		fmt.Fprintln(os.Stderr, "-host-key, -user-db, -datastore, and -running-config are required")
		os.Exit(2)
	}

	runningConfig, err := os.ReadFile(runningConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read running config: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := seedRunningConfig(ctx, datastorePath, string(runningConfig)); err != nil {
		fmt.Fprintf(os.Stderr, "seed running config: %v\n", err)
		os.Exit(1)
	}

	cfg := netconf.DefaultSSHConfig()
	cfg.ListenAddr = listenAddr
	cfg.HostKeyPath = hostKeyPath
	cfg.UserDBPath = userDBPath
	cfg.DatastorePath = datastorePath
	cfg.IdleTimeout = 5 * time.Minute
	cfg.AbsoluteTimeout = 10 * time.Minute
	cfg.AdvertiseStandardXPath = standardXPath
	cfg.DisableStandardXPath = !standardXPath

	server, err := netconf.NewSSHServer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create NETCONF server: %v\n", err)
		os.Exit(1)
	}

	if err := server.Start(ctx); err != nil {
		_ = server.Stop()
		fmt.Fprintf(os.Stderr, "start NETCONF server: %v\n", err)
		os.Exit(1)
	}

	<-ctx.Done()
	if err := server.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "stop NETCONF server: %v\n", err)
		os.Exit(1)
	}
}

func seedRunningConfig(ctx context.Context, datastorePath, configText string) error {
	ds, err := datastore.NewDatastore(&datastore.Config{
		Backend:    datastore.BackendSQLite,
		SQLitePath: datastorePath,
	})
	if err != nil {
		return err
	}
	defer func() { _ = ds.Close() }()

	const sessionID = "netconf-interop-bootstrap"
	if err := ds.AcquireLock(ctx, &datastore.LockRequest{
		Target:    datastore.LockTargetCandidate,
		SessionID: sessionID,
		User:      "system",
		Timeout:   time.Minute,
	}); err != nil {
		return err
	}
	defer func() { _ = ds.ReleaseLock(context.Background(), datastore.LockTargetCandidate, sessionID) }()

	if err := ds.SaveCandidate(ctx, sessionID, configText); err != nil {
		return err
	}
	_, err = ds.Commit(ctx, &datastore.CommitRequest{
		SessionID: sessionID,
		User:      "system",
		Message:   "seed NETCONF interop running config",
		SourceIP:  "127.0.0.1",
	})
	return err
}
