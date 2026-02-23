package network

import (
	"net"
	"testing"
)

func TestParseARPLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantIP  string
		wantMAC string
	}{
		{
			"macos-format",
			"? (192.168.0.1) at aa:bb:cc:dd:ee:ff on en0 ifscope [ethernet]",
			"192.168.0.1",
			"aa:bb:cc:dd:ee:ff",
		},
		{
			"incomplete",
			"? (192.168.0.5) at (incomplete) on en0 ifscope [ethernet]",
			"192.168.0.5",
			"(incomplete)",
		},
		{
			"empty-line",
			"",
			"",
			"",
		},
		{
			"header-line",
			"Address    HWtype  HWaddress           Flags Mask  Iface",
			"",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, mac := parseARPLine(tt.line)
			if ip != tt.wantIP {
				t.Errorf("IP = %q, want %q", ip, tt.wantIP)
			}
			if mac != tt.wantMAC {
				t.Errorf("MAC = %q, want %q", mac, tt.wantMAC)
			}
		})
	}
}

func TestIncrementIP(t *testing.T) {
	ip := net.IP{192, 168, 1, 0}
	incrementIP(ip)
	if ip.String() != "192.168.1.1" {
		t.Errorf("expected 192.168.1.1, got %s", ip.String())
	}

	ip = net.IP{192, 168, 1, 255}
	incrementIP(ip)
	if ip.String() != "192.168.2.0" {
		t.Errorf("expected 192.168.2.0, got %s", ip.String())
	}
}

func TestGetLocalSubnet(t *testing.T) {
	subnet, err := getLocalSubnet()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _, err = net.ParseCIDR(subnet)
	if err != nil {
		t.Errorf("invalid CIDR: %s", subnet)
	}
}
