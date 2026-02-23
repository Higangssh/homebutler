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

type AlertItem struct {
	Status    string  `json:"status"`
	Current   float64 `json:"current"`
	Threshold float64 `json:"threshold"`
}

type DiskAlert struct {
	Mount     string  `json:"mount"`
	Status    string  `json:"status"`
	Current   float64 `json:"current"`
	Threshold float64 `json:"threshold"`
}

func Check(cfg *config.AlertConfig) (*AlertResult, error) {
	info, err := system.Status()
	if err != nil {
		return nil, err
	}

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

	return result, nil
}

func statusFor(current, threshold float64) string {
	if current >= threshold {
		return "critical"
	}
	if current >= threshold*0.9 {
		return "warning"
	}
	return "ok"
}
