package watch

import (
	"fmt"
	"strings"
	"time"

	"github.com/Higangssh/homebutler/internal/notify"
)

type WatchNotifier struct {
	Settings   NotifySettings
	Dispatcher *notify.Dispatcher
}

func NewWatchNotifier(settings NotifySettings, providers *notify.ProviderConfig) *WatchNotifier {
	cooldown, err := time.ParseDuration(settings.Cooldown)
	if err != nil {
		cooldown = 5 * time.Minute
	}

	return &WatchNotifier{
		Settings:   settings,
		Dispatcher: notify.NewDispatcher(providers, cooldown),
	}
}

func (wn *WatchNotifier) NotifyIncident(inc Incident, flap FlappingResult, crash *CrashSummary, now time.Time) error {
	if wn == nil || !wn.Settings.Enabled || wn.Dispatcher == nil {
		return nil
	}

	shouldNotify := (flap.IsFlapping && wn.Settings.OnFlapping) ||
		(!flap.IsFlapping && wn.Settings.OnIncident)
	if !shouldNotify {
		return nil
	}

	status := "restart"
	kind := "watch.incident"
	if flap.IsFlapping {
		status = "flapping"
		kind = "watch.flapping"
	}

	var details []string
	if crash != nil {
		details = append(details, fmt.Sprintf("category=%s reason=%s", crash.Category, crash.Reason))
	}

	event := notify.Event{
		Kind:        kind,
		Source:      "watch",
		Name:        inc.Container,
		Status:      status,
		Details:     strings.Join(details, "; "),
		Time:        inc.DetectedAt,
		Fingerprint: kind + ":" + inc.Container,
	}

	errs := wn.Dispatcher.Send(event.Fingerprint, event, now)
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
