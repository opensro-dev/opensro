/*
===========================================================================

tombbox.go - the Ch'in Tomb treasure boxes: their tables and their rolls

Port-only switch, native content (owner 2026-11-19, #4810). The v1.150
client defines the four boxes on the tomb's second floor (characterdata
NPC_TQ_TREASUREBOX1-4), their keys (ITEM_ETC_TREASUREBOX_KEY_*) and every
talk line, but the shipped world places no box. The v1.188 server runs
each box as its own hard-coded event: a key opens the box, a weighted
roll picks one of nine outcomes, and the key is spent. The same events
drop their keys from tomb monsters. The later client marks the boxes
deleted at one publisher's request, so the switch is off by default.

This package owns the data and the arithmetic; action/tombbox.go owns the
talk, the open and the key drop, and simulation/npctombbox.go places the
boxes. The evidence (addresses, constants) is in the research tree:
investigations/server/tomb-treasure-boxes.

===========================================================================
*/
package tombbox

import "strings"

// Env is the switch; absent and unknown values leave the tomb native.
const Env = "SRO_TOMB_TREASURE_BOXES"

/*
================
Enabled - port-only; absent and unknown values stay native.
================
*/
func Enabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on":
		return true
	}
	return false
}

// The outcomes, in the order their weights are stored.
const (
	OutcomeDamage     = iota // the box hurts the opener
	OutcomeGuards            // the box summons its guards
	OutcomeGold              // a gold purse
	OutcomePotions           // a stack of potions
	OutcomeRecipe            // one alchemy recipe
	OutcomeItem              // top-degree normal item, not repairable
	OutcomeItemRepair        // top-degree normal item, repairable
	OutcomeSeal              // top-degree sealed item, not repairable
	OutcomeSealRepair        // top-degree sealed item, repairable
	OutcomeCount
)

// The box talk lines (v1.150 textquest): the option row, the damage and
// summon answers, and the two refusals.
const (
	TalkOption    = "SN_TALK_QET_TQ_TREASUREBOX_01"
	TalkDamage    = "SN_TALK_QET_TQ_TREASUREBOX_02"
	TalkGuards    = "SN_TALK_QET_TQ_TREASUREBOX_03"
	TalkNoKey     = "SN_TALK_QET_TQ_TREASUREBOX_04"
	TalkBagFull   = "SN_TALK_QET_TQ_TREASUREBOX_05"
	ServicePrefix = "QET_TQ_TREASUREBOX:open:"
)

// MinFreeSlots is the room the open demands before it looks for the key:
// with fewer free bag slots it answers the bag-full line.
const MinFreeSlots = 2

// GuardRadius is the ring the guards land in around the opener.
const GuardRadius = 30

// TombMonsterPrefix marks the monsters whose death can drop a key: the
// server flags every characterdata row whose codename starts with it.
const TombMonsterPrefix = "MOB_TQ_"

// PartyKeyFactor multiplies a key's chance when the dead monster is a
// party monster.
const PartyKeyFactor = 8

/*
================
Box

One box event's table. Weights are percents over the nine outcomes.
Item is the normal item's plus (not repairable, then repairable); Seal is
the sealed item's grade (1 Star, 2 Moon, 3 Sun) and its plus.
================
*/
type Box struct {
	Npc           string
	Key           string
	KeyChance     float64 // percent per tomb monster death
	Weights       [OutcomeCount]float64
	DamagePercent int64
	Guard         string
	GuardCount    int
	Gold          int64
	Potion        string
	PotionCount   uint32
	Recipe        string
	ItemPlus      [2]uint8 // not repairable, repairable
	SealGrade     [2]int   // not repairable, repairable
	SealPlus      [2]uint8 // not repairable, repairable
}

