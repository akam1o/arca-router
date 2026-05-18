package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteConfigBackupFileCreatesSecureFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.conf")

	if err := WriteConfigBackupFile(path, "set system host-name router"); err != nil {
		t.Fatalf("WriteConfigBackupFile() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "set system host-name router\n" {
		t.Fatalf("backup content = %q, want trailing newline", string(data))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != SecureBackupFileMode {
		t.Fatalf("backup mode = %04o, want %04o", got, SecureBackupFileMode)
	}
}

func TestWriteConfigBackupFileRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.conf")
	if err := os.WriteFile(path, []byte("existing\n"), SecureBackupFileMode); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := WriteConfigBackupFile(path, "set system host-name router")
	if err == nil || !strings.Contains(err.Error(), "create backup file") {
		t.Fatalf("WriteConfigBackupFile() error = %v, want create failure", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "existing\n" {
		t.Fatalf("backup content = %q, want existing content preserved", string(data))
	}
}

func TestWriteConfigBackupFileRejectsEmptyPath(t *testing.T) {
	err := WriteConfigBackupFile(" \t", "set system host-name router")
	if err == nil || !strings.Contains(err.Error(), "backup path must not be empty") {
		t.Fatalf("WriteConfigBackupFile() error = %v, want empty path error", err)
	}
}
