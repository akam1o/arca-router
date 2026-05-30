package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akam1o/arca-router/internal/compat"
)

func upgradePreflightLines(ctx context.Context, client showClient) ([]string, error) {
	return upgradePreflightLinesWithOptions(ctx, client, upgradePreflightOptions{})
}

type upgradePreflightOptions struct {
	BackupPath string
}

func parseUpgradePreflightOptions(args []string) (upgradePreflightOptions, error) {
	if len(args) == 1 && args[0] == "upgrade" {
		return upgradePreflightOptions{}, nil
	}
	if len(args) == 3 && args[0] == "upgrade" && args[1] == "backup" {
		return upgradePreflightOptions{BackupPath: args[2]}, nil
	}
	return upgradePreflightOptions{}, errors.New(checkUpgradeUsage)
}

func upgradePreflightLinesWithOptions(ctx context.Context, client showClient, options upgradePreflightOptions) ([]string, error) {
	runningText, runningVersion, err := client.GetRunning(ctx)
	if err != nil {
		return nil, fmt.Errorf("load running configuration: %w", err)
	}

	lines := []string{"upgrade preflight:"}
	warnings := 0
	if strings.TrimSpace(runningText) == "" {
		lines, warnings = appendUpgradePreflightWarning(lines, warnings, "running configuration is empty")
	} else {
		lines = append(lines, fmt.Sprintf("  running config: version %d, %d bytes", runningVersion, len(runningText)))
		if err := validateConfigurationText(runningText); err != nil {
			lines, warnings = appendUpgradePreflightWarning(lines, warnings, "running configuration validation failed: "+err.Error())
		} else {
			lines = append(lines, "  running validation: ok")
		}
	}
	lines = append(lines, upgradeCompatibilityPreflightLines()...)

	rollbackCommitID, rollbackConfigText, ok, err := latestRollbackArchiveText(ctx, client)
	if err != nil {
		lines, warnings = appendUpgradePreflightWarning(lines, warnings, "rollback archive check failed: "+err.Error())
	} else if !ok {
		lines, warnings = appendUpgradePreflightWarning(lines, warnings, "no rollback archive entries available")
	} else if strings.TrimSpace(rollbackConfigText) == "" {
		lines, warnings = appendUpgradePreflightWarning(lines, warnings, "latest rollback archive has no config text")
	} else {
		lines = append(lines, fmt.Sprintf("  rollback archive: latest commit %s available", shortCommitID(rollbackCommitID)))
		if err := validateConfigurationText(rollbackConfigText); err != nil {
			lines, warnings = appendUpgradePreflightWarning(lines, warnings, "latest rollback archive validation failed: "+err.Error())
		} else {
			lines = append(lines, "  rollback archive validation: ok")
		}
	}

	catalog, err := client.GetTelemetryCatalog(ctx)
	if err != nil {
		lines, warnings = appendUpgradePreflightWarning(lines, warnings, "telemetry catalog check failed: "+err.Error())
	} else if catalog.EventSchemaVersion == "" || catalog.Encoding == "" || len(catalog.Paths) == 0 {
		lines, warnings = appendUpgradePreflightWarning(lines, warnings, "telemetry catalog is incomplete")
	} else {
		lines = append(lines, fmt.Sprintf("  telemetry catalog: %d paths, schema %s, encoding %s",
			len(catalog.Paths), catalog.EventSchemaVersion, catalog.Encoding))
	}

	cosInfo, err := client.GetClassOfService(ctx)
	if err != nil {
		lines, warnings = appendUpgradePreflightWarning(lines, warnings, "qos capability check failed: "+err.Error())
	} else if cosInfo == nil || cosInfo.Capabilities == nil {
		lines, warnings = appendUpgradePreflightWarning(lines, warnings, "qos capability snapshot unavailable")
	} else {
		capabilities := cosInfo.Capabilities
		lines = append(lines,
			fmt.Sprintf("  qos metadata binding: %s", yesNo(capabilities.MetadataBindingSupported)),
			fmt.Sprintf("  qos scheduler: %s", yesNo(capabilities.QueueSchedulerSupported)),
			fmt.Sprintf("  qos policer: %s", yesNo(capabilities.PolicerSupported)),
			fmt.Sprintf("  qos counters: %s", yesNo(capabilities.CountersSupported)),
		)
		if capabilities.LastError != "" {
			lines, warnings = appendUpgradePreflightWarning(lines, warnings, "qos capability detector reported: "+capabilities.LastError)
		}
	}

	if options.BackupPath != "" {
		lines, warnings = appendUpgradeBackupPathCheck(lines, warnings, options.BackupPath)
	}
	packageLines, packageWarnings := upgradePackagePreflightLines()
	lines = append(lines, packageLines...)
	warnings += packageWarnings
	lines = append(lines, upgradeReleaseReadinessLines()...)
	lines = append(lines, upgradeRollbackGuidanceLines()...)

	if warnings == 0 {
		lines = append(lines, "  status: ready for package-specific upgrade checks")
	} else {
		lines = append(lines, fmt.Sprintf("  status: %d warning(s), review before upgrade", warnings))
	}
	lines = append(lines, "  next step: keep a fresh configuration backup and verify release-specific package notes")
	return lines, nil
}

