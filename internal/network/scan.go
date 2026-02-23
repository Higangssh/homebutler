package network

import (
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Higangssh/homebutler/internal/util"
)

type Device struct {
	IP       string `json:"ip"`
	MAC      string `json:"mac,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	Status   string `json:"status"`
}

// Scan discovers devices on the local network.
// It determines the local subnet, pings all addresses, then reads the ARP table.
func Scan() ([]Device, error) {
	subnet, err := getLocalSubnet()
	if err != nil {
		return nil, fmt.Errorf("failed to determine local subnet: %w", err)
	}

	// Ping sweep to populate ARP table
	pingSweep(subnet)

	// Read ARP table
	devices, err := readARP()
	if err != nil {
		return nil, fmt.Errorf("failed to read ARP table: %w", err)
	}

	// Resolve hostnames
	for i := range devices {
		names, err := net.LookupAddr(devices[i].IP)
		if err == nil && len(names) > 0 {
			devices[i].Hostname = strings.TrimSuffix(names[0], ".")
		}
	}

	return devices, nil
}

func getLocalSubnet() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ip := ipnet.IP.To4()
				mask := ipnet.Mask
				// Calculate network address
				network := make(net.IP, 4)
				for i := 0; i < 4; i++ {
					network[i] = ip[i] & mask[i]
				}
				ones, _ := mask.Size()
				return fmt.Sprintf("%s/%d", network.String(), ones), nil
			}
		}
	}
	return "", fmt.Errorf("no suitable network interface found")
}

func pingSweep(subnet string) {
	ip, ipnet, err := net.ParseCIDR(subnet)
	if err != nil {
		return
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 50) // limit concurrency

	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); incrementIP(ip) {
		target := ip.String()
		wg.Add(1)
		sem <- struct{}{}
		go func(t string) {
			defer wg.Done()
			defer func() { <-sem }()
			pingHost(t)
		}(target)
	}

	wg.Wait()
}

func pingHost(ip string) {
	switch runtime.GOOS {
	case "darwin":
		util.RunCmd("ping", "-c", "1", "-W", "500", ip)
	case "linux":
		util.RunCmd("ping", "-c", "1", "-W", "1", ip)
	case "windows":
		util.RunCmd("ping", "-n", "1", "-w", "500", ip)
	}
}

func readARP() ([]Device, error) {
	var out string
	var err error

	switch runtime.GOOS {
	case "darwin":
		out, err = util.RunCmd("/usr/sbin/arp", "-an")
	case "linux":
		out, err = util.RunCmd("arp", "-an")
	case "windows":
		out, err = util.RunCmd("arp", "-a")
	default:
		return nil, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	if err != nil {
		return nil, err
	}

	var devices []Device
	seen := make(map[string]bool)

	for _, line := range strings.Split(out, "\n") {
		ip, mac := parseARPLine(line)
		if ip == "" || mac == "" || mac == "(incomplete)" || mac == "ff:ff:ff:ff:ff:ff" ||
			strings.HasPrefix(ip, "224.") || strings.HasPrefix(ip, "239.") || strings.HasPrefix(ip, "255.") {
			// Filter out multicast (224.x, 239.x) and broadcast addresses
			continue
		}
		if seen[ip] {
			continue
		}
		seen[ip] = true

		devices = append(devices, Device{
			IP:     ip,
			MAC:    mac,
			Status: "up",
		})
	}

	return devices, nil
}

func parseARPLine(line string) (string, string) {
	// macOS/Linux: ? (192.168.0.1) at aa:bb:cc:dd:ee:ff on en0 ifscope [ethernet]
	// Windows:     192.168.0.1       aa-bb-cc-dd-ee-ff     dynamic
	line = strings.TrimSpace(line)

	if strings.Contains(line, ") at ") {
		// Unix format
		parts := strings.Fields(line)
		if len(parts) >= 4 {
			ip := strings.Trim(parts[1], "()")
			mac := parts[3]
			return ip, mac
		}
	} else {
		// Windows format
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			ip := parts[0]
			mac := strings.ReplaceAll(parts[1], "-", ":")
			if net.ParseIP(ip) != nil {
				return ip, mac
			}
		}
	}
	return "", ""
}

func incrementIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

// ScanWithTimeout runs scan with a timeout.
func ScanWithTimeout(timeout time.Duration) ([]Device, error) {
	type result struct {
		devices []Device
		err     error
	}

	ch := make(chan result, 1)
	go func() {
		devices, err := Scan()
		ch <- result{devices, err}
	}()

	select {
	case r := <-ch:
		return r.devices, r.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("network scan timed out after %s", timeout)
	}
}
