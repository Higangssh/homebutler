package alerts

import (
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/system"
)

type AlertResult struct {
	CPU    AlertItem   `json:"cpu"`
	Memory AlertItem   `json:"memory"`
	Disks  []DiskAlert `json:"disks"`
}

// Level is how close a resource is to its threshold. It is one of three
// words and an agent branches on which, so the words are frozen at 1.0.
type Level string

const (
	LevelOK       Level = "ok"
	LevelWarning  Level = "warning"
	LevelCritical Level = "critical"
)

// Levels is the whole vocabulary, in rising order.
func Levels() []Level {
	return []Level{LevelOK, LevelWarning, LevelCritical}
}

type AlertItem struct {
	Status    Level   `json:"status"`
	Current   float64 `json:"current"`
	Threshold float64 `json:"threshold"`
}

type DiskAlert struct {
	Mount     string  `json:"mount"`
	Status    Level   `json:"status"`
	Current   float64 `json:"current"`
	Threshold float64 `json:"threshold"`
}

func Check(cfg *config.AlertConfig) (*AlertResult, error) {
	info, err := system.Status()
	if err != nil {
		return nil, err
	}
	return CheckWithStatus(cfg, info), nil
}

func CheckWithStatus(cfg *config.AlertConfig, info *system.StatusInfo) *AlertResult {
	result := &AlertResult{
		CPU: AlertItem{
			Status:    statusFor(info.CPU.UsagePercent, cfg.CPU),
			Current:   info.CPU.UsagePercent,
			Threshold: cfg.CPU,
		},
		Memory: AlertItem{
			Status:    statusFor(info.Memory.Percent, cfg.Memory),
			Current:   info.Memory.Percent,
			Threshold: cfg.Memory,
		},
	}

	for _, d := range info.Disks {
		result.Disks = append(result.Disks, DiskAlert{
			Mount:     d.Mount,
			Status:    statusFor(d.Percent, cfg.Disk),
			Current:   d.Percent,
			Threshold: cfg.Disk,
		})
	}

	return result
}

func statusFor(current, threshold float64) Level {
	if current >= threshold {
		return LevelCritical
	}
	if current >= threshold*0.9 {
		return LevelWarning
	}
	return LevelOK
}
