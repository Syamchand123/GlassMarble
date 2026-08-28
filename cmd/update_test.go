package cmd

import (
	"testing"
)

func TestVerifyChecksum(t *testing.T) {
	checksumsContent := []byte(`a1c455429e3b5b868a257fb01dd5450c21ea4739654ad9a2a86de9639cf85eef  gmb_1.0.0_darwin_amd64.tar.gz
4545fa05891ac7a6ceae0d56073df4e582730558b77413b796d1f1915dc180d5  gmb_1.0.0_linux_amd64.tar.gz
8d844c8eb5caeccebca5e3ba06e86ef46487e411b439c28eeb3eb80c85c2c770 *gmb_1.0.0_windows_amd64.zip
`)

	tests := []struct {
		name     string
		file     string
		hash     string
		expected bool
	}{
		{
			name:     "matching darwin archive",
			file:     "gmb_1.0.0_darwin_amd64.tar.gz",
			hash:     "a1c455429e3b5b868a257fb01dd5450c21ea4739654ad9a2a86de9639cf85eef",
			expected: true,
		},
		{
			name:     "matching windows archive with star",
			file:     "gmb_1.0.0_windows_amd64.zip",
			hash:     "8d844c8eb5caeccebca5e3ba06e86ef46487e411b439c28eeb3eb80c85c2c770",
			expected: true,
		},
		{
			name:     "case insensitive match",
			file:     "gmb_1.0.0_linux_amd64.tar.gz",
			hash:     "4545FA05891AC7A6CEAE0D56073DF4E582730558B77413B796D1F1915DC180D5",
			expected: true,
		},
		{
			name:     "mismatched hash",
			file:     "gmb_1.0.0_linux_amd64.tar.gz",
			hash:     "0000000000000000000000000000000000000000000000000000000000000000",
			expected: false,
		},
		{
			name:     "unknown file",
			file:     "unknown.tar.gz",
			hash:     "a1c455429e3b5b868a257fb01dd5450c21ea4739654ad9a2a86de9639cf85eef",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := verifyChecksum(checksumsContent, tt.file, tt.hash)
			if got != tt.expected {
				t.Errorf("verifyChecksum(%s, %s) = %v; want %v", tt.file, tt.hash, got, tt.expected)
			}
		})
	}
}

func TestUpdateCommandRegistration(t *testing.T) {
	root := RootCmd()
	found := false
	for _, c := range root.Commands() {
		if c.Name() == "update" {
			found = true
			if c.GroupID != GroupUtility.ID {
				t.Errorf("expected updateCmd in GroupUtility, got %s", c.GroupID)
			}
			break
		}
	}
	if !found {
		t.Fatal("updateCmd not registered in rootCmd")
	}
}
