package bot_test

import (
	"errors"
	"testing"

	"opensro.online/server/internal/bot"
)

func TestRequireEnabledRefusesWhenSwitchIsOff(t *testing.T) {
	t.Setenv(bot.EnvBotPlane, "")
	if bot.Enabled() {
		t.Fatal("the bot switch is on by default")
	}
	if err := bot.RequireEnabled(); !errors.Is(err, bot.ErrDisabled) {
		t.Fatalf("RequireEnabled off = %v, want ErrDisabled", err)
	}
}

func TestRequireEnabledAcceptsOnlyOne(t *testing.T) {
	for _, value := range []string{"", "0", "true", "on", "yes"} {
		t.Setenv(bot.EnvBotPlane, value)
		if bot.Enabled() {
			t.Fatalf("the bot switch turned on for %q", value)
		}
	}
	t.Setenv(bot.EnvBotPlane, "1")
	if !bot.Enabled() {
		t.Fatal("the bot switch did not turn on for \"1\"")
	}
	if err := bot.RequireEnabled(); err != nil {
		t.Fatalf("RequireEnabled on = %v, want nil", err)
	}
}
