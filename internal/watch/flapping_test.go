package watch

import (
	"encoding/json"
	"testing"
	"time"
)

func incident(container string, detectedAt time.Time) Incident {
	return Incident{Container: container, DetectedAt: detectedAt}
}

func TestFlapping(t *testing.T) {
	cfg := DefaultFlappingConfig()
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)

	t.Run("empty incidents", func(t *testing.T) {
		r := cfg.Check("web", nil, now)
		if r.IsFlapping {
			t.Fatal("expected IsFlapping=false for empty incidents")
		}
		if r.Level != "none" {
			t.Fatalf("expected level none, got %s", r.Level)
		}
	})

	t.Run("below threshold", func(t *testing.T) {
		incidents := []Incident{
			incident("web", now.Add(-5*time.Minute)),
			incident("web", now.Add(-8*time.Minute)),
		}
		r := cfg.Check("web", incidents, now)
		if r.IsFlapping {
			t.Fatal("expected IsFlapping=false below threshold")
		}
	})

	t.Run("short window exactly at threshold", func(t *testing.T) {
		incidents := []Incident{
			incident("web", now.Add(-2*time.Minute)),
			incident("web", now.Add(-5*time.Minute)),
			incident("web", now.Add(-9*time.Minute)),
		}
		r := cfg.Check("web", incidents, now)
		if !r.IsFlapping {
			t.Fatal("expected IsFlapping=true")
		}
		if r.Level != "acute" {
			t.Fatalf("expected acute, got %s", r.Level)
		}
		if r.Count != 3 {
			t.Fatalf("expected count 3, got %d", r.Count)
		}
		if r.Window != "short" {
			t.Fatalf("expected window short, got %s", r.Window)
		}
	})

	t.Run("short window above threshold", func(t *testing.T) {
		incidents := []Incident{
			incident("web", now.Add(-1*time.Minute)),
			incident("web", now.Add(-3*time.Minute)),
			incident("web", now.Add(-6*time.Minute)),
			incident("web", now.Add(-8*time.Minute)),
		}
		r := cfg.Check("web", incidents, now)
		if !r.IsFlapping {
			t.Fatal("expected IsFlapping=true")
		}
		if r.Level != "acute" {
			t.Fatalf("expected acute, got %s", r.Level)
		}
		if r.Count != 4 {
			t.Fatalf("expected count 4, got %d", r.Count)
		}
	})

	t.Run("long window only", func(t *testing.T) {
		incidents := []Incident{
			incident("web", now.Add(-1*time.Hour)),
			incident("web", now.Add(-3*time.Hour)),
			incident("web", now.Add(-6*time.Hour)),
			incident("web", now.Add(-12*time.Hour)),
			incident("web", now.Add(-20*time.Hour)),
		}
		r := cfg.Check("web", incidents, now)
		if !r.IsFlapping {
			t.Fatal("expected IsFlapping=true")
		}
		if r.Level != "chronic" {
			t.Fatalf("expected chronic, got %s", r.Level)
		}
		if r.Count != 5 {
			t.Fatalf("expected count 5, got %d", r.Count)
		}
		if r.Window != "long" {
			t.Fatalf("expected window long, got %s", r.Window)
		}
	})

	t.Run("short and long both met prefers acute", func(t *testing.T) {
		incidents := []Incident{
			incident("web", now.Add(-1*time.Minute)),
			incident("web", now.Add(-3*time.Minute)),
			incident("web", now.Add(-5*time.Minute)),
			incident("web", now.Add(-2*time.Hour)),
			incident("web", now.Add(-10*time.Hour)),
		}
		r := cfg.Check("web", incidents, now)
		if !r.IsFlapping {
			t.Fatal("expected IsFlapping=true")
		}
		if r.Level != "acute" {
			t.Fatalf("expected acute, got %s", r.Level)
		}
		if r.Window != "short" {
			t.Fatalf("expected window short, got %s", r.Window)
		}
	})

	t.Run("boundary inclusive exactly 10m ago", func(t *testing.T) {
		incidents := []Incident{
			incident("web", now.Add(-1*time.Minute)),
			incident("web", now.Add(-5*time.Minute)),
			incident("web", now.Add(-10*time.Minute)),
		}
		r := cfg.Check("web", incidents, now)
		if !r.IsFlapping {
			t.Fatal("expected IsFlapping=true, boundary should be inclusive")
		}
		if r.Level != "acute" {
			t.Fatalf("expected acute, got %s", r.Level)
		}
	})

	t.Run("boundary exclusive 10m+1ns ago", func(t *testing.T) {
		incidents := []Incident{
			incident("web", now.Add(-1*time.Minute)),
			incident("web", now.Add(-5*time.Minute)),
			incident("web", now.Add(-10*time.Minute-1*time.Nanosecond)),
		}
		r := cfg.Check("web", incidents, now)
		if r.IsFlapping {
			t.Fatal("expected IsFlapping=false, 10m+1ns should be excluded from short window")
		}
	})

	t.Run("filters by container", func(t *testing.T) {
		incidents := []Incident{
			incident("web", now.Add(-1*time.Minute)),
			incident("db", now.Add(-2*time.Minute)),
			incident("web", now.Add(-3*time.Minute)),
			incident("db", now.Add(-4*time.Minute)),
			incident("cache", now.Add(-5*time.Minute)),
		}
		r := cfg.Check("web", incidents, now)
		if r.IsFlapping {
			t.Fatal("expected IsFlapping=false, only 2 web incidents")
		}
	})

	t.Run("reverse order input", func(t *testing.T) {
		incidents := []Incident{
			incident("web", now.Add(-9*time.Minute)),
			incident("web", now.Add(-5*time.Minute)),
			incident("web", now.Add(-1*time.Minute)),
		}
		r := cfg.Check("web", incidents, now)
		if !r.IsFlapping {
			t.Fatal("expected IsFlapping=true regardless of input order")
		}
		if r.Level != "acute" {
			t.Fatalf("expected acute, got %s", r.Level)
		}
	})

	t.Run("multiple incidents same second", func(t *testing.T) {
		ts := now.Add(-5 * time.Minute)
		incidents := []Incident{
			incident("web", ts),
			incident("web", ts),
			incident("web", ts),
		}
		r := cfg.Check("web", incidents, now)
		if !r.IsFlapping {
			t.Fatal("expected IsFlapping=true, same-second incidents all count")
		}
		if r.Count != 3 {
			t.Fatalf("expected count 3, got %d", r.Count)
		}
	})
}

// An incident is a file on disk, and every one saved before 0.41.2 wrote the
// flapping record as IsFlapping, Count and the rest. Reading those back must
// still find them; the new name alone would not.
func TestFlappingReadsKeysSavedBeforeTheRename(t *testing.T) {
	old := `{"IsFlapping":true,"Level":"acute","Count":4,"Window":"short","Since":"2026-09-30T12:00:00Z"}`
	var got FlappingResult
	if err := json.Unmarshal([]byte(old), &got); err != nil {
		t.Fatal(err)
	}
	if !got.IsFlapping || got.Level != "acute" || got.Count != 4 || got.Window != "short" || got.Since.IsZero() {
		t.Errorf("an incident saved by 0.41.1 read back as %+v", got)
	}

	out, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"is_flapping":true,"level":"acute","count":4,"window":"short","since":"2026-09-30T12:00:00Z"}` {
		t.Errorf("written as %s", out)
	}
	var again FlappingResult
	if err := json.Unmarshal(out, &again); err != nil || again != got {
		t.Errorf("the new keys did not read back: %+v, %v", again, err)
	}
}
