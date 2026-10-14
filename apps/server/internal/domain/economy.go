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

import (
	"slices"
	"strconv"
	"strings"
)

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
	"job-alias":        {loss: "job_fee"},
	"job-outcome":      {gain: "weekly_reward_paid"},
	// A kill pays gold only as a most-wanted bounty (port-only).
	"player-abnormal-kill": {gain: "most_wanted_bounty"},
	"player-strike-player": {gain: "most_wanted_bounty"},
	"player-credited-hit":  {gain: "most_wanted_bounty"},
	// The bounty taken from the victim instead (SRO_MOST_WANTED_BOUNTY_TRANSFER).
	"most-wanted-bounty":        {gain: "most_wanted_transfer", loss: "most_wanted_transfer"},
	"fortress-tax":              {gain: "fortress_withdraw"},
	"fortress-staff":            {loss: "fortress_fee"},
	"fortress-item-forge":       {loss: "fortress_fee"},
	"fortress-war-apply":        {loss: "fortress_fee"},
	"fortress-structure-repair": {loss: "fortress_fee"},
	"guild-level-up":            {loss: "guild_fee"},
	"training-camp-open":        {loss: "academy_fee"},
	"guild-war-begin":           {loss: "guild_war_stake"},
	"guild-war-compensation":    {gain: "guild_war_compensation"},
	"mercenary-attribute":       {loss: "guild_fee"},
	"operator-grant-items":      {gain: "gm_grant", loss: "gm_grant"},
}

// economyMemberReasons maps a settlement door whose label names its subject
// ("trade-sale 42") to the subject's reason and every other member's. A
// trade-goods sale pays the seller and the party's shares in one commit
// (action/commerce.go commerceDoorLabel); a thief's sale at the den is told
// apart from a trader's by its door.
var economyMemberReasons = map[string]struct{ subject, member string }{
	"trade-sale": {subject: "trade_sale_credit", member: "trade_party_share"},
	"thief-sale": {subject: "thief_den_sale", member: "trade_party_share"},
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

/*
================
EconomyRowReason

The reason of one character's gold change in a door's commit. A settlement
door names its subject's id after the door word; its subject and the other
members gain under different reasons. Every other door goes through
EconomyReason.
================
*/
func EconomyRowReason(label string, charID int64, gain bool) string {
	door, rest, _ := strings.Cut(strings.TrimSpace(label), " ")
	reasons, settlement := economyMemberReasons[door]
	if !settlement || !gain {
		return EconomyReason(door, gain)
	}
	if subject, err := strconv.ParseInt(rest, 10, 64); err == nil && subject == charID {
		return reasons.subject
	}
	return reasons.member
}

//============================================================================

// The economy kinds of a reason (contract v1 meta): a mint adds gold to the
// shard, a burn removes it, a transfer only moves it between holders, and
// an unknown change is a door the table above does not name yet.
const (
	EconomyKindMint     = "mint"
	EconomyKindBurn     = "burn"
	EconomyKindTransfer = "transfer"
	EconomyKindUnknown  = "unknown"
)

// economyReasonKinds names every reason the door table produces. A gold
// drop is a burn: what nobody picks up expires, and what someone picks up
// comes back through pickup-gold as monster_gold. trade_payout is no longer
// produced (economyMemberReasons split it); rows written before keep a kind.
var economyReasonKinds = map[string]string{
	"npc_sale":               EconomyKindMint,
	"monster_gold":           EconomyKindMint,
	"quest_reward":           EconomyKindMint,
	"quest_refund":           EconomyKindMint,
	"weekly_reward_paid":     EconomyKindMint,
	"most_wanted_bounty":     EconomyKindMint,
	"fortress_withdraw":      EconomyKindMint,
	"guild_war_compensation": EconomyKindMint,
	"trade_payout":           EconomyKindMint,
	"trade_sale_credit":      EconomyKindMint,
	"trade_party_share":      EconomyKindMint,
	"thief_den_sale":         EconomyKindMint,
	"teleport_refund":        EconomyKindMint,
	"starting_gold":          EconomyKindMint,
	"gm_grant":               EconomyKindMint,
	"shop_buy":               EconomyKindBurn,
	"buyback":                EconomyKindBurn,
	"gold_drop":              EconomyKindBurn,
	"repair":                 EconomyKindBurn,
	"alchemy":                EconomyKindBurn,
	"skill_learn":            EconomyKindBurn,
	"skill_untrain":          EconomyKindBurn,
	"quest_fee":              EconomyKindBurn,
	"job_fee":                EconomyKindBurn,
	"fortress_fee":           EconomyKindBurn,
	"guild_fee":              EconomyKindBurn,
	"academy_fee":            EconomyKindBurn,
	"guild_war_stake":        EconomyKindBurn,
	"teleport_fee":           EconomyKindBurn,
	"exchange":               EconomyKindTransfer,
	"stall_sale":             EconomyKindTransfer,
	"storage_withdraw":       EconomyKindTransfer,
	"storage_deposit":        EconomyKindTransfer,
	"guild_storage":          EconomyKindTransfer,
	"most_wanted_transfer":   EconomyKindTransfer,
}

/*
================
EconomyReasonKind

The kind of a ledger reason; unattributed and unknown names are unknown.
================
*/
func EconomyReasonKind(reason string) string {
	if kind, known := economyReasonKinds[reason]; known {
		return kind
	}
	return EconomyKindUnknown
}

/*
================
EconomyReasons

Every named reason, sorted, for the endpoint's meta.
================
*/
func EconomyReasons() []string {
	names := make([]string, 0, len(economyReasonKinds)+1)
	for name := range economyReasonKinds {
		names = append(names, name)
	}
	names = append(names, EconomyReasonUnattributed)
	slices.Sort(names)
	return names
}
