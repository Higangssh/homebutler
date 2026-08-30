package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/alerts"
	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/notify"
)

// watch start resolves notification providers once and uses them for restart
// incidents and threshold events alike. Before #97 the threshold loop lived in
// another process with its own path to the same providers.
func TestWatchStartResolvesProvidersFromConfig(t *testing.T) {
	saved := cfg
	t.Cleanup(func() { cfg = saved })

	cfg = &config.Config{Notify: notify.ProviderConfig{
		Telegram: &notify.TelegramConfig{BotToken: "t", ChatID: "1"},
	}}
	providers := &alerts.NotifyConfig{}
	if cfg != nil {
		providers = &cfg.Notify
	}
	if providers.IsEmpty() {
		t.Fatal("providers resolved from config.yaml should not be empty")
	}
}

// The alerts command should no longer send readers away to watch as though the
// two were alternatives; they overlap, and the help has to say which is which.
func TestAlertsHelpDescribesTheOverlapWithWatch(t *testing.T) {
	long := newAlertsCmd().Long
	if strings.Contains(long, "For most users, start with") {
		t.Error("alerts still redirects users to watch as if they were alternatives")
	}
	for _, want := range []string{"watch start", "one notifier", "resources only"} {
		if !strings.Contains(long, want) {
			t.Errorf("alerts help does not mention %q:\n%s", want, long)
		}
	}
}

var osReadFile = os.ReadFile

func readSource(t *testing.T, name string) string {
	t.Helper()
	data, err := osReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(data)
}
