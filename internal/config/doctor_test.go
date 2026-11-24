package config

import (
	"strings"
	"testing"
	"time"
)

// `image_updates: false` is the spelling a person reaches for first. yaml's
// own message names a Go type; this one says what to write.
func TestImageUpdatesWrittenAsAValueSaysWhatToWrite(t *testing.T) {
	for _, body := range []string{"image_updates: false", "image_updates: [a, b]"} {
		_, err := Load(writeConfig(t, "doctor:\n  "+body+"\n"))
		if err == nil || !strings.Contains(err.Error(), "enabled: false") || strings.Contains(err.Error(), "config.") {
			t.Fatalf("%s: err = %v", body, err)
		}
	}
}

func TestImageUpdatesTTLIsHeldToARange(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"1s":    MinImageUpdatesTTL,
		"5m":    5 * time.Minute,
		"720h":  MaxImageUpdatesTTL,
		"-1h":   DefaultImageUpdatesTTL,
		"bogus": DefaultImageUpdatesTTL,
	} {
		if got := (ImageUpdatesConfig{CacheTTL: in}).TTL(); got != want {
			t.Errorf("TTL(%q) = %s, want %s", in, got, want)
		}
	}
	r := Validate(writeConfig(t, "doctor:\n  image_updates:\n    cache_ttl: 30s\n"))
	f := requireFinding(t, r, "doctor.image_updates.cache_ttl", SeverityWarning)
	if !strings.Contains(f.Message, "5m0s is used") {
		t.Fatalf("message = %q, want it to say what is used", f.Message)
	}
}

// Leaving the block out keeps the check on; only an explicit false turns it
// off. A plain bool could not tell those apart.
func TestImageUpdatesDefaultsOn(t *testing.T) {
	cfg, err := Load(writeConfig(t, "alerts:\n  cpu: 90\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Doctor.ImageUpdates.On() || cfg.Doctor.ImageUpdates.TTL() != DefaultImageUpdatesTTL {
		t.Fatalf("defaults = %+v", cfg.Doctor.ImageUpdates)
	}

	cfg, err = Load(writeConfig(t, "doctor:\n  image_updates:\n    enabled: false\n    cache_ttl: 30m\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Doctor.ImageUpdates.On() {
		t.Fatal("enabled: false left the check on")
	}
	if cfg.Doctor.ImageUpdates.TTL() != 30*time.Minute {
		t.Fatalf("ttl = %s, want 30m", cfg.Doctor.ImageUpdates.TTL())
	}
}

func TestValidateReportsAnUnreadableCacheTTL(t *testing.T) {
	r := Validate(writeConfig(t, "doctor:\n  image_updates:\n    cache_ttl: 6 hours\n"))
	requireFinding(t, r, "doctor.image_updates.cache_ttl", SeverityWarning)

	r = Validate(writeConfig(t, "doctor:\n  image_updates:\n    cache_ttl: 6h\n    enabeld: false\n"))
	if _, ok := findingFor(r, "doctor.image_updates.cache_ttl"); ok {
		t.Fatalf("6h was reported: %+v", r.Findings)
	}
	requireFinding(t, r, "enabeld", SeverityWarning)
}
