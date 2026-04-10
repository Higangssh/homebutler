package watch

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Higangssh/homebutler/internal/alerts"
)

type WatchNotifier struct {
	Settings   NotifySettings
	AlertsCfg  *alerts.NotifyConfig
	cooldowns  map[string]time.Time
	mu         sync.Mutex
	notifyFunc func(*alerts.NotifyConfig, alerts.NotifyEvent) []error
}

func NewWatchNotifier(settings NotifySettings, alertsCfg *alerts.NotifyConfig) *WatchNotifier {
	return &WatchNotifier{
		Settings:   settings,
		AlertsCfg:  alertsCfg,
		cooldowns:  make(map[string]time.Time),
		notifyFunc: alerts.NotifyAll,
	}
}

func (wn *WatchNotifier) NotifyIncident(inc Incident, flap FlappingResult, crash *CrashSummary, now time.Time) error {
	if !wn.Settings.Enabled {
		return nil
	}

	shouldNotify := false
	if flap.IsFlapping && wn.Settings.OnFlapping {
		shouldNotify = true
	}
	if !flap.IsFlapping && wn.Settings.OnIncident {
		shouldNotify = true
	}
	if !shouldNotify {
		return nil
	}

	cooldown, err := time.ParseDuration(wn.Settings.Cooldown)
	if err != nil {
		cooldown = 5 * time.Minute
	}

	wn.mu.Lock()
	if last, ok := wn.cooldowns[inc.Container]; ok && now.Before(last.Add(cooldown)) {
		wn.mu.Unlock()
		return nil
	}
	wn.cooldowns[inc.Container] = now
	wn.mu.Unlock()

	if wn.AlertsCfg == nil {
		return nil
	}

	status := "restart"
	if flap.IsFlapping {
		status = "flapping"
	}

	var details []string
	if crash != nil {
		details = append(details, fmt.Sprintf("category=%s reason=%s", crash.Category, crash.Reason))
	}

	event := alerts.NotifyEvent{
		RuleName: "watch:" + inc.Container,
		Status:   status,
		Details:  strings.Join(details, "; "),
		Time:     inc.DetectedAt.Format(time.RFC3339),
	}

	fn := wn.notifyFunc
	if fn == nil {
		fn = alerts.NotifyAll
	}
	errs := fn(wn.AlertsCfg, event)
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
