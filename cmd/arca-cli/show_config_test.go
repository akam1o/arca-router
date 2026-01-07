package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadConfigFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
		wantErr bool
	}{
		{
			name: "basic config",
			content: `set system host-name router1
set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24
set protocols bgp group ibgp type internal`,
			want: []string{
				"set system host-name router1",
				"set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24",
				"set protocols bgp group ibgp type internal",
			},
			wantErr: false,
		},
		{
			name: "skip empty lines",
			content: `set system host-name router1

set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24


set protocols bgp group ibgp type internal`,
			want: []string{
				"set system host-name router1",
				"set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24",
				"set protocols bgp group ibgp type internal",
			},
			wantErr: false,
		},
		{
			name: "skip comments",
			content: `# This is a comment
set system host-name router1
# Another comment
set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24
### Multiple hashes
set protocols bgp group ibgp type internal`,
			want: []string{
				"set system host-name router1",
				"set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24",
				"set protocols bgp group ibgp type internal",
			},
			wantErr: false,
		},
		{
			name: "skip empty lines and comments together",
			content: `# Header comment
set system host-name router1

# Interface configuration
set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24

# BGP configuration
set protocols bgp group ibgp type internal
`,
			want: []string{
				"set system host-name router1",
				"set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24",
				"set protocols bgp group ibgp type internal",
			},
			wantErr: false,
		},
		{
			name: "whitespace handling",
			content: `  set system host-name router1
set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24
	set protocols bgp group ibgp type internal`,
			want: []string{
				"  set system host-name router1",
				"set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24",
				"	set protocols bgp group ibgp type internal",
			},
			wantErr: false,
		},
		{
			name:    "empty file",
			content: "",
			want:    []string{},
			wantErr: false,
		},
		{
			name: "only comments",
			content: `# Comment 1
# Comment 2
### Comment 3`,
			want:    []string{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "test.conf")
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}

			got, err := readConfigFile(tmpFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("readConfigFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("readConfigFile() length = %d, want %d", len(got), len(tt.want))
				t.Logf("Got: %#v", got)
				t.Logf("Want: %#v", tt.want)
				return
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("readConfigFile()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestReadConfigFile_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentFile := filepath.Join(tmpDir, "nonexistent.conf")

	_, err := readConfigFile(nonExistentFile)
	if err == nil {
		t.Error("readConfigFile() expected error for non-existent file, got nil")
	}

	// Check that the error mentions "cannot open file"
	if err != nil && !contains(err.Error(), "cannot open file") {
		t.Errorf("readConfigFile() error message = %q, want to contain 'cannot open file'", err.Error())
	}
}

func TestReadConfigFile_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test when running as root")
	}

	// Create temporary file with no read permission
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "no-read.conf")
	if err := os.WriteFile(tmpFile, []byte("set system host-name router1"), 0000); err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	_, err := readConfigFile(tmpFile)
	if err == nil {
		t.Error("readConfigFile() expected error for permission denied, got nil")
	}
}

func TestReadConfigFile_PreservesOriginalLines(t *testing.T) {
	content := `set system host-name router1
  set interfaces ge-0/0/0 description "WAN Interface"
	set protocols bgp group ibgp type internal  `

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.conf")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	got, err := readConfigFile(tmpFile)
	if err != nil {
		t.Fatalf("readConfigFile() error = %v", err)
	}

	// Verify that leading/trailing whitespace is preserved
	if got[1] != "  set interfaces ge-0/0/0 description \"WAN Interface\"" {
		t.Errorf("readConfigFile() did not preserve leading whitespace: got %q", got[1])
	}
	if got[2] != "\tset protocols bgp group ibgp type internal  " {
		t.Errorf("readConfigFile() did not preserve trailing whitespace: got %q", got[2])
	}
}

func TestReadConfigFile_LargeFile(t *testing.T) {
	// Test with a file containing many lines
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "large.conf")

	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	// Write 1000 lines
	for i := 0; i < 1000; i++ {
		if _, err := f.WriteString("set system host-name router1\n"); err != nil {
			t.Fatalf("WriteString failed: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	got, err := readConfigFile(tmpFile)
	if err != nil {
		t.Fatalf("readConfigFile() error = %v", err)
	}

	if len(got) != 1000 {
		t.Errorf("readConfigFile() length = %d, want 1000", len(got))
	}
}

func TestReadConfigFile_WithUTF8(t *testing.T) {
	content := `set system host-name router1
set interfaces ge-0/0/0 description "日本語 UTF-8"
set interfaces xe-0/1/0 description "Über Netzwerk"`

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "utf8.conf")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	got, err := readConfigFile(tmpFile)
	if err != nil {
		t.Fatalf("readConfigFile() error = %v", err)
	}

	if len(got) != 3 {
		t.Errorf("readConfigFile() length = %d, want 3", len(got))
	}

	if !contains(got[1], "日本語 UTF-8") {
		t.Errorf("readConfigFile() did not preserve UTF-8: got %q", got[1])
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
