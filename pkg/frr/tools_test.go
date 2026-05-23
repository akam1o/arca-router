package frr

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLookupSystemExecutableUsesTrustedCandidate(t *testing.T) {
	untrustedDir := t.TempDir()
	untrustedPath := filepath.Join(untrustedDir, "vtysh")
	if err := os.WriteFile(untrustedPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("failed to write untrusted executable: %v", err)
	}
	t.Setenv("PATH", untrustedDir)

	trustedPath := filepath.Join(t.TempDir(), "vtysh")
	if err := os.WriteFile(trustedPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("failed to write trusted executable: %v", err)
	}

	got, err := lookupSystemExecutable("vtysh", []string{trustedPath})
	if err != nil {
		t.Fatalf("lookupSystemExecutable() error = %v", err)
	}
	if got != trustedPath {
		t.Fatalf("lookupSystemExecutable() = %q, want %q", got, trustedPath)
	}
}

func TestLookupSystemExecutableDoesNotSearchPATH(t *testing.T) {
	untrustedDir := t.TempDir()
	untrustedPath := filepath.Join(untrustedDir, "vtysh")
	if err := os.WriteFile(untrustedPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("failed to write untrusted executable: %v", err)
	}
	t.Setenv("PATH", untrustedDir)

	_, err := lookupSystemExecutable("vtysh", []string{filepath.Join(t.TempDir(), "missing-vtysh")})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lookupSystemExecutable() error = %v, want os.ErrNotExist", err)
	}
}

func TestLookupSystemExecutableReportsPermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping permission check as root")
	}

	trustedPath := filepath.Join(t.TempDir(), "vtysh")
	if err := os.WriteFile(trustedPath, []byte("#!/bin/sh\n"), 0644); err != nil {
		t.Fatalf("failed to write trusted executable: %v", err)
	}

	_, err := lookupSystemExecutable("vtysh", []string{trustedPath})
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("lookupSystemExecutable() error = %v, want os.ErrPermission", err)
	}
}
