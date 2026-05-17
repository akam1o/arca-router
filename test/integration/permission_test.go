package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akam1o/arca-router/pkg/config"
	"github.com/akam1o/arca-router/pkg/frr"
)

func TestPermissionErrorHandlingForRestrictedConfigDirectories(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping permission integration test as root")
	}

	restrictedDir := t.TempDir()
	if err := os.Chmod(restrictedDir, 0500); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	defer func() {
		_ = os.Chmod(restrictedDir, 0700)
	}()

	backupPath := filepath.Join(restrictedDir, "backup.conf")
	err := config.WriteConfigBackupFile(backupPath, "set system host-name router")
	if err == nil {
		t.Fatal("WriteConfigBackupFile() error = nil, want permission error")
	}
	if !strings.Contains(err.Error(), "create backup file") {
		t.Fatalf("WriteConfigBackupFile() error = %v, want create backup detail", err)
	}

	reloader := &frr.Reloader{
		ConfigPath:    filepath.Join(restrictedDir, "frr.conf"),
		BackupEnabled: false,
	}
	err = reloader.WriteConfig("hostname router\n")
	if err == nil {
		t.Fatal("Reloader.WriteConfig() error = nil, want permission error")
	}
	if !frr.IsPermissionDenied(err) {
		t.Fatalf("Reloader.WriteConfig() error = %v, want FRR permission error", err)
	}
}
