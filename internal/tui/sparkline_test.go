package tui

import "testing"

func TestSparkline_Empty(t *testing.T) {
	got := sparkline(nil, 10)
	if got != "" {
		t.Errorf("expected empty string for nil data, got %q", got)
	}
	got = sparkline([]float64{}, 10)
	if got != "" {
		t.Errorf("expected empty string for empty data, got %q", got)
	}
}

func TestSparkline_ZeroWidth(t *testing.T) {
	got := sparkline([]float64{50}, 0)
	if got != "" {
		t.Errorf("expected empty string for zero width, got %q", got)
	}
}

func TestSparkline_SingleValue(t *testing.T) {
	got := sparkline([]float64{50}, 10)
	runes := []rune(got)
	if len(runes) != 1 {
		t.Errorf("expected 1 character, got %d: %q", len(runes), got)
	}
}

func TestSparkline_PartialFill(t *testing.T) {
	data := []float64{10, 30, 50, 70, 90}
	got := sparkline(data, 20)
	runes := []rune(got)
	if len(runes) != 5 {
		t.Errorf("expected 5 characters for partial data, got %d", len(runes))
	}
}

func TestSparkline_Full(t *testing.T) {
	data := make([]float64, 20)
	for i := range data {
		data[i] = float64(i) * 5 // 0, 5, 10, ..., 95
	}
	got := sparkline(data, 20)
	runes := []rune(got)
	if len(runes) != 20 {
		t.Errorf("expected 20 characters, got %d", len(runes))
	}
}

func TestSparkline_OverflowTrimmed(t *testing.T) {
	data := make([]float64, 100)
	for i := range data {
		data[i] = float64(i)
	}
	got := sparkline(data, 10)
	runes := []rune(got)
	if len(runes) != 10 {
		t.Errorf("expected 10 characters (trimmed to width), got %d", len(runes))
	}
}

func TestSparkline_BoundaryValues(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		char  rune
	}{
		{"zero", 0, '▁'},
		{"hundred", 100, '█'},
		{"negative", -10, '▁'},
		{"over hundred", 150, '█'},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sparkline([]float64{tt.value}, 1)
			runes := []rune(got)
			if len(runes) != 1 || runes[0] != tt.char {
				t.Errorf("sparkline([%f], 1) = %q, want %c", tt.value, got, tt.char)
			}
		})
	}
}

func TestSparkline_MonotonicIncrease(t *testing.T) {
	// Verify that increasing values produce non-decreasing block indices
	data := []float64{0, 14, 28, 42, 57, 71, 85, 100}
	got := sparkline(data, 10)
	runes := []rune(got)
	for i := 1; i < len(runes); i++ {
		if runes[i] < runes[i-1] {
			t.Errorf("expected non-decreasing blocks, but index %d < %d", i, i-1)
		}
	}
}

func TestAppendHistory(t *testing.T) {
	var h []float64
	for i := 0; i < 70; i++ {
		h = appendHistory(h, float64(i))
	}
	if len(h) != maxHistory {
		t.Errorf("expected maxHistory=%d entries, got %d", maxHistory, len(h))
	}
	if h[0] != 10 {
		t.Errorf("expected first entry 10 after overflow, got %f", h[0])
	}
	if h[len(h)-1] != 69 {
		t.Errorf("expected last entry 69, got %f", h[len(h)-1])
	}
}

func TestAppendHistory_UnderMax(t *testing.T) {
	var h []float64
	for i := 0; i < 5; i++ {
		h = appendHistory(h, float64(i))
	}
	if len(h) != 5 {
		t.Errorf("expected 5 entries, got %d", len(h))
	}
}

func TestSparklineColor(t *testing.T) {
	tests := []struct {
		name string
		data []float64
	}{
		{"empty", nil},
		{"green low", []float64{30}},
		{"yellow mid", []float64{60}},
		{"red high", []float64{90}},
		{"boundary 50", []float64{50}},
		{"boundary 80", []float64{80}},
		{"boundary 81", []float64{81}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			style := sparklineColor(tt.data)
			_ = style.Render("test") // must not panic
		})
	}
}
