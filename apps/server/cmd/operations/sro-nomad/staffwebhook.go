/*
===========================================================================

staffwebhook.go - the GameWorld's staff webhook item, end to end (#1447)

Ops only. The staff Discord channel the GameWorld pages about the
WebTransport pair it serves (transport certwatch) reaches it as one Nomad
variable item: the deployer's SRO_STAFF_DISCORD_WEBHOOK is read here
(configuredStaffWebhook), kept or removed against the stored item
(keepStaffWebhook), written by desiredVariables, and rendered by the job
template into secrets/staff-webhook with change_mode noop. Changing it
alone never needs the GameWorld stopped (planVariables, onlyItemDiffers).

===========================================================================
*/
package main

import (
	"strings"

	nomad "github.com/hashicorp/nomad/api"
	"opensro.online/server/internal/agent/bugreport"
)

const (
	// staffWebhookItem is the GameWorld variable item the job template
	// renders into secrets/staff-webhook.
	staffWebhookItem = "staff_discord_webhook"
	envStaffWebhook  = "SRO_STAFF_DISCORD_WEBHOOK"
)

/*
================
configuredStaffWebhook

The deployer's SRO_STAFF_DISCORD_WEBHOOK: empty keeps the stored item,
"off" removes it, a Discord webhook URL replaces it. Anything else is
warned about and treated as unset, keeping the stored one: an ops alert
channel must never be the reason a release does not ship. The warning
never quotes the value, which carries the webhook token.
================
*/
func configuredStaffWebhook(raw string) (webhook string, off bool, warning string) {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "":
		return "", false, ""
	case strings.EqualFold(raw, "off"):
		return "", true, ""
	case bugreport.ValidWebhookURL(raw):
		return raw, false, ""
	}
	return "", false, envStaffWebhook + " is not a Discord webhook URL " +
		"(https://discord.com/api/webhooks/<id>/<token>) or off; the stored staff webhook is kept"
}

/*
================
keepStaffWebhook

The GameWorld's staff webhook, kept the way keepBugReportWebhook keeps the
Agent's: a deploy that names none keeps the stored one, "off" removes it.
Its template is change_mode noop and the GameWorld reads the file at every
post, so changing it never needs the job stopped (onlyItemDiffers).
================
*/
func keepStaffWebhook(items nomad.VariableItems, existing *nomad.Variable, off bool) nomad.VariableItems {
	if off {
		delete(items, staffWebhookItem)
		return items
	}
	if _, named := items[staffWebhookItem]; named || existing == nil {
		return items
	}
	if stored := existing.Items[staffWebhookItem]; bugreport.ValidWebhookURL(stored) {
		items[staffWebhookItem] = stored
	}
	return items
}
