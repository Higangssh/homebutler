package wake

import "testing"

func TestMacRegex(t *testing.T) {
	tests := []struct {
		name  string
		mac   string
		valid bool
	}{
		{"colon-separated", "AA:BB:CC:DD:EE:FF", true},
		{"hyphen-separated", "AA-BB-CC-DD-EE-FF", true},
		{"lowercase", "aa:bb:cc:dd:ee:ff", true},
		{"mixed-case", "aA:bB:cC:dD:eE:fF", true},
		{"too-short", "AA:BB:CC:DD:EE", false},
		{"too-long", "AA:BB:CC:DD:EE:FF:00", false},
		{"invalid-chars", "GG:HH:II:JJ:KK:LL", false},
		{"no-separator", "AABBCCDDEEFF", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := macRegex.MatchString(tt.mac)
			if got != tt.valid {
				t.Errorf("macRegex.MatchString(%q) = %v, want %v", tt.mac, got, tt.valid)
			}
		})
	}
}

func TestParseMac(t *testing.T) {
	bytes, err := parseMac("AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bytes) != 6 {
		t.Errorf("expected 6 bytes, got %d", len(bytes))
	}
	if bytes[0] != 0xAA || bytes[5] != 0xFF {
		t.Errorf("unexpected bytes: %x", bytes)
	}

	// Hyphen separated
	bytes, err = parseMac("11-22-33-44-55-66")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bytes[0] != 0x11 || bytes[5] != 0x66 {
		t.Errorf("unexpected bytes: %x", bytes)
	}
}
