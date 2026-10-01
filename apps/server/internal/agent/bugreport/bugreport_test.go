/*
===========================================================================

bugreport_test.go - operator configuration

===========================================================================
*/
package bugreport

import (
	"strings"
	"testing"
)

const testWebhook = "https://discord.com/api/webhooks/1234567890123456789/" +
	"abcdefghijklmnopqrstuvwxyz0123456789_-ABCDEFGHIJKLMNOPQRSTUVWXYZ"

/*
================
envMap
================
*/
func envMap(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

/*
================
TestLoadConfigIsOffWithoutWebhook
================
*/
func TestLoadConfigIsOffWithoutWebhook(t *testing.T) {
	config, warnings := LoadConfig(envMap(nil))
	if config.WebhookURL != "" || len(warnings) != 0 {
		t.Fatalf("unset variables must disable quietly: %+v %v", config, warnings)
	}
	if !config.ReplayDefault || config.MaxBytes != DefaultMaxBytes {
		t.Fatalf("defaults: %+v", config)
	}
}

/*
================
TestLoadConfigReadsAllVariables
================
*/
func TestLoadConfigReadsAllVariables(t *testing.T) {
	config, warnings := LoadConfig(envMap(map[string]string{
		EnvDiscordWebhook: " " + testWebhook + " ",
		EnvReplayDefault:  "OFF",
		EnvMaxBytes:       "52428800",
	}))
	if len(warnings) != 0 {
		t.Fatal(warnings)
	}
	if config.WebhookURL != testWebhook || config.ReplayDefault || config.MaxBytes != 50<<20 {
		t.Fatalf("%+v", config)
	}
}

/*
================
TestLoadConfigRejectsBadValuesWithoutLeakingWebhook
================
*/
func TestLoadConfigRejectsBadValuesWithoutLeakingWebhook(t *testing.T) {
	bad := "https://evil.example/api/webhooks/1234567890123456789/" + strings.Repeat("s", 40)
	config, warnings := LoadConfig(envMap(map[string]string{
		EnvDiscordWebhook: bad,
		EnvReplayDefault:  "maybe",
		EnvMaxBytes:       "12",
	}))
	if config.WebhookURL != "" || !config.ReplayDefault || config.MaxBytes != DefaultMaxBytes {
		t.Fatalf("bad values must fall back: %+v", config)
	}
	if len(warnings) != 3 {
		t.Fatalf("want one warning per variable, got %v", warnings)
	}
	for _, warning := range warnings {
		if strings.Contains(warning, strings.Repeat("s", 40)) {
			t.Fatal("warning leaks the webhook token")
		}
	}
}

/*
================
TestValidWebhookURL
================
*/
func TestValidWebhookURL(t *testing.T) {
	token := strings.Repeat("a", 68)
	for raw, want := range map[string]bool{
		"https://discord.com/api/webhooks/1234567890123456789/" + token:            true,
		"https://discordapp.com/api/webhooks/1234567890123456789/" + token:         true,
		"https://canary.discord.com/api/v10/webhooks/1234567890123456789/" + token: true,
		"http://discord.com/api/webhooks/1234567890123456789/" + token:             false,
		"https://discord.com.evil.io/api/webhooks/1234567890123456789/" + token:    false,
		"https://discord.com/api/webhooks/1234567890123456789/" + token + "?x=1":   false,
		"https://user@discord.com/api/webhooks/1234567890123456789/" + token:       false,
		"https://discord.com/api/webhooks/notanid/" + token:                        false,
	} {
		if got := ValidWebhookURL(raw); got != want {
			t.Errorf("ValidWebhookURL(%q) = %t, want %t", raw, got, want)
		}
	}
}
