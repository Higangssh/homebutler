package alerts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/docker"
	"github.com/Higangssh/homebutler/internal/system"
)

// Event represents a single alert event fired by the watcher.
type Event struct {
	Time     time.Time `json:"time"`
	Severity string    `json:"severity"` // "warning", "critical"
	Resource string    `json:"resource"` // "cpu", "memory", "disk:/mount", "container:name"
	Message  string    `json:"message"`
	Current  float64   `json:"current,omitempty"`
}

// WatchConfig holds watcher parameters.
type WatchConfig struct {
	Interval time.Duration
	Alert    config.AlertConfig
}

// DefaultWatchConfig returns sensible defaults.
func DefaultWatchConfig() WatchConfig {
	return WatchConfig{
		Interval: 30 * time.Second,
		Alert: config.AlertConfig{
			CPU:    90,
			Memory: 85,
			Disk:   90,
		},
	}
}

// prevState tracks the last known state to avoid duplicate alerts.
type prevState struct {
	resources  map[string]string // resource -> last severity
	containers map[string]string // container name -> last status
}

func newPrevState() *prevState {
	return &prevState{
		resources:  make(map[string]string),
		containers: make(map[string]string),
	}
}

// changed returns true if the severity for a resource changed.
func (p *prevState) changed(resource, severity string) bool {
	prev, exists := p.resources[resource]
	p.resources[resource] = severity
	if !exists {
		return severity != "ok"
	}
	return prev != severity
}

// containerChanged returns true if a container's status changed.
func (p *prevState) containerChanged(name, status string) bool {
	prev, exists := p.containers[name]
	p.containers[name] = status
	if !exists {
		return status != "running"
	}
	return prev != status
}

// Watch starts a blocking loop that checks system resources at the given interval.
// It writes alert events to the returned channel. Cancel the context to stop.
func Watch(ctx context.Context, cfg WatchConfig) <-chan Event {
	ch := make(chan Event, 32)

	go func() {
		defer close(ch)
		prev := newPrevState()

		// check immediately on start
		checkAndEmit(cfg, prev, ch)

		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkAndEmit(cfg, prev, ch)
			}
		}
	}()

	return ch
}

func checkAndEmit(cfg WatchConfig, prev *prevState, ch chan<- Event) {
	now := time.Now()

	// System resource checks
	info, err := system.Status()
	if err == nil {
		checkResource(prev, ch, now, "cpu", info.CPU.UsagePercent, cfg.Alert.CPU, "CPU")
		checkResource(prev, ch, now, "memory", info.Memory.Percent, cfg.Alert.Memory, "Memory")
		for _, d := range info.Disks {
			res := fmt.Sprintf("disk:%s", d.Mount)
			label := fmt.Sprintf("Disk %s", d.Mount)
			checkResource(prev, ch, now, res, d.Percent, cfg.Alert.Disk, label)
		}
	}

	// Docker container checks
	containers, err := docker.List()
	if err == nil {
		// Track which containers still exist to prune stale entries
		seen := make(map[string]bool, len(containers))
		for _, c := range containers {
			status := strings.ToLower(c.State)
			name := c.Name
			seen[name] = true
			if prev.containerChanged(name, status) {
				if status != "running" {
					ch <- Event{
						Time:     now,
						Severity: "critical",
						Resource: fmt.Sprintf("container:%s", name),
						Message:  fmt.Sprintf("Container '%s' is %s", name, status),
					}
				} else {
					ch <- Event{
						Time:     now,
						Severity: "ok",
						Resource: fmt.Sprintf("container:%s", name),
						Message:  fmt.Sprintf("Container '%s' recovered (running)", name),
					}
				}
			}
		}
		// Prune containers that no longer exist
		for name := range prev.containers {
			if !seen[name] {
				delete(prev.containers, name)
			}
		}
	}
}

func checkResource(prev *prevState, ch chan<- Event, now time.Time, resource string, current, threshold float64, label string) {
	severity := statusFor(current, threshold)

	if !prev.changed(resource, severity) {
		return
	}

	switch severity {
	case "warning":
		ch <- Event{
			Time:     now,
			Severity: "warning",
			Resource: resource,
			Message:  fmt.Sprintf("%-10s %5.1f%% (threshold: %.0f%%)", label, current, threshold),
			Current:  current,
		}
	case "critical":
		ch <- Event{
			Time:     now,
			Severity: "critical",
			Resource: resource,
			Message:  fmt.Sprintf("%-10s %5.1f%% (threshold: %.0f%%)", label, current, threshold),
			Current:  current,
		}
	case "ok":
		ch <- Event{
			Time:     now,
			Severity: "ok",
			Resource: resource,
			Message:  fmt.Sprintf("%-10s recovered (%.1f%%)", label, current),
			Current:  current,
		}
	}
}