type upgradePackageCheck struct {
	Path        string
	Description string
}

func upgradePackagePreflightLines() ([]string, int) {
	return upgradePackagePreflightLinesWithRoot("")
}

func upgradePackagePreflightLinesWithRoot(root string) ([]string, int) {
	checks := []upgradePackageCheck{
		{Path: "/usr/sbin/arca-routerd", Description: "daemon binary"},
		{Path: "/usr/bin/arca", Description: "CLI binary"},
		{Path: "/usr/lib/systemd/system/arca-routerd.service", Description: "systemd unit"},
		{Path: "/etc/arca-router", Description: "configuration directory"},
		{Path: "/var/lib/arca-router", Description: "state directory"},
	}

	lines := []string{"  package preflight:"}
	if !packagedInstallDetected(root, checks) {
		return append(lines, "    packaged install paths: not detected; run package manager dry-run checks before deployment"), 0
	}

	warnings := 0
	for _, check := range checks {
		fullPath := rootedPackagePath(root, check.Path)
		info, err := os.Stat(fullPath)
		if err != nil {
			lines, warnings = appendUpgradePreflightWarning(lines, warnings, fmt.Sprintf("%s missing at %s", check.Description, check.Path))
			continue
		}
		kind := "file"
		if info.IsDir() {
			kind = "directory"
		}
		lines = append(lines, fmt.Sprintf("    %s: %s present", check.Description, kind))
	}
	return lines, warnings
}

func packagedInstallDetected(root string, checks []upgradePackageCheck) bool {
	for _, check := range checks {
		if _, err := os.Stat(rootedPackagePath(root, check.Path)); err == nil {
			return true
		}
	}
	return false
}

func rootedPackagePath(root, path string) string {
	if strings.TrimSpace(root) == "" {
		return path
	}
	return filepath.Join(root, strings.TrimPrefix(path, string(os.PathSeparator)))
}

func upgradeRollbackGuidanceLines() []string {
	return []string{
		"  rollback guidance:",
		"    keep the pre-upgrade package artifact or repository pin available until post-upgrade validation passes",
		"    keep a fresh configuration backup and verify at least one rollback archive entry before replacing packages",
		"    if daemon startup fails after upgrade, restore the previous package, then use arca backup/show configuration rollback output to recover config",
	}
}

func upgradeReleaseReadinessLines() []string {
	return []string{
		"  release readiness:",
		"    complete docs/v0.10-operational-runbook.md or docs/v0.10-operational-runbook.ja.md before release sign-off",
		"    attach docs/v0.10-release-readiness.md evidence for package build, tests, compatibility output, and accepted warnings",
		fmt.Sprintf("    record lab-only HA/restart/churn gaps and NETCONF capability gaps in %s", compat.DeferredGateDocument),
		"    release sign-off must list owner, date, commit/tag, accepted warnings, and deferred gates",
	}
}

