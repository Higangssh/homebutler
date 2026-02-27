package ports

import "testing"

func TestSplitAddrPort(t *testing.T) {
	tests := []struct {
		in       string
		wantAddr string
		wantPort string
	}{
		{"127.0.0.1:8080", "127.0.0.1", "8080"},
		{"*:443", "*", "443"},
		{"[::1]:3000", "[::1]", "3000"},
		{":::22", "::", "22"},
		{"noport", "noport", ""},
	}
	for _, tc := range tests {
		a, p := splitAddrPort(tc.in)
		if a != tc.wantAddr || p != tc.wantPort {
			t.Fatalf("splitAddrPort(%q) = (%q,%q), want (%q,%q)", tc.in, a, p, tc.wantAddr, tc.wantPort)
		}
	}
}
