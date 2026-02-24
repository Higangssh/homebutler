package wake

import (
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"strings"
)

var macRegex = regexp.MustCompile(`^([0-9A-Fa-f]{2}[:-]){5}([0-9A-Fa-f]{2})$`)

// WakeResult holds the result of a WOL operation.
type WakeResult struct {
	Action    string `json:"action"`
	MAC       string `json:"mac"`
	Broadcast string `json:"broadcast"`
	Status    string `json:"status"`
}

// Send transmits a Wake-on-LAN magic packet to the given MAC address.
func Send(mac string, broadcast string) (*WakeResult, error) {
	if !macRegex.MatchString(mac) {
		return nil, fmt.Errorf("invalid MAC address: %s (expected format: AA:BB:CC:DD:EE:FF)", mac)
	}

	macBytes, err := parseMac(mac)
	if err != nil {
		return nil, err
	}

	// Build magic packet: 6x 0xFF + 16x MAC address
	packet := make([]byte, 0, 102)
	for i := 0; i < 6; i++ {
		packet = append(packet, 0xFF)
	}
	for i := 0; i < 16; i++ {
		packet = append(packet, macBytes...)
	}

	addr := fmt.Sprintf("%s:9", broadcast)
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}
	defer conn.Close()

	_, err = conn.Write(packet)
	if err != nil {
		return nil, fmt.Errorf("failed to send magic packet: %w", err)
	}

	return &WakeResult{Action: "wake", MAC: mac, Broadcast: broadcast, Status: "sent"}, nil
}

func parseMac(mac string) ([]byte, error) {
	clean := strings.ReplaceAll(mac, ":", "")
	clean = strings.ReplaceAll(clean, "-", "")
	return hex.DecodeString(clean)
}
