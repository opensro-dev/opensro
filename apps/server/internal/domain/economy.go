/*
===========================================================================

economy.go - the economy ledger's reason vocabulary

Port-only, not native: the original keeps no economy journal. Every gold
change a store door commits becomes one ledger row (store/economy_ledger.go)
whose reason comes from the door's label and the change's direction. The
names are the economy endpoint's contract (v1): once shipped a name never
changes meaning; new names may be added.

===========================================================================
*/
package domain

import "strings"

// EconomyReasonUnattributed names a change whose door the table below does
// not know; its row keeps the door label so the table can learn it.
const EconomyReasonUnattributed = "unattributed"

/*
================
economyDoorReason

The reasons of one door: what a gain is, and what a loss is. An empty name
leaves that direction unattributed.
================
*/
type economyDoorReason struct {
	gain string
	loss string
}

// economyDoorReasons maps a door label (its first word, see EconomyDoor) to
// its reasons. Several gameplay paths share a door (an NPC shop's buy and
// sell are both "shop-transaction"), so the direction picks the reason.
var economyDoorReasons = map[string]economyDoorReason{
	"shop-transaction": {gain: "npc_sale", loss: "shop_buy"},
	// A new character's starting gold, at creation or (for a record made
	// before the creation seed existed) at its first world entry.
	"create-character": {gain: "starting_gold"},
	"bootstrap-seed":   {gain: "starting_gold"},
	"shop-buyback":     {loss: "buyback"},
	// A trade-goods sale's payouts: the seller's credit and every party
	// share commit in one settlement (commerce.go commerceDoorLabel).
	"trade-sale": {gain: "trade_payout"},
	// A portal or guide's fee, and its refund when the travel rolls back.
	"portal-travel":         {loss: "teleport_fee"},
	"guide-reverse-return":  {loss: "teleport_fee"},
	"portal-world-rollback": {gain: "teleport_refund"},
	"portal-entry-rollback": {gain: "teleport_refund"},
	"pickup-gold":           {gain: "monster_gold"},
	// A pickup pet looting gold (cosground.go) is monster gold too.
	"cos-ground-item":  {gain: "monster_gold"},
	"gold-drop":        {loss: "gold_drop"},
	"quest-reward":     {gain: "quest_reward"},
	"quest-start":      {loss: "quest_fee"},
	"quest-collect":    {gain: "quest_reward"},
	"quest-giveup":     {gain: "quest_refund"},
	"npc-repair":       {loss: "repair"},
	"repair-hammer":    {loss: "repair"},
	"alchemy":          {loss: "alchemy"},
	"alchemy-process":  {loss: "alchemy"},
	"skill-learn":      {loss: "skill_learn"},
	"mastery-levelup":  {loss: "skill_learn"},
	"skill-withdrawal": {loss: "skill_untrain"},
	"exchange":         {gain: "exchange", loss: "exchange"},
	"stall-sale":       {gain: "stall_sale", loss: "stall_sale"},
	"storage-transfer": {gain: "storage_withdraw", loss: "storage_deposit"},
	"guild-storage":    {gain: "guild_storage", loss: "guild_storage"},
	"job-join":         {loss: "job_fee"},
	"job-outcome":      {gain: "weekly_reward_paid"},
	// A kill pays gold only as a most-wanted bounty (port-only).
	"player-abnormal-kill":      {gain: "most_wanted_bounty"},
	"player-strike-player":      {gain: "most_wanted_bounty"},
	"player-credited-hit":       {gain: "most_wanted_bounty"},
	"fortress-tax":              {gain: "fortress_withdraw"},
	"fortress-staff":            {loss: "fortress_fee"},
	"fortress-item-forge":       {loss: "fortress_fee"},
	"fortress-war-apply":        {loss: "fortress_fee"},
	"fortress-structure-repair": {loss: "fortress_fee"},
	"guild-level-up":            {loss: "guild_fee"},
	"guild-war-begin":           {loss: "guild_war_stake"},
	"guild-war-compensation":    {gain: "guild_war_compensation"},
	"mercenary-attribute":       {loss: "guild_fee"},
	"operator-grant-items":      {gain: "gm_grant", loss: "gm_grant"},
}

/*
================
EconomyDoor

The label's first word: door labels may carry a name or an id after a
space ("guild-level-up 1/7"), which the ledger never stores.
================
*/
func EconomyDoor(label string) string {
	door, _, _ := strings.Cut(strings.TrimSpace(label), " ")
	return door
}

/*
================
EconomyReason

The reason of a gold change of the given sign committed by the door.
================
*/
func EconomyReason(door string, gain bool) string {
	reasons := economyDoorReasons[door]
	name := reasons.loss
	if gain {
		name = reasons.gain
	}
	if name == "" {
		return EconomyReasonUnattributed
	}
	return name
}
