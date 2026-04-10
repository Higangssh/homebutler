package watch

import "time"

type FlappingConfig struct {
	ShortWindow    time.Duration
	ShortThreshold int
	LongWindow     time.Duration
	LongThreshold  int
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

func (fc *FlappingConfig) Check(container string, incidents []Incident, now time.Time) FlappingResult {
	shortCutoff := now.Add(-fc.ShortWindow)
	longCutoff := now.Add(-fc.LongWindow)

	var shortCount, longCount int
	var shortOldest, longOldest time.Time

	for _, inc := range incidents {
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
