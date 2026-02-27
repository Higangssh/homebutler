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

func TestParseMacAllBytes(t *testing.T) {
	bytes, err := parseMac("01:23:45:67:89:AB")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB}
	for i, b := range bytes {
		if b != expected[i] {
			t.Errorf("byte[%d] = %02x, want %02x", i, b, expected[i])
		}
	}
}

func TestParseMacLowercase(t *testing.T) {
	bytes, err := parseMac("aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bytes[0] != 0xAA || bytes[5] != 0xFF {
		t.Errorf("unexpected bytes: %x", bytes)
	}
}

func TestSendInvalidMAC(t *testing.T) {
	tests := []struct {
		name string
		mac  string
	}{
		{"empty", ""},
		{"too short", "AA:BB:CC"},
		{"invalid chars", "GG:HH:II:JJ:KK:LL"},
		{"no separator", "AABBCCDDEEFF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Send(tt.mac, "255.255.255.255")
			if err == nil {
				t.Errorf("Send(%q) expected error", tt.mac)
			}
		})
	}
}

func TestWakeResultStruct(t *testing.T) {
	r := WakeResult{
		Action:    "wake",
		MAC:       "AA:BB:CC:DD:EE:FF",
		Broadcast: "255.255.255.255",
		Status:    "sent",
	}
	if r.Action != "wake" {
		t.Errorf("Action = %q, want %q", r.Action, "wake")
	}
	if r.MAC != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("MAC = %q, want %q", r.MAC, "AA:BB:CC:DD:EE:FF")
	}
	if r.Status != "sent" {
		t.Errorf("Status = %q, want %q", r.Status, "sent")
	}
}

func TestMagicPacketStructure(t *testing.T) {
	// We can't directly test the packet construction since it's inside Send,
	// but we can verify parseMac returns correct bytes that would be used
	macBytes, err := parseMac("AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Build magic packet the same way as Send does
	packet := make([]byte, 0, 102)
	for i := 0; i < 6; i++ {
		packet = append(packet, 0xFF)
	}
	for i := 0; i < 16; i++ {
		packet = append(packet, macBytes...)
	}

	// Verify packet length: 6 + 16*6 = 102
	if len(packet) != 102 {
		t.Errorf("packet length = %d, want 102", len(packet))
	}

	// Verify header (6 bytes of 0xFF)
	for i := 0; i < 6; i++ {
		if packet[i] != 0xFF {
			t.Errorf("packet[%d] = %02x, want 0xFF", i, packet[i])
		}
	}

	// Verify 16 repetitions of MAC
	for rep := 0; rep < 16; rep++ {
		offset := 6 + rep*6
		for j := 0; j < 6; j++ {
			if packet[offset+j] != macBytes[j] {
				t.Errorf("packet[%d] (rep %d, byte %d) = %02x, want %02x",
					offset+j, rep, j, packet[offset+j], macBytes[j])
			}
		}
	}
}
