package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/akam1o/arca-router/internal/model"
	configcli "github.com/akam1o/arca-router/pkg/cli"
	pkgconfig "github.com/akam1o/arca-router/pkg/config"
)

func (sh *interactiveShell) cmdBackup(ctx context.Context, args []string) error {
	if len(args) == 2 && args[0] == "configuration" {
		var text string
		var err error
		if sh.mode == modeConfiguration {
			text, err = sh.client.GetCandidate(ctx, sh.sessionID)
		} else {
			text, _, err = sh.client.GetRunning(ctx)
		}
		if err != nil {
			return err
		}
		return sh.writeConfigurationBackup(args[1], text)
	}

	if len(args) == 4 && args[0] == "configuration" && args[1] == "rollback" {
		rollbackNum, err := parseRollbackNumber(args[2])
		if err != nil {
			return err
		}
		text, err := sh.archivedConfiguration(ctx, rollbackNum)
		if err != nil {
			return err
		}
		return sh.writeConfigurationBackup(args[3], text)
	}

	return fmt.Errorf("usage: backup configuration [rollback <N>] <path>")
}

func (sh *interactiveShell) cmdRestore(ctx context.Context, args []string) error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'restore' command only available in configuration mode")
	}
	if len(args) == 2 && args[0] == "configuration" {
		data, err := os.ReadFile(args[1])
		if err != nil {
			return fmt.Errorf("read configuration backup: %w", err)
		}
		text := string(data)
		if err := validateConfigurationText(text); err != nil {
			return fmt.Errorf("validate configuration backup: %w", err)
		}
		if err := sh.client.ReplaceCandidate(ctx, sh.sessionID, text); err != nil {
			return fmt.Errorf("restore configuration: %w", err)
		}
		fmt.Printf("configuration restored to candidate from %s\n", args[1])
		return nil
	}
	if len(args) == 3 && args[0] == "configuration" && args[1] == "rollback" {
		rollbackNum, err := parseRollbackNumber(args[2])
		if err != nil {
			return err
		}
		text, err := sh.archivedConfiguration(ctx, rollbackNum)
		if err != nil {
			return err
		}
		if err := validateConfigurationText(text); err != nil {
			return fmt.Errorf("validate rollback configuration: %w", err)
		}
		if err := sh.client.ReplaceCandidate(ctx, sh.sessionID, text); err != nil {
			return fmt.Errorf("restore configuration: %w", err)
		}
		fmt.Printf("configuration restored to candidate from rollback %d\n", rollbackNum)
		return nil
	}
	return fmt.Errorf("usage: restore configuration <path> | restore configuration rollback <N>")
}

func (sh *interactiveShell) writeConfigurationBackup(path, text string) error {
	if err := writeConfigBackupFile(path, text); err != nil {
		return err
	}
	fmt.Printf("configuration backup written to %s\n", path)
	return nil
}

func writeConfigBackupFile(path, text string) error {
	return pkgconfig.WriteConfigBackupFile(path, text)
}

func validateConfigurationText(text string) error {
	cfg, err := pkgconfig.NewParser(strings.NewReader(text)).Parse()
	if err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	if err := model.FromLegacyConfig(cfg).Validate(); err != nil {
		return fmt.Errorf("validate model: %w", err)
	}
	return nil
}

func (sh *interactiveShell) cmdSet(ctx context.Context, args []string) error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'set' command only available in configuration mode")
	}
	// Build the full set command and send to daemon
	fullPath := append(sh.editPath, args...)
	setCmd := "set " + configcli.NormalizeConfigPath(fullPath)
	if err := sh.client.EditCandidate(ctx, sh.sessionID, setCmd); err != nil {
		return err
	}
	fmt.Println("[edit]")
	return nil
}

func (sh *interactiveShell) cmdDelete(ctx context.Context, args []string) error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'delete' command only available in configuration mode")
	}
	fullPath := append(sh.editPath, args...)
	delCmd := "delete " + configcli.NormalizeConfigPath(fullPath)
	if err := sh.client.EditCandidate(ctx, sh.sessionID, delCmd); err != nil {
		return err
	}
	fmt.Println("[edit]")
	return nil
}

