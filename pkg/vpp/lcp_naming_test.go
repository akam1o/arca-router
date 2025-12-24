package vpp

import (
	"testing"
)

func TestConvertJunosToLinuxName(t *testing.T) {
	tests := []struct {
		name      string
		junosName string
		want      string
		wantErr   bool
	}{
		{
			name:      "basic gigabit ethernet",
			junosName: "ge-0/0/0",
			want:      "ge0-0-0",
			wantErr:   false,
		},
		{
			name:      "10g ethernet",
			junosName: "xe-1/2/3",
			want:      "xe1-2-3",
			wantErr:   false,
		},
		{
			name:      "100g ethernet",
			junosName: "et-0/1/2",
			want:      "et0-1-2",
			wantErr:   false,
		},
		{
			name:      "two digit port",
			junosName: "ge-0/0/10",
			want:      "ge0-0-10",
			wantErr:   false,
		},
		{
			name:      "all two digits",
			junosName: "xe-10/20/30",
			want:      "xe10-20-30",
			wantErr:   false,
		},
		{
			name:      "subinterface with VLAN",
			junosName: "ge-0/0/0.10",
			want:      "ge0-0-0v10",
			wantErr:   false,
		},
		{
			name:      "subinterface with 3-digit VLAN",
			junosName: "ge-0/0/0.100",
			want:      "ge0-0-0v100",
			wantErr:   false,
		},
		{
			name:      "subinterface with 4-digit VLAN",
			junosName: "xe-1/2/3.4094",
			want:      "xe1-2-3v4094",
			wantErr:   false,
		},
		{
			name:      "empty name",
			junosName: "",
			want:      "",
			wantErr:   true,
		},
		{
			name:      "invalid format - missing slashes",
			junosName: "ge-0-0-0",
			want:      "",
			wantErr:   true,
		},
		{
			name:      "invalid format - missing hyphen",
			junosName: "ge0/0/0",
			want:      "",
			wantErr:   true,
		},
		{
			name:      "invalid format - no numbers",
			junosName: "ge-a/b/c",
			want:      "",
			wantErr:   true,
		},
		{
			name:      "management interface",
			junosName: "fxp-0/0/0",
			want:      "fxp0-0-0",
			wantErr:   false,
		},
		{
			name:      "ambiguous case 1 (ge-1/11/1)",
			junosName: "ge-1/11/1",
			want:      "ge1-11-1",
			wantErr:   false,
		},
		{
			name:      "ambiguous case 2 (ge-11/1/1)",
			junosName: "ge-11/1/1",
			want:      "ge11-1-1",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertJunosToLinuxName(tt.junosName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertJunosToLinuxName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ConvertJunosToLinuxName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertJunosToLinuxName_LongNames(t *testing.T) {
	tests := []struct {
		name      string
		junosName string
		maxLen    int
		wantErr   bool
	}{
		{
			name:      "very long interface name",
			junosName: "ge-100/100/100.4094",
			maxLen:    MaxLinuxIfNameLen,
			wantErr:   false,
		},
		{
			name:      "extreme long name",
			junosName: "et-999/999/999.4094",
			maxLen:    MaxLinuxIfNameLen,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertJunosToLinuxName(tt.junosName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertJunosToLinuxName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) > tt.maxLen {
				t.Errorf("ConvertJunosToLinuxName() length = %d, max = %d, name = %s", len(got), tt.maxLen, got)
			}
		})
	}
}

func TestConvertJunosToLinuxName_Deterministic(t *testing.T) {
	// Test that hash generation is deterministic
	junosName := "ge-100/100/100.4094"

	// Convert multiple times
	name1, err1 := ConvertJunosToLinuxName(junosName)
	name2, err2 := ConvertJunosToLinuxName(junosName)
	name3, err3 := ConvertJunosToLinuxName(junosName)

	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("unexpected errors: %v, %v, %v", err1, err2, err3)
	}

	if name1 != name2 || name2 != name3 {
		t.Errorf("non-deterministic conversion: %s, %s, %s", name1, name2, name3)
	}
}

func TestConvertJunosToLinuxName_UniqueHashes(t *testing.T) {
	// Test that different Junos names produce different Linux names
	names := []string{
		"ge-100/100/100.4094",
		"xe-100/100/100.4094",
		"et-100/100/100.4094",
		"ge-100/100/101.4094",
		"ge-100/101/100.4094",
		"ge-101/100/100.4094",
		"ge-1/11/1",  // Ambiguous cases that should be distinct
		"ge-11/1/1",
	}

	linuxNames := make(map[string]string)
	for _, junosName := range names {
		linuxName, err := ConvertJunosToLinuxName(junosName)
		if err != nil {
			t.Fatalf("ConvertJunosToLinuxName(%s) error = %v", junosName, err)
		}

		if existingJunosName, exists := linuxNames[linuxName]; exists {
			t.Errorf("collision: %s and %s both map to %s", junosName, existingJunosName, linuxName)
		}

		linuxNames[linuxName] = junosName
	}
}

func TestValidateLinuxIfName(t *testing.T) {
	tests := []struct {
		name    string
		ifName  string
		wantErr bool
	}{
		{
			name:    "valid simple name",
			ifName:  "ge000",
			wantErr: false,
		},
		{
			name:    "valid with dot",
			ifName:  "ge000v10",
			wantErr: false,
		},
		{
			name:    "valid with dash",
			ifName:  "eth-0",
			wantErr: false,
		},
		{
			name:    "valid with underscore",
			ifName:  "eth_0",
			wantErr: false,
		},
		{
			name:    "valid max length (15 chars)",
			ifName:  "ge000v123456789",
			wantErr: false,
		},
		{
			name:    "empty name",
			ifName:  "",
			wantErr: true,
		},
		{
			name:    "too long (16 chars)",
			ifName:  "ge000v1234567890",
			wantErr: true,
		},
		{
			name:    "invalid char - space",
			ifName:  "ge 000",
			wantErr: true,
		},
		{
			name:    "invalid char - slash",
			ifName:  "ge/000",
			wantErr: true,
		},
		{
			name:    "invalid char - special",
			ifName:  "ge@000",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLinuxIfName(tt.ifName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateLinuxIfName() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsJunosInterfaceName(t *testing.T) {
	tests := []struct {
		name string
		input string
		want bool
	}{
		{
			name:  "valid ge interface",
			input: "ge-0/0/0",
			want:  true,
		},
		{
			name:  "valid xe interface",
			input: "xe-1/2/3",
			want:  true,
		},
		{
			name:  "valid with VLAN",
			input: "ge-0/0/0.10",
			want:  true,
		},
		{
			name:  "invalid - missing slashes",
			input: "ge-0-0-0",
			want:  false,
		},
		{
			name:  "invalid - missing hyphen",
			input: "ge0/0/0",
			want:  false,
		},
		{
			name:  "invalid - letters in numbers",
			input: "ge-a/b/c",
			want:  false,
		},
		{
			name:  "invalid - Linux format",
			input: "ge000",
			want:  false,
		},
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsJunosInterfaceName(tt.input)
			if got != tt.want {
				t.Errorf("IsJunosInterfaceName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseJunosInterfaceName(t *testing.T) {
	tests := []struct {
		name      string
		junosName string
		want      *JunosInterfaceComponents
		wantErr   bool
	}{
		{
			name:      "basic interface",
			junosName: "ge-0/0/0",
			want: &JunosInterfaceComponents{
				Type: "ge",
				FPC:  "0",
				PIC:  "0",
				Port: "0",
				VLAN: "",
			},
			wantErr: false,
		},
		{
			name:      "interface with VLAN",
			junosName: "xe-1/2/3.100",
			want: &JunosInterfaceComponents{
				Type: "xe",
				FPC:  "1",
				PIC:  "2",
				Port: "3",
				VLAN: "100",
			},
			wantErr: false,
		},
		{
			name:      "two digit numbers",
			junosName: "et-10/20/30",
			want: &JunosInterfaceComponents{
				Type: "et",
				FPC:  "10",
				PIC:  "20",
				Port: "30",
				VLAN: "",
			},
			wantErr: false,
		},
		{
			name:      "invalid format",
			junosName: "ge-0-0-0",
			want:      nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseJunosInterfaceName(tt.junosName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseJunosInterfaceName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.Type != tt.want.Type || got.FPC != tt.want.FPC ||
				got.PIC != tt.want.PIC || got.Port != tt.want.Port ||
				got.VLAN != tt.want.VLAN {
				t.Errorf("ParseJunosInterfaceName() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func BenchmarkConvertJunosToLinuxName(b *testing.B) {
	testCases := []string{
		"ge-0/0/0",
		"xe-1/2/3.100",
		"et-10/20/30",
		"ge-100/100/100.4094", // Will require hashing
	}

	for _, tc := range testCases {
		b.Run(tc, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _ = ConvertJunosToLinuxName(tc)
			}
		})
	}
}
