package watch

import "time"

type FlappingConfig struct {
	ShortWindow    time.Duration `yaml:"short_window" json:"short_window"`
	ShortThreshold int           `yaml:"short_threshold" json:"short_threshold"`
	LongWindow     time.Duration `yaml:"long_window" json:"long_window"`
	LongThreshold  int           `yaml:"long_threshold" json:"long_threshold"`
}

type FlappingResult struct {
	IsFlapping bool
	Level      string
	Count      int
	Window     string
	Since      time.Time
}

func DefaultFlappingConfig() FlappingConfig {
	return FlappingConfig{
		ShortWindow:    10 * time.Minute,
		ShortThreshold: 3,
		LongWindow:     24 * time.Hour,
		LongThreshold:  5,
	}
}

// Check classifies an incident against the history it belongs to.
//
// The decision uses two fields, Container and DetectedAt, both of which an
// incident's filename already carries. Callers on the hot path — every save,
// and every alert rule evaluation — should use CheckRefs instead so they never
// read an incident body to answer a question about its name.
func (fc *FlappingConfig) Check(container string, incidents []Incident, now time.Time) FlappingResult {
	refs := make([]IncidentRef, len(incidents))
	for i, inc := range incidents {
		refs[i] = IncidentRef{ID: inc.ID, Container: inc.Container, DetectedAt: inc.DetectedAt}
	}
	return fc.CheckRefs(container, refs, now)
}

// CheckRefs is Check over incident references.
func (fc *FlappingConfig) CheckRefs(container string, refs []IncidentRef, now time.Time) FlappingResult {
	shortCutoff := now.Add(-fc.ShortWindow)
	longCutoff := now.Add(-fc.LongWindow)

	var shortCount, longCount int
	var shortOldest, longOldest time.Time

	for _, inc := range refs {
		if inc.Container != container {
			continue
		}
		if !inc.DetectedAt.Before(shortCutoff) {
			shortCount++
			if shortOldest.IsZero() || inc.DetectedAt.Before(shortOldest) {
				shortOldest = inc.DetectedAt
			}
		}
		if !inc.DetectedAt.Before(longCutoff) {
			longCount++
			if longOldest.IsZero() || inc.DetectedAt.Before(longOldest) {
				longOldest = inc.DetectedAt
			}
		}
	}

	if shortCount >= fc.ShortThreshold {
		return FlappingResult{
			IsFlapping: true,
			Level:      "acute",
			Count:      shortCount,
			Window:     "short",
			Since:      shortOldest,
		}
	}

	if longCount >= fc.LongThreshold {
		return FlappingResult{
			IsFlapping: true,
			Level:      "chronic",
			Count:      longCount,
			Window:     "long",
			Since:      longOldest,
		}
	}

	return FlappingResult{Level: "none"}
}
