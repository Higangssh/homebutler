package notify

type Channel string

const (
	ChannelTelegram Channel = "telegram"
	ChannelSlack    Channel = "slack"
	ChannelDiscord  Channel = "discord"
	ChannelWebhook  Channel = "webhook"
	ChannelNtfy     Channel = "ntfy"
	ChannelGotify   Channel = "gotify"
)

// The credential fields below carry json:"-" so they cannot be serialized at
// all, and secret:"true" so config.hasSecrets finds them without having to be
// told about each new section. Nothing serializes a *config.Config today, but
// that is a property of the current call sites rather than of these types, and
// report, doctor and the MCP tools all serialize a growing amount of state.

type TelegramConfig struct {
	BotToken string `yaml:"bot_token" json:"-" secret:"true"`
	ChatID   string `yaml:"chat_id" json:"chat_id"`
}

type SlackConfig struct {
	WebhookURL string `yaml:"webhook_url" json:"-" secret:"true"`
}

type DiscordConfig struct {
	WebhookURL string `yaml:"webhook_url" json:"-" secret:"true"`
}

type WebhookConfig struct {
	URL string `yaml:"url" json:"-" secret:"true"`
}

// NtfyConfig reaches an ntfy server. The token is optional: a public topic
// needs none, and a protected one takes an access token. It goes in a header
// rather than the URL so it cannot end up quoted in an error (#176).
type NtfyConfig struct {
	URL string `yaml:"url" json:"url"`
	// Topic is a credential on a public server. ntfy.sh has no accounts in the
	// default arrangement: anyone who knows the topic can subscribe to it and
	// read every event homebutler sends, which is why ntfy's own documentation
	// asks for a name nobody can guess. It is tagged like one so the config
	// permission check covers a file that has only this, and so the dashboard
	// cannot serialize it when #154 gives it settings to show.
	Topic string `yaml:"topic" json:"-" secret:"true"`
	Token string `yaml:"token,omitempty" json:"-" secret:"true"`
}

// GotifyConfig reaches a Gotify server with an application token, which is
// required — Gotify has no unauthenticated publish.
type GotifyConfig struct {
	URL   string `yaml:"url" json:"url"`
	Token string `yaml:"token" json:"-" secret:"true"`
}

type ProviderConfig struct {
	Telegram *TelegramConfig `yaml:"telegram,omitempty" json:"telegram,omitempty"`
	Slack    *SlackConfig    `yaml:"slack,omitempty" json:"slack,omitempty"`
	Discord  *DiscordConfig  `yaml:"discord,omitempty" json:"discord,omitempty"`
	Webhook  *WebhookConfig  `yaml:"webhook,omitempty" json:"webhook,omitempty"`
	Ntfy     *NtfyConfig     `yaml:"ntfy,omitempty" json:"ntfy,omitempty"`
	Gotify   *GotifyConfig   `yaml:"gotify,omitempty" json:"gotify,omitempty"`
}

// provider is everything the rest of the package needs to know about one
// channel. The lists this replaces were written out by hand in six places, so
// adding a channel meant remembering all six and the next provider was going
// to be forgotten in one of them (#177). A channel is added here and nowhere
// else.
// Field is one config key a channel takes. Secret marks the ones that are
// credentials, which the dashboard may write and may never read back.
type Field struct {
	Name   string
	Secret bool
	// Optional marks a key the channel sends without. A form that asks for
	// every key as though all were required sends people looking for a token
	// their server does not use.
	Optional bool
}

type provider struct {
	channel Channel
	// fields are the config keys this channel takes. A channel is described
	// once, here, and the config writer and the dashboard both read it rather
	// than each carrying their own idea of what a channel looks like.
	fields []Field
	// present reports whether the block exists at all, however incomplete.
	present func(*ProviderConfig) bool
	// enabled reports whether it has what it needs to send.
	enabled func(*ProviderConfig) bool
	// send delivers one event.
	send func(*ProviderConfig, Event) error
	// pick copies this channel's block from src to dst, for the filtering a
	// per-event channel list asks for.
	pick func(dst, src *ProviderConfig)
}

