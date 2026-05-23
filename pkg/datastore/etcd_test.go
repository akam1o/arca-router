package datastore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildTLSConfigRejectsInsecureKeyPermissions(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "client.crt")
	keyFile := filepath.Join(dir, "client.key")
	caFile := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(certFile, []byte("not a certificate"), 0600); err != nil {
		t.Fatalf("WriteFile(cert) error = %v", err)
	}
	if err := os.WriteFile(keyFile, []byte("not a key"), 0600); err != nil {
		t.Fatalf("WriteFile(key) error = %v", err)
	}
	if err := os.WriteFile(caFile, []byte("not a ca"), 0600); err != nil {
		t.Fatalf("WriteFile(ca) error = %v", err)
	}
	if err := os.Chmod(keyFile, 0644); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	_, err := buildTLSConfig(&TLSConfig{
		CertFile: certFile,
		KeyFile:  keyFile,
		CAFile:   caFile,
	})
	if err == nil {
		t.Fatal("buildTLSConfig() error = nil, want key permission error")
	}
	if !strings.Contains(err.Error(), "failed to load client cert/key") || !strings.Contains(err.Error(), "insecure permissions") {
		t.Fatalf("buildTLSConfig() error = %v, want key permission validation error", err)
	}
}
