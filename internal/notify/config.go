package notify

type Channel string

const (
	ChannelTelegram Channel = "telegram"
	ChannelSlack    Channel = "slack"
	ChannelDiscord  Channel = "discord"
	ChannelWebhook  Channel = "webhook"
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

type ProviderConfig struct {
	Telegram *TelegramConfig `yaml:"telegram,omitempty" json:"telegram,omitempty"`
	Slack    *SlackConfig    `yaml:"slack,omitempty" json:"slack,omitempty"`
	Discord  *DiscordConfig  `yaml:"discord,omitempty" json:"discord,omitempty"`
	Webhook  *WebhookConfig  `yaml:"webhook,omitempty" json:"webhook,omitempty"`
}

func (pc *ProviderConfig) EnabledChannels() []Channel {
	if pc == nil {
		return nil
	}

	channels := make([]Channel, 0, 4)
	if pc.Telegram != nil && pc.Telegram.BotToken != "" && pc.Telegram.ChatID != "" {
		channels = append(channels, ChannelTelegram)
	}
	if pc.Slack != nil && pc.Slack.WebhookURL != "" {
		channels = append(channels, ChannelSlack)
	}
	if pc.Discord != nil && pc.Discord.WebhookURL != "" {
		channels = append(channels, ChannelDiscord)
	}
	if pc.Webhook != nil && pc.Webhook.URL != "" {
		channels = append(channels, ChannelWebhook)
	}
	return channels
}

func (pc *ProviderConfig) IsEmpty() bool {
	return pc == nil || (pc.Telegram == nil && pc.Slack == nil && pc.Discord == nil && pc.Webhook == nil)
}