// FormatEvent returns a human-readable line for a single event.
func FormatEvent(e Event) string {
	icon := "✅"
	switch e.Severity {
	case "warning":
		icon = "⚠️"
	case "critical":
		icon = "🔴"
	}
	return fmt.Sprintf("[%s] %s %s", e.Time.Format("15:04:05"), icon, e.Message)
}

// WatchRules runs the self-healing watch loop using YAML-defined rules.
// Returns a channel of formatted log lines. Cancel the context to stop.
func WatchRules(ctx context.Context, interval time.Duration, rulesCfg *AlertsConfig) <-chan string {
	ch := make(chan string, 32)

	go func() {
		defer close(ch)
		cooldowns := newCooldownTracker()

		// Check immediately
		evaluateRules(rulesCfg, cooldowns, ch)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				evaluateRules(rulesCfg, cooldowns, ch)
			}
		}
	}()

	return ch
}

func evaluateRules(rulesCfg *AlertsConfig, cooldowns *cooldownTracker, ch chan<- string) {
	now := time.Now()
	ts := now.Format("15:04:05")

	// Gather system metrics once
	info, sysErr := system.Status()
	var cpuPct, memPct, diskPct float64
	if sysErr == nil {
		cpuPct = info.CPU.UsagePercent
		memPct = info.Memory.Percent
		if len(info.Disks) > 0 {
			for _, d := range info.Disks {
				if d.Percent > diskPct {
					diskPct = d.Percent
				}
			}
		}
	}

	triggered := false
	for _, rule := range rulesCfg.Rules {
		if cooldowns.InCooldown(rule.Name, rule.CooldownDuration()) {
			continue
		}

		var ruleTriggered bool
		var details string

		switch rule.Metric {
		case "cpu":
			if sysErr != nil {
				continue
			}
			if cpuPct >= rule.Threshold {
				ruleTriggered = true
				details = fmt.Sprintf("cpu %.1f%% >= %.0f%%", cpuPct, rule.Threshold)
			}
		case "memory":
			if sysErr != nil {
				continue
			}
			if memPct >= rule.Threshold {
				ruleTriggered = true
				details = fmt.Sprintf("memory %.1f%% >= %.0f%%", memPct, rule.Threshold)
			}
		case "disk":
			if sysErr != nil {
				continue
			}
			if diskPct >= rule.Threshold {
				ruleTriggered = true
				details = fmt.Sprintf("disk %.1f%% >= %.0f%%", diskPct, rule.Threshold)
			}
		case "container":
			statuses, err := CheckContainers(rule.Watch)
			if err != nil {
				continue
			}
			for _, s := range statuses {
				if !s.Running {
					ruleTriggered = true
					details = fmt.Sprintf("%s is %s", s.Name, s.State)
					break
				}
			}
		}

		if !ruleTriggered {
			continue
		}

		triggered = true
		cooldowns.MarkFired(rule.Name)

		// Log the trigger
		icon := "⚠️"
		if rule.Metric == "container" {
			icon = "🔴"
		}
		ch <- fmt.Sprintf("  ⏱️  %s  %s  %s triggered (%s)", ts, icon, rule.Name, details)

		// Execute action
		result := ExecuteAction(rule)
		resultStatus := "success"
		if !result.Success {
			resultStatus = "failed"
		}

		if rule.Action != "notify" {
			ch <- fmt.Sprintf("                 → Executing: %s", actionDescription(rule))
			if result.Success {
				ch <- fmt.Sprintf("                 → ✅ %s", result.Output)
			} else {
				ch <- fmt.Sprintf("                 → ❌ %s", result.Output)
			}
		}

		// Send webhook
		if rule.Notify == "webhook" && rulesCfg.Webhook.URL != "" {
			payload := WebhookPayload{
				Rule:         rule.Name,
				Status:       "triggered",
				Details:      details,
				ActionTaken:  rule.Action,
				ActionResult: resultStatus,
				Timestamp:    now.Format(time.RFC3339),
			}
			if err := SendWebhook(rulesCfg.Webhook.URL, payload); err != nil {
				ch <- fmt.Sprintf("                 → webhook error: %s", err)
			}
		}

		// Record history
		entry := HistoryEntry{
			Timestamp:    now,
			Rule:         rule.Name,
			Metric:       rule.Metric,
			Details:      details,
			ActionTaken:  rule.Action,
			ActionResult: resultStatus,
		}
		_ = RecordHistory(entry)
	}

	if !triggered {
		ch <- fmt.Sprintf("  ⏱️  %s  ✅ All clear (cpu %.0f%%, mem %.0f%%, disk %.0f%%)",
			ts, cpuPct, memPct, diskPct)
	}
}

func actionDescription(rule Rule) string {
	switch rule.Action {
	case "restart":
		return "docker restart " + strings.Join(rule.Watch, ", ")
	case "exec":
		return rule.Exec
	default:
		return rule.Action
	}
}