var providers = []provider{
	{
		channel: ChannelTelegram,
		fields:  []Field{{Name: "bot_token", Secret: true}, {Name: "chat_id"}},
		present: func(c *ProviderConfig) bool { return c.Telegram != nil },
		enabled: func(c *ProviderConfig) bool {
			return c.Telegram != nil && c.Telegram.BotToken != "" && c.Telegram.ChatID != ""
		},
		send: func(c *ProviderConfig, e Event) error { return sendTelegram(c.Telegram, e) },
		pick: func(dst, src *ProviderConfig) { dst.Telegram = src.Telegram },
	},
	{
		channel: ChannelSlack,
		fields:  []Field{{Name: "webhook_url", Secret: true}},
		present: func(c *ProviderConfig) bool { return c.Slack != nil },
		enabled: func(c *ProviderConfig) bool { return c.Slack != nil && c.Slack.WebhookURL != "" },
		send:    func(c *ProviderConfig, e Event) error { return sendSlack(c.Slack, e) },
		pick:    func(dst, src *ProviderConfig) { dst.Slack = src.Slack },
	},
	{
		channel: ChannelDiscord,
		fields:  []Field{{Name: "webhook_url", Secret: true}},
		present: func(c *ProviderConfig) bool { return c.Discord != nil },
		enabled: func(c *ProviderConfig) bool { return c.Discord != nil && c.Discord.WebhookURL != "" },
		send:    func(c *ProviderConfig, e Event) error { return sendDiscord(c.Discord, e) },
		pick:    func(dst, src *ProviderConfig) { dst.Discord = src.Discord },
	},
	{
		channel: ChannelWebhook,
		fields:  []Field{{Name: "url", Secret: true}},
		present: func(c *ProviderConfig) bool { return c.Webhook != nil },
		enabled: func(c *ProviderConfig) bool { return c.Webhook != nil && c.Webhook.URL != "" },
		send:    func(c *ProviderConfig, e Event) error { return sendWebhook(c.Webhook, e) },
		pick:    func(dst, src *ProviderConfig) { dst.Webhook = src.Webhook },
	},
	{
		channel: ChannelNtfy,
		fields:  []Field{{Name: "url"}, {Name: "topic", Secret: true}, {Name: "token", Secret: true, Optional: true}},
		present: func(c *ProviderConfig) bool { return c.Ntfy != nil },
		// A topic without a server, or a server without a topic, addresses
		// nothing. The token is optional.
		enabled: func(c *ProviderConfig) bool {
			return c.Ntfy != nil && c.Ntfy.URL != "" && c.Ntfy.Topic != ""
		},
		send: func(c *ProviderConfig, e Event) error { return sendNtfy(c.Ntfy, e) },
		pick: func(dst, src *ProviderConfig) { dst.Ntfy = src.Ntfy },
	},
	{
		channel: ChannelGotify,
		fields:  []Field{{Name: "url"}, {Name: "token", Secret: true}},
		present: func(c *ProviderConfig) bool { return c.Gotify != nil },
		enabled: func(c *ProviderConfig) bool {
			return c.Gotify != nil && c.Gotify.URL != "" && c.Gotify.Token != ""
		},
		send: func(c *ProviderConfig, e Event) error { return sendGotify(c.Gotify, e) },
		pick: func(dst, src *ProviderConfig) { dst.Gotify = src.Gotify },
	},
}

// Channels lists every channel homebutler can speak to, configured or not.
func Channels() []Channel {
	out := make([]Channel, 0, len(providers))
	for _, p := range providers {
		out = append(out, p.channel)
	}
	return out
}

// EnabledChannels lists the channels this config can actually send through.
func (pc *ProviderConfig) EnabledChannels() []Channel {
	if pc == nil {
		return nil
	}
	channels := make([]Channel, 0, len(providers))
	for _, p := range providers {
		if p.enabled(pc) {
			channels = append(channels, p.channel)
		}
	}
	return channels
}

// PresentChannels lists the channels with a block in the config, complete or
// not. A block that is present and not enabled is the shape config validate
// warns about.
func (pc *ProviderConfig) PresentChannels() []Channel {
	if pc == nil {
		return nil
	}
	channels := make([]Channel, 0, len(providers))
	for _, p := range providers {
		if p.present(pc) {
			channels = append(channels, p.channel)
		}
	}
	return channels
}

func (pc *ProviderConfig) IsEmpty() bool {
	return len(pc.PresentChannels()) == 0
}

// FieldsFor returns the config keys a channel takes, or false when the name is
// not a channel homebutler has.
func FieldsFor(channel Channel) ([]Field, bool) {
	for _, p := range providers {
		if p.channel == channel {
			return p.fields, true
		}
	}
	return nil, false
}

// Setting reads one field of one channel by name, so a caller that already
// knows the field list from FieldsFor does not need a switch of its own.
// The second return is false when the channel has no such field.
func (pc *ProviderConfig) Setting(channel Channel, field string) (string, bool) {
	if pc == nil {
		return "", false
	}
	switch channel {
	case ChannelTelegram:
		if pc.Telegram == nil {
			return "", false
		}
		switch field {
		case "bot_token":
			return pc.Telegram.BotToken, true
		case "chat_id":
			return pc.Telegram.ChatID, true
		}
	case ChannelSlack:
		if pc.Slack == nil {
			return "", false
		}
		if field == "webhook_url" {
			return pc.Slack.WebhookURL, true
		}
	case ChannelDiscord:
		if pc.Discord == nil {
			return "", false
		}
		if field == "webhook_url" {
			return pc.Discord.WebhookURL, true
		}
	case ChannelWebhook:
		if pc.Webhook == nil {
			return "", false
		}
		if field == "url" {
			return pc.Webhook.URL, true
		}
	case ChannelNtfy:
		if pc.Ntfy == nil {
			return "", false
		}
		switch field {
		case "url":
			return pc.Ntfy.URL, true
		case "topic":
			return pc.Ntfy.Topic, true
		case "token":
			return pc.Ntfy.Token, true
		}
	case ChannelGotify:
		if pc.Gotify == nil {
			return "", false
		}
		switch field {
		case "url":
			return pc.Gotify.URL, true
		case "token":
			return pc.Gotify.Token, true
		}
	}
	return "", false
}