func (sh *interactiveShell) cmdCommit(ctx context.Context, args []string) error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'commit' command only available in configuration mode")
	}

	message := ""
	check := false
	andQuit := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "check":
			check = true
		case "and-quit":
			andQuit = true
		case "comment":
			if i+1 < len(args) {
				message = args[i+1]
				i++
			} else {
				return fmt.Errorf("'comment' requires an argument")
			}
		default:
			return fmt.Errorf("unknown commit option: %s", args[i])
		}
	}

	if check && andQuit {
		return fmt.Errorf("'check' and 'and-quit' cannot be used together")
	}
	if check && message != "" {
		return fmt.Errorf("'check' and 'comment' cannot be used together")
	}

	if check {
		if err := sh.client.ValidateCandidate(ctx, sh.sessionID); err != nil {
			return fmt.Errorf("configuration check failed: %w", err)
		}
		fmt.Println("configuration check succeeds")
		if err := sh.printChangeImpactPreview(ctx); err != nil {
			return fmt.Errorf("change impact preview failed: %w", err)
		}
		return nil
	}

	diffText, hasChanges, diffErr := sh.client.Diff(ctx, sh.sessionID)
	for _, warning := range commitRollbackArchiveWarnings(ctx, sh.client) {
		fmt.Println(warning)
	}
	user := currentUsername()

	commitID, version, err := sh.client.Commit(ctx, sh.sessionID, user, message)
	if err != nil {
		if diagErr := sh.printCommitFailureDiagnostics(ctx, diffText, hasChanges, diffErr); diagErr != nil {
			return fmt.Errorf("commit failed: %w (diagnostics unavailable: %v)", err, diagErr)
		}
		return fmt.Errorf("commit failed: %w", err)
	}
	fmt.Printf("commit complete (id: %s, version: %d)\n", shortCommitID(commitID), version)
	if diagErr := sh.printPostCommitDiagnostics(ctx, diffText, hasChanges, diffErr); diagErr != nil {
		fmt.Printf("post-commit diagnostics unavailable: %v\n", diagErr)
	}

	if andQuit {
		if err := sh.releaseConfigurationLock(ctx); err != nil {
			return fmt.Errorf("commit complete but failed to exit configuration mode: %w", err)
		}
	}
	return nil
}

func (sh *interactiveShell) cmdRollback(ctx context.Context, args []string) error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'rollback' command only available in configuration mode")
	}
	rollbackNum := 0
	if len(args) > 0 {
		var err error
		rollbackNum, err = parseRollbackNumber(args[0])
		if err != nil {
			return err
		}
	}
	if rollbackNum == 0 {
		return sh.cmdDiscardChanges(ctx)
	}

	history, err := sh.client.ListHistory(ctx, rollbackNum+1, 0)
	if err != nil {
		return fmt.Errorf("failed to load commit history: %w", err)
	}
	if len(history) <= rollbackNum {
		availableCommits := len(history) - 1
		if availableCommits < 0 {
			availableCommits = 0
		}
		return fmt.Errorf("not enough history for rollback %d (only %d commits available)", rollbackNum, availableCommits)
	}
	target := history[rollbackNum]
	user := currentUsername()
	newCommitID, version, err := sh.client.Rollback(ctx, sh.sessionID, target.CommitID, user, fmt.Sprintf("CLI rollback %d", rollbackNum))
	if err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}
	fmt.Printf("rollback complete (id: %s, version: %d)\n", shortCommitID(newCommitID), version)
	return nil
}

func (sh *interactiveShell) cmdDiscardChanges(ctx context.Context) error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'discard-changes' command only available in configuration mode")
	}
	if err := sh.client.Discard(ctx, sh.sessionID); err != nil {
		return err
	}
	fmt.Println("Changes discarded")
	return nil
}

func (sh *interactiveShell) cmdCompare(ctx context.Context) error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'compare' command only available in configuration mode")
	}
	diffText, hasChanges, err := sh.client.Diff(ctx, sh.sessionID)
	if err != nil {
		return err
	}
	if !hasChanges {
		fmt.Println("No changes")
	} else {
		fmt.Println(diffText)
	}
	return nil
}

func (sh *interactiveShell) printChangeImpactPreview(ctx context.Context) error {
	diffText, hasChanges, err := sh.client.Diff(ctx, sh.sessionID)
	if err != nil {
		return err
	}
	for _, line := range formatChangeImpactPreview(diffText, hasChanges) {
		fmt.Println(line)
	}
	for _, line := range sh.classOfServicePreflightLines(ctx, diffText, hasChanges) {
		fmt.Println(line)
	}
	return nil
}

func (sh *interactiveShell) printCommitFailureDiagnostics(ctx context.Context, diffText string, hasChanges bool, diffErr error) error {
	if diffErr != nil {
		return diffErr
	}
	fmt.Println("commit failure diagnostics:")
	for _, line := range formatChangeImpactPreview(diffText, hasChanges) {
		fmt.Printf("  %s\n", line)
	}
	for _, line := range sh.classOfServicePreflightLines(ctx, diffText, hasChanges) {
		fmt.Printf("  %s\n", line)
	}
	fmt.Println("  next step: resolve the error and run 'commit check'")
	return nil
}

func (sh *interactiveShell) printPostCommitDiagnostics(ctx context.Context, diffText string, hasChanges bool, diffErr error) error {
	if diffErr != nil || !hasChanges || !analyzeChangeImpact(diffText).classOfService.hasChanges() {
		return diffErr
	}
	info, err := sh.client.GetClassOfService(ctx)
	if err != nil {
		return err
	}
	for _, line := range formatClassOfServicePostCommit(info) {
		fmt.Println(line)
	}
	return nil
}

func (sh *interactiveShell) classOfServicePreflightLines(ctx context.Context, diffText string, hasChanges bool) []string {
	if !hasChanges || !analyzeChangeImpact(diffText).classOfService.hasChanges() {
		return nil
	}
	info, err := sh.client.GetClassOfService(ctx)
	if err != nil {
		return []string{"qos preflight: capability check unavailable: " + err.Error()}
	}
	return formatClassOfServicePreflight(info)
}
