package notify

import (
	"sync"
	"time"
)

type Dispatcher struct {
	Providers *ProviderConfig
	Cooldown  time.Duration
	cooldowns map[string]time.Time
	mu        sync.Mutex
	sendFunc  func(*ProviderConfig, Event) []error
}

func (d *Dispatcher) SetSendFunc(fn func(*ProviderConfig, Event) []error) {
	if fn != nil {
		d.sendFunc = fn
	}
}

func NewDispatcher(providers *ProviderConfig, cooldown time.Duration) *Dispatcher {
	return &Dispatcher{
		Providers: providers,
		Cooldown:  cooldown,
		cooldowns: make(map[string]time.Time),
		sendFunc:  SendAll,
	}
}

func (d *Dispatcher) Send(key string, event Event, now time.Time) []error {
	providers := d.resolveProviders(event)
	if providers == nil || providers.IsEmpty() {
		return nil
	}

	if key == "" {
		key = event.Fingerprint
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if key != "" && d.Cooldown > 0 {
		if last, ok := d.cooldowns[key]; ok && now.Before(last.Add(d.Cooldown)) {
			return nil
		}
	}

	errs := d.sendFunc(providers, event)
	if key != "" {
		d.cooldowns[key] = now
	}
	return errs
}

func (d *Dispatcher) SendImmediate(event Event) []error {
	providers := d.resolveProviders(event)
	if providers == nil || providers.IsEmpty() {
		return nil
	}
	return d.sendFunc(providers, event)
}

func (d *Dispatcher) resolveProviders(event Event) *ProviderConfig {
	if d.Providers == nil {
		return nil
	}
	if len(event.Channels) == 0 {
		return d.Providers
	}

	filtered := &ProviderConfig{}
	for _, ch := range event.Channels {
		switch ch {
		case ChannelTelegram:
			filtered.Telegram = d.Providers.Telegram
		case ChannelSlack:
			filtered.Slack = d.Providers.Slack
		case ChannelDiscord:
			filtered.Discord = d.Providers.Discord
		case ChannelWebhook:
			filtered.Webhook = d.Providers.Webhook
		}
	}
	if filtered.IsEmpty() {
		return nil
	}
	return filtered
}
