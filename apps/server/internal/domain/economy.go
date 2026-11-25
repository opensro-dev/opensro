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

// EconomyDoorUndeclared is the reserved door of a change the commit found
// that no door noted (store/ledger_door.go): a writer changed a character
// outside the door lifecycle. Its gold rows carry EconomyReasonUndeclared,
// as unknown a kind as unattributed, and its item rows this door word.
const EconomyDoorUndeclared = "undeclared-write"

// EconomyReasonUndeclared is the reason of an undeclared change's gold.
const EconomyReasonUndeclared = "undeclared"

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
	// An EventBot win (social/eventbot, port-only #3533): the prize
	// item lands in the winner's bag through this door.
	"eventbot-reward": {gain: "event_reward"},
	"shop-buyback":    {loss: "buyback"},
	// A portal or guide's fee, and its refund when the travel rolls back.
	"portal-travel":         {loss: "teleport_fee"},
	"guide-reverse-return":  {loss: "teleport_fee"},
	"portal-world-rollback": {gain: "teleport_refund"},
	"portal-entry-rollback": {gain: "teleport_refund"},
	"pickup-gold":           {gain: "monster_gold"},
	// A pickup pet looting gold (cosground.go) is monster gold too.
	"cos-ground-item":  {gain: "monster_gold"},
	"gold-drop":        {loss: "gold_drop"},
	"quest-reward":     {gain: "quest_reward", loss: "quest_fee"},
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
	// The operator's gold correction (#3309): one signed adjustment
	// through the character's own door. Its kind follows the sign
	// (EconomyReasonKindAt): a credit mints, a debit burns.
	"operator-adjust-gold": {gain: "operator_adjust", loss: "operator_adjust"},
	// Loading a transport at a specialty shop (action/commerce.go): the
	// caravan outcome the trade-sale doors complete, told apart from an
	// ordinary purchase so a run's start is on the ledger too (#3162).
	"trade-load": {loss: "trade_goods_cost"},
	// A market buy paid in gold (port-only, store/market.go): the buyer's
	// gold leaves for the seller, less the 1% fee; a website listing's
	// seller is paid in its warehouse, a bag listing's character by
	// market-proceeds when its lane claims the sale (market_proceeds.go).
	"market-buy":      {loss: "market_sale"},
	"market-proceeds": {gain: "market_sale"},
}

// economyMemberReasons maps a settlement door whose label names its subject
// ("trade-sale 42") to the subject's reason and every other member's. A
// trade-goods sale pays the seller and the party's shares in one commit
// (action/commerce.go commerceDoorLabel); a thief's sale at the den is told
// apart from a trader's by its door. The trade doors carry the caravan's
// "-<slots>s<stars>" suffix on the door word (#3162), which
// economyDoorBase strips before this table is read.
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

// itemBulkDoors are the doors whose item changes the item ledger
// (store/item_ledger.go, #3307) folds into per-character hourly buckets
// instead of exact rows: consumption and loot, a change every potion,
// arrow or pickup, where a row each would be noise beside the evidence
// rows of grants, sales, transfers and destroys. The ammunition debit
// rides the four strike doors (action/ammunition.go applyAmmunitionDebit:
// skillcombat.go, skillarea.go, pvpstrike.go, playerstrike_cos.go). An NPC
// shop's sold items also land in the sale buckets (NoteItemSale), which
// carry the gold per item a holdings diff cannot see.
var itemBulkDoors = map[string]bool{
	"item-use":             true,
	"potion-recovery":      true,
	"player-basic-attack":  true,
	"player-area-attack":   true,
	"player-strike-player": true,
	"player-strike-cos":    true,
	"pickup-item":          true,
	"cos-ground-item":      true,
	"shop-transaction":     true,
}

// The free grants (#3307): what the shard hands an account for nothing,
// counted per account and day at grant time (store/grant_days.go) so a
// create-grant-sell-delete loop stays visible after the character is
// gone. The two item grants are door words (enterworld/bootstrap.go);
// the three silk grants name their store writers (betasilk.go,
// referrals.go). An operator's grant is staff work, not a free grant.
const (
	GrantStarterKit    = "starter-kit"
	GrantStarterRefill = "starter-refill"
	GrantStarterSilk   = "starter-silk"
	GrantBetaSilk      = "beta-silk"
	GrantReferralSilk  = "referral-silk"
	// GrantEventBotReward is the EventBot host's win payment
	// (social/eventbot, port-only #3533): an audited free grant, so
	// the daily win cap reads the same rows the audit keeps.
	GrantEventBotReward = "eventbot-reward"
	// GrantEventBotTicket is the Lottery ticket door (social/eventbot,
	// port-only #3539): one ticket to a unique's kill owner, one
	// spent at entry. Audited like any grant, but deliberately not
	// a free grant (freeGrantDoors): a ticket is entry currency,
	// not a win, and must never count toward the daily win cap.
	GrantEventBotTicket = "eventbot-ticket"
)

