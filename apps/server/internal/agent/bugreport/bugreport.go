/*
===========================================================================

bugreport.go - in-game bug reports: operator configuration

Package bugreport accepts the bug reports players send from the browser
client (a description, client context and an optional replay clip or
screenshot) and delivers them to a Discord channel through a webhook, to a
local directory, or to both.

The feature is off unless the operator configures a sink. Four environment
variables control it; a missing or invalid sink leaves it disabled rather
than failing the Agent, because bug reporting is never worth an outage. The webhook URL is a credential (anyone holding it can post to the
channel): it is never logged and never sent to clients.

===========================================================================
*/
package bugreport

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	// DestinationDiscord and DestinationDirectory are the Settings
	// destination names.
	DestinationDiscord   = "discord"
	DestinationDirectory = "directory"

	// EnvDiscordWebhook turns the feature on and names the receiving channel.
	EnvDiscordWebhook = "SRO_BUG_REPORT_DISCORD_WEBHOOK"
	// EnvReplayDefault is the replay recording state ("on" or "off") for
	// players who have not changed it in the Options window.
	EnvReplayDefault = "SRO_BUG_REPORT_REPLAY_DEFAULT"
	// EnvMaxBytes caps one report's attachment. Discord accepts 10 MiB per
	// file on servers without boosts and more on boosted ones.
	EnvMaxBytes = "SRO_BUG_REPORT_MAX_BYTES"
	// EnvDirectory is an absolute directory that receives every report
	// (directory.go): the sink for a deployment without a Discord channel.
	EnvDirectory = "SRO_BUG_REPORT_DIRECTORY"

	DefaultMaxBytes int64 = 10 << 20
	minMaxBytes     int64 = 1 << 20
	maxMaxBytes     int64 = 500 << 20

	// ReplaySeconds is the length of the rolling replay the client keeps.
	ReplaySeconds = 60
)

// Official Discord hosts only: the Agent must not become a relay that posts
// player uploads to an arbitrary URL through a mistyped variable.
var webhookPattern = regexp.MustCompile(
	`^https://(?:(?:canary|ptb)\.)?discord(?:app)?\.com/api(?:/v\d+)?/webhooks/\d{15,22}/[A-Za-z0-9_-]{20,128}$`,
)

/*
================
Config

Operator settings. With neither a WebhookURL nor a Directory the feature is
disabled.
================
*/
type Config struct {
	WebhookURL    string
	Directory     string
	ReplayDefault bool
	MaxBytes      int64
	// Off is the deployer's explicit "off": remove a stored webhook. A
	// deploy that names no webhook keeps the stored one (sro-nomad).
	Off bool
}

/*
================
Settings

What the client needs to know; never includes the webhook.
================
*/
type Settings struct {
	Enabled       bool  `json:"enabled"`
	ReplayDefault bool  `json:"replayDefault"`
	MaxBytes      int64 `json:"maxBytes"`
	ReplaySeconds int   `json:"replaySeconds"`
	// MaxDiagnosticsBytes advertises that this Agent accepts the optional
	// diagnostics part, and its bound. An older Agent omits it (zero), and
	// refuses the part as unknown, so a client attaches the archive only when
	// this is positive.
	MaxDiagnosticsBytes int64 `json:"maxDiagnosticsBytes"`
	// Destinations names where an accepted report goes ("discord",
	// "directory"), so the dialog tells the player the truth about it.
	// Empty when reports are disabled.
	Destinations []string `json:"destinations"`
}

/*
================
Config.Enabled
================
*/
func (config Config) Enabled() bool {
	return config.WebhookURL != "" || config.Directory != ""
}

/*
================
DisabledSettings
================
*/
func DisabledSettings() Settings {
	return Settings{
		Enabled:       false,
		ReplayDefault: false,
		MaxBytes:      0,
		ReplaySeconds: ReplaySeconds,
		Destinations:  []string{},
	}
}

/*
================
LoadConfig

Reads the four variables through getenv (os.Getenv in service). Every
problem becomes a warning and a safe value: an invalid webhook disables the
feature, an invalid default or size falls back to the documented default.
The warnings never contain the webhook itself.
================
*/
func LoadConfig(getenv func(string) string) (Config, []string) {
	var warnings []string
	config := Config{ReplayDefault: true, MaxBytes: DefaultMaxBytes}

	webhook := strings.TrimSpace(getenv(EnvDiscordWebhook))
	if strings.EqualFold(webhook, "off") {
		config.Off = true
	} else if webhook != "" {
		if ValidWebhookURL(webhook) {
			config.WebhookURL = webhook
		} else {
			warnings = append(warnings, fmt.Sprintf(
				"%s is not a Discord webhook URL (https://discord.com/api/webhooks/<id>/<token>); bug reports are disabled",
				EnvDiscordWebhook,
			))
		}
	}

	if raw := strings.TrimSpace(getenv(EnvDirectory)); raw != "" {
		// A relative path would follow the Agent's working directory, which
		// Nomad allocates per run: reports would vanish with the allocation.
		if filepath.IsAbs(raw) {
			config.Directory = filepath.Clean(raw)
		} else {
			warnings = append(warnings, fmt.Sprintf("%s=%q is not an absolute path; ignored", EnvDirectory, raw))
		}
	}

	if raw := strings.TrimSpace(getenv(EnvReplayDefault)); raw != "" {
		value, ok := parseSwitch(raw)
		if ok {
			config.ReplayDefault = value
		} else {
			warnings = append(warnings, fmt.Sprintf("%s=%q is not on/off; using on", EnvReplayDefault, raw))
		}
	}

	if raw := strings.TrimSpace(getenv(EnvMaxBytes)); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err == nil && value >= minMaxBytes && value <= maxMaxBytes {
			config.MaxBytes = value
		} else {
			warnings = append(warnings, fmt.Sprintf(
				"%s=%q must be a byte count between %d and %d; using %d",
				EnvMaxBytes, raw, minMaxBytes, maxMaxBytes, DefaultMaxBytes,
			))
		}
	}
	return config, warnings
}

/*
================
ValidWebhookURL
================
*/
func ValidWebhookURL(raw string) bool {
	if !webhookPattern.MatchString(raw) {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

/*
================
parseSwitch
================
*/
func parseSwitch(raw string) (bool, bool) {
	switch strings.ToLower(raw) {
	case "on", "true", "1", "yes":
		return true, true
	case "off", "false", "0", "no":
		return false, true
	}
	return false, false
}
