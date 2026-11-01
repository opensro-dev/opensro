/*
===========================================================================

wiring_certwatch.go - the served WebTransport pair's staff alert (#1447)

Ops only, port-only and not native: nothing here reaches a player. The
transport's CertificateWatch checks the pair this run serves at boot and
every CertificateCheckInterval. A problem is always logged at error. It is
also posted to the staff Discord channel when the deployment rendered the
staff webhook into the task's secrets (SRO_STAFF_WEBHOOK_PATH). That file
is read at every post, so a rotated webhook needs no restart; an absent,
empty or invalid one leaves the log as the only signal.

===========================================================================
*/
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"opensro.online/server/internal/agent/bugreport"
	"opensro.online/server/internal/transport"
)

const (
	// EnvStaffWebhookPath names the secrets file the job template renders
	// from the GameWorld variable's staff_discord_webhook item.
	EnvStaffWebhookPath = "SRO_STAFF_WEBHOOK_PATH"
	staffPostTimeout    = 10 * time.Second
	// Discord's own message ceiling; the alert texts are far below it.
	staffMessageLimit = 2000
)

var errNoStaffWebhook = errors.New("no staff webhook configured")

/*
================
installCertificateWatch

The application runs the watch from Run, on a CertificateCheckInterval
ticker. It is installed in every posture; outside the public one it
finds nothing to report.
================
*/
func installCertificateWatch(application *gameWorldApplication, ts *transport.Server, shardID string) {
	poster := staffWebhookPoster(os.Getenv(EnvStaffWebhookPath), &http.Client{Timeout: staffPostTimeout})
	application.certificateWatch = transport.NewCertificateWatch(ts, shardID, time.Now, poster)
}

/*
================
staffWebhookPoster

Posts one plain message to the webhook in the file at path, read fresh at
every post. Mentions are disabled: the text quotes a load error, and an
error string must never ping anyone.
================
*/
func staffWebhookPoster(path string, client *http.Client) func(string) error {
	return func(text string) error {
		if path == "" {
			return errNoStaffWebhook
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("staff webhook: %w", err)
		}
		webhook := strings.TrimSpace(string(raw))
		if webhook == "" {
			return errNoStaffWebhook
		}
		if !bugreport.ValidWebhookURL(webhook) {
			return errors.New("staff webhook is not a Discord webhook URL")
		}
		if len(text) > staffMessageLimit {
			text = text[:staffMessageLimit]
		}
		body, err := json.Marshal(map[string]any{
			"content":          text,
			"allowed_mentions": map[string]any{"parse": []string{}},
		})
		if err != nil {
			return err
		}
		response, err := client.Post(webhook, "application/json", bytes.NewReader(body))
		if err != nil {
			// The URL carries the webhook token: never let it reach a log.
			return errors.New("staff webhook: request failed")
		}
		defer response.Body.Close()
		if response.StatusCode/100 != 2 {
			return fmt.Errorf("staff webhook: HTTP %d", response.StatusCode)
		}
		return nil
	}
}
