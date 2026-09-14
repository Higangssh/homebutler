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
type provider struct {
	channel Channel
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
		present: func(c *ProviderConfig) bool { return c.Telegram != nil },
		enabled: func(c *ProviderConfig) bool {
			return c.Telegram != nil && c.Telegram.BotToken != "" && c.Telegram.ChatID != ""
		},
		send: func(c *ProviderConfig, e Event) error { return sendTelegram(c.Telegram, e) },
		pick: func(dst, src *ProviderConfig) { dst.Telegram = src.Telegram },
	},
	{
		channel: ChannelSlack,
		present: func(c *ProviderConfig) bool { return c.Slack != nil },
		enabled: func(c *ProviderConfig) bool { return c.Slack != nil && c.Slack.WebhookURL != "" },
		send:    func(c *ProviderConfig, e Event) error { return sendSlack(c.Slack, e) },
		pick:    func(dst, src *ProviderConfig) { dst.Slack = src.Slack },
	},
	{
		channel: ChannelDiscord,
		present: func(c *ProviderConfig) bool { return c.Discord != nil },
		enabled: func(c *ProviderConfig) bool { return c.Discord != nil && c.Discord.WebhookURL != "" },
		send:    func(c *ProviderConfig, e Event) error { return sendDiscord(c.Discord, e) },
		pick:    func(dst, src *ProviderConfig) { dst.Discord = src.Discord },
	},
	{
		channel: ChannelWebhook,
		present: func(c *ProviderConfig) bool { return c.Webhook != nil },
		enabled: func(c *ProviderConfig) bool { return c.Webhook != nil && c.Webhook.URL != "" },
		send:    func(c *ProviderConfig, e Event) error { return sendWebhook(c.Webhook, e) },
		pick:    func(dst, src *ProviderConfig) { dst.Webhook = src.Webhook },
	},
	{
		channel: ChannelNtfy,
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
