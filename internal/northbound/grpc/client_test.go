package grpc

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildClientTLSConfigAppliesPolicyAndServerName(t *testing.T) {
	cfg, err := buildClientTLSConfig(TLSClientOptions{ServerName: " router.example.test "})
	if err != nil {
		t.Fatalf("buildClientTLSConfig() error = %v", err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x, want TLS 1.2", cfg.MinVersion)
	}
	if cfg.ServerName != "router.example.test" {
		t.Fatalf("ServerName = %q, want trimmed value", cfg.ServerName)
	}
}

func TestBuildClientTLSConfigRejectsPartialClientCertificate(t *testing.T) {
	_, err := buildClientTLSConfig(TLSClientOptions{ClientCertFile: "/tmp/client.crt"})
	if err == nil {
		t.Fatal("buildClientTLSConfig() error = nil, want partial certificate error")
	}
	if !strings.Contains(err.Error(), "both client cert and key") {
		t.Fatalf("buildClientTLSConfig() error = %v, want partial certificate error", err)
	}
}

func TestBuildClientTLSConfigRejectsInvalidCA(t *testing.T) {
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caFile, []byte("not a certificate"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err := buildClientTLSConfig(TLSClientOptions{CAFile: caFile})
	if err == nil {
		t.Fatal("buildClientTLSConfig() error = nil, want invalid CA error")
	}
	if !strings.Contains(err.Error(), "parse gRPC CA") {
		t.Fatalf("buildClientTLSConfig() error = %v, want parse CA error", err)
	}
}

func TestBuildClientTLSConfigRejectsInsecureClientKeyPermissions(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "client.crt")
	keyFile := filepath.Join(dir, "client.key")
	if err := os.WriteFile(certFile, []byte("not a certificate"), 0600); err != nil {
		t.Fatalf("WriteFile(cert) error = %v", err)
	}
	if err := os.WriteFile(keyFile, []byte("not a key"), 0600); err != nil {
		t.Fatalf("WriteFile(key) error = %v", err)
	}
	if err := os.Chmod(keyFile, 0644); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	_, err := buildClientTLSConfig(TLSClientOptions{
		ClientCertFile: certFile,
		ClientKeyFile:  keyFile,
	})
	if err == nil {
		t.Fatal("buildClientTLSConfig() error = nil, want key permission error")
	}
	if !strings.Contains(err.Error(), "load gRPC client cert/key") || !strings.Contains(err.Error(), "insecure permissions") {
		t.Fatalf("buildClientTLSConfig() error = %v, want key permission validation error", err)
	}
}