// freeGrantDoors are the item doors whose gains count as free grants.
var freeGrantDoors = map[string]bool{
	GrantStarterKit:     true,
	GrantStarterRefill:  true,
	GrantEventBotReward: true,
}

/*
================
FreeGrantDoor

Whether an item door's gains are a free grant. door is EconomyDoor's
word.
================
*/
func FreeGrantDoor(door string) bool {
	return freeGrantDoors[door]
}

/*
================
ItemDoorBulk

Whether the item ledger buckets a door's item changes instead of
keeping a row each. door is EconomyDoor's word.
================
*/
func ItemDoorBulk(door string) bool {
	return itemBulkDoors[door]
}

/*
================
economyDoorBase

A door word without its caravan cargo suffix. The trade doors record
the caravan they moved gold for as "-<slots>s<stars>" on the door word
(action/commerce.go, #3162: the transport bag the goods rode in and
the seller's star rating), so one sale's rows read "trade-sale-32s5"
in the ledger while the reason tables stay keyed by the bare door.
Only that exact digit shape strips, so a door whose name merely ends
in dashes and letters keeps its whole name.
================
*/
func economyDoorBase(door string) string {
	dash := strings.LastIndex(door, "-")
	if dash <= 0 {
		return door
	}
	suffix := door[dash+1:]
	star := strings.IndexByte(suffix, 's')
	if star <= 0 || star == len(suffix)-1 {
		return door
	}
	for _, r := range suffix[:star] + suffix[star+1:] {
		if r < '0' || r > '9' {
			return door
		}
	}
	return door[:dash]
}

/*
================
EconomyReason

The reason of a gold change of the given sign committed by the door.
================
*/
func EconomyReason(door string, gain bool) string {
	if door == EconomyDoorUndeclared {
		return EconomyReasonUndeclared
	}
	reasons := economyDoorReasons[economyDoorBase(door)]
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
	reasons, settlement := economyMemberReasons[economyDoorBase(door)]
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
	"event_reward":           EconomyKindMint,
	"trade_payout":           EconomyKindMint,
	"trade_sale_credit":      EconomyKindMint,
	"trade_party_share":      EconomyKindMint,
	"thief_den_sale":         EconomyKindMint,
	"teleport_refund":        EconomyKindMint,
	"starting_gold":          EconomyKindMint,
	"gm_grant":               EconomyKindMint,
	// The credit side's kind; a debit burns instead (EconomyReasonKindAt).
	"operator_adjust":      EconomyKindMint,
	"shop_buy":             EconomyKindBurn,
	"trade_goods_cost":     EconomyKindBurn,
	"buyback":              EconomyKindBurn,
	"gold_drop":            EconomyKindBurn,
	"repair":               EconomyKindBurn,
	"alchemy":              EconomyKindBurn,
	"skill_learn":          EconomyKindBurn,
	"skill_untrain":        EconomyKindBurn,
	"quest_fee":            EconomyKindBurn,
	"job_fee":              EconomyKindBurn,
	"fortress_fee":         EconomyKindBurn,
	"guild_fee":            EconomyKindBurn,
	"academy_fee":          EconomyKindBurn,
	"guild_war_stake":      EconomyKindBurn,
	"teleport_fee":         EconomyKindBurn,
	"exchange":             EconomyKindTransfer,
	"stall_sale":           EconomyKindTransfer,
	"market_sale":          EconomyKindTransfer,
	"storage_withdraw":     EconomyKindTransfer,
	"storage_deposit":      EconomyKindTransfer,
	"guild_storage":        EconomyKindTransfer,
	"most_wanted_transfer": EconomyKindTransfer,
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
EconomyReasonKindAt

The kind of one change of a reason in the given direction. Every reason
keeps its EconomyReasonKind in both directions except operator_adjust,
the operator's gold correction (#3309): it mints gold into the shard
when it credits a character and burns it out when it debits one, so its
kind follows the change's sign.
================
*/
func EconomyReasonKindAt(reason string, gain bool) string {
	if reason == "operator_adjust" {
		if gain {
			return EconomyKindMint
		}
		return EconomyKindBurn
	}
	return EconomyReasonKind(reason)
}

/*
================
EconomyReasons

Every named reason, sorted, for the endpoint's meta.
================
*/
func EconomyReasons() []string {
	names := make([]string, 0, len(economyReasonKinds)+2)
	for name := range economyReasonKinds {
		names = append(names, name)
	}
	names = append(names, EconomyReasonUnattributed, EconomyReasonUndeclared)
	slices.Sort(names)
	return names
}