/*
================
Boxes

The four events as the v1.188 server's init functions store them, box 1
(Courage) to box 4 (Heroes).
================
*/
var Boxes = [...]Box{
	{
		Npc: "NPC_TQ_TREASUREBOX1", Key: "ITEM_ETC_TREASUREBOX_KEY_COURAGE", KeyChance: 0.5,
		Weights:       [OutcomeCount]float64{9.2818, 30, 30, 20, 10, 0.5666, 0.0944, 0.0566, 0.0006},
		DamagePercent: 10, Guard: "MOB_TQ_TREASURE_GUARD_CLON", GuardCount: 2, Gold: 38000,
		Potion: "ITEM_ETC_ALL_POTION_05", PotionCount: 50, Recipe: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_SHIELD_B",
		ItemPlus: [2]uint8{4, 1}, SealGrade: [2]int{1, 1}, SealPlus: [2]uint8{0, 0},
	},
	{
		Npc: "NPC_TQ_TREASUREBOX2", Key: "ITEM_ETC_TREASUREBOX_KEY_TRIAL", KeyChance: 0.25,
		Weights:       [OutcomeCount]float64{8.8849, 30, 30, 20, 10, 0.85, 0.17, 0.0944, 0.0007},
		DamagePercent: 20, Guard: "MOB_TQ_TREASURE_GUARD_CLON", GuardCount: 2, Gold: 76000,
		Potion: "ITEM_ETC_HP_SPOTION_01", PotionCount: 10, Recipe: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_B",
		ItemPlus: [2]uint8{5, 2}, SealGrade: [2]int{1, 1}, SealPlus: [2]uint8{3, 3},
	},
	{
		Npc: "NPC_TQ_TREASUREBOX3", Key: "ITEM_ETC_TREASUREBOX_KEY_CHALLENGE", KeyChance: 0.16667,
		Weights:       [OutcomeCount]float64{8.6398, 30, 30, 20, 10, 1.02, 0.2125, 0.1275, 0.0002},
		DamagePercent: 30, Guard: "MOB_TQ_TREASURE_GUARD", GuardCount: 1, Gold: 110000,
		Potion: "ITEM_ETC_MP_SPOTION_01", PotionCount: 10, Recipe: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_B",
		ItemPlus: [2]uint8{6, 3}, SealGrade: [2]int{2, 2}, SealPlus: [2]uint8{0, 0},
	},
	{
		Npc: "NPC_TQ_TREASUREBOX4", Key: "ITEM_ETC_TREASUREBOX_KEY_HERO", KeyChance: 0.025,
		Weights:       [OutcomeCount]float64{2.2042, 5, 42.5, 20, 23, 5.6666, 1.0625, 0.5666, 0.0001},
		DamagePercent: 40, Guard: "MOB_TQ_TREASURE_GUARD", GuardCount: 1, Gold: 400000,
		Potion: "ITEM_ETC_ALL_SPOTION_01", PotionCount: 50, Recipe: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B",
		ItemPlus: [2]uint8{7, 4}, SealGrade: [2]int{3, 3}, SealPlus: [2]uint8{0, 0},
	},
}

/*
================
BoxAt

The box an NPC codename names.
================
*/
func BoxAt(npc string) (Box, bool) {
	for _, box := range Boxes {
		if box.Npc == npc {
			return box, true
		}
	}
	return Box{}, false
}

/*
================
Service

The talk service that opens one box (NpcQuestHooks.Finish routes it).
================
*/
func Service(box Box) string {
	return ServicePrefix + box.Npc
}

// randMax is the C runtime's RAND_MAX, the open roll's denominator.
const randMax = 32767

/*
================
PickOutcome

The open roll: r = rand()/32767*100, then the first outcome whose running
sum of weights is not below r. A draw past every sum (the weights total
just under 100) falls to the last outcome, as the walk's end does.
================
*/
func PickOutcome(box Box, draw uint32) int {
	roll := float64(draw%(randMax+1)) / randMax * 100
	sum := 0.0
	for outcome, weight := range box.Weights {
		sum += weight
		if sum >= roll {
			return outcome
		}
	}
	return OutcomeCount - 1
}

// keyRollScale is the key roll's resolution: a million steps.
const keyRollScale = 1_000_000

/*
================
KeyDrops

The key roll for one tomb monster death: r = ((hi << 15) | lo) % 1e6 / 1e6
over two 15-bit draws; r below one step fails; otherwise the key drops
when chance/100 >= r, the chance multiplied by PartyKeyFactor for a party
monster.
================
*/
func KeyDrops(box Box, party bool, hi, lo uint32) bool {
	chance := box.KeyChance
	if party {
		chance *= PartyKeyFactor
	}
	r := float64(((hi&randMax)<<15|lo&randMax)%keyRollScale) / keyRollScale
	if r < 1.0/keyRollScale {
		return false
	}
	return chance/100 >= r
}

/*
================
IsTombMonster

Whether a monster's death rolls the keys: its codename starts with the
tomb prefix.
================
*/
func IsTombMonster(codename string) bool {
	return strings.HasPrefix(codename, TombMonsterPrefix)
}