func upgradeCompatibilityPreflightLines() []string {
	policy := compat.CurrentPolicy()
	return []string{
		fmt.Sprintf("  compatibility phase: %s", policy.Phase),
		fmt.Sprintf("  supported direct upgrade sources: %s", strings.Join(policy.SupportedDirectUpgradeSources, ", ")),
		fmt.Sprintf("  unsupported direct upgrades: %s", policy.UnsupportedDirectUpgradeNote),
		fmt.Sprintf("  API compatibility: %s, %s", compat.GRPCAPIPackage, compat.TelemetryEventSchema),
		fmt.Sprintf("  datastore schema guard: SQLite schema 1-%d accepted", compat.CurrentSQLiteSchema),
	}
}

func appendUpgradePreflightWarning(lines []string, warnings int, message string) ([]string, int) {
	return append(lines, "  warning: "+message), warnings + 1
}

func appendUpgradeBackupPathCheck(lines []string, warnings int, path string) ([]string, int) {
	if strings.TrimSpace(path) == "" {
		return appendUpgradePreflightWarning(lines, warnings, "backup path must not be empty")
	}
	if _, err := os.Stat(path); err == nil {
		return appendUpgradePreflightWarning(lines, warnings, "backup path already exists: "+path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return appendUpgradePreflightWarning(lines, warnings, "backup path cannot be checked: "+err.Error())
	}

	dir := filepath.Dir(path)
	if info, err := os.Stat(dir); err != nil {
		return appendUpgradePreflightWarning(lines, warnings, "backup directory unavailable: "+err.Error())
	} else if !info.IsDir() {
		return appendUpgradePreflightWarning(lines, warnings, "backup directory is not a directory: "+dir)
	}

	probePath := filepath.Join(dir, fmt.Sprintf(".arca-upgrade-preflight-%d-%d.tmp", os.Getpid(), time.Now().UnixNano()))
	file, err := os.OpenFile(probePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return appendUpgradePreflightWarning(lines, warnings, "backup directory is not writable: "+err.Error())
	}
	closeErr := file.Close()
	removeErr := os.Remove(probePath)
	if closeErr != nil {
		return appendUpgradePreflightWarning(lines, warnings, "backup path probe close failed: "+closeErr.Error())
	}
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return appendUpgradePreflightWarning(lines, warnings, "backup path probe cleanup failed: "+removeErr.Error())
	}

	return append(lines, "  backup path: writable "+path), warnings
}

func commitRollbackArchiveWarnings(ctx context.Context, client showClient) []string {
	_, configText, ok, err := latestRollbackArchiveText(ctx, client)
	if err != nil {
		return []string{"commit safety warning: rollback archive check failed: " + err.Error()}
	}
	if !ok {
		return []string{"commit safety warning: no rollback archive entry is available before commit"}
	}
	if strings.TrimSpace(configText) == "" {
		return []string{"commit safety warning: latest rollback archive has no config text"}
	}
	if err := validateConfigurationText(configText); err != nil {
		return []string{"commit safety warning: latest rollback archive validation failed: " + err.Error()}
	}
	return nil
}

func latestRollbackArchiveText(ctx context.Context, client showClient) (commitID, configText string, ok bool, err error) {
	history, err := client.ListHistory(ctx, 1, 0)
	if err != nil {
		return "", "", false, err
	}
	if len(history) == 0 {
		return "", "", false, nil
	}
	entry := history[0]
	detail, err := client.GetCommit(ctx, entry.CommitID)
	if err != nil {
		return "", "", true, err
	}
	commitID = detail.CommitID
	if commitID == "" {
		commitID = entry.CommitID
	}
	return commitID, detail.ConfigText, true, nil
}
