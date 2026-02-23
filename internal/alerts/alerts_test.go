package alerts

import "testing"

func TestStatusFor(t *testing.T) {
	tests := []struct {
		name      string
		current   float64
		threshold float64
		want      string
	}{
		{"ok-low", 10, 90, "ok"},
		{"ok-medium", 50, 90, "ok"},
		{"ok-below-warning", 80, 90, "ok"},
		{"warning-at-90pct", 81, 90, "warning"},
		{"warning-high", 89, 90, "warning"},
		{"critical-at-threshold", 90, 90, "critical"},
		{"critical-above", 95, 90, "critical"},
		{"critical-100", 100, 90, "critical"},
		{"zero-threshold", 0, 0, "critical"},
		{"custom-threshold-ok", 40, 50, "ok"},
		{"custom-threshold-warning", 46, 50, "warning"},
		{"custom-threshold-critical", 50, 50, "critical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := statusFor(tt.current, tt.threshold)
			if got != tt.want {
				t.Errorf("statusFor(%f, %f) = %q, want %q", tt.current, tt.threshold, got, tt.want)
			}
		})
	}
}
