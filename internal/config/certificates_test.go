package config

import "testing"

func TestCertificateEndpoint(t *testing.T) {
	ok := map[string][2]string{
		"home.example.com:443": {"home.example.com", "443"},
		"home.example.com":     {"home.example.com", "443"},
		"  nas.lan:8443 ":      {"nas.lan", "8443"},
		"192.168.0.10:8006":    {"192.168.0.10", "8006"},
		"[2001:db8::1]:443":    {"2001:db8::1", "443"},
		"[2001:db8::1]":        {"2001:db8::1", "443"},
	}
	for entry, want := range ok {
		host, port, err := CertificateEndpoint(entry)
		if err != nil || host != want[0] || port != want[1] {
			t.Errorf("CertificateEndpoint(%q) = %q, %q, %v; want %q, %q", entry, host, port, err, want[0], want[1])
		}
	}

	for _, entry := range []string{"", "https://home.example.com", "home.example.com/path", ":443", "home:0", "home:70000", "home:https", "2001:db8::1"} {
		if _, _, err := CertificateEndpoint(entry); err == nil {
			t.Errorf("CertificateEndpoint(%q) accepted an entry doctor could not dial", entry)
		}
	}
}

func TestValidateRejectsACertificateURL(t *testing.T) {
	path := writeConfig(t, `
certificates:
  - home.example.com:443
  - https://nas.example.com
`)
	r := Validate(path)

	f := requireFinding(t, r, "certificates[1]", SeverityError)
	if f.Hint == "" {
		t.Error("no hint for a URL in certificates")
	}
	if _, bad := findingFor(r, "certificates[0]"); bad {
		t.Errorf("a valid entry was reported: %+v", r.Findings)
	}
	for _, s := range r.Sections {
		if s.Name == "certificates" && (!s.Present || s.Summary != "2 endpoints") {
			t.Errorf("certificates section = %+v", s)
		}
	}
}
