/*
===========================================================================

uniquerules.go - unique-monster reward rules (port-only, not native)

Port-only, not native (owner-approved TUNING deviation, 2026-10-20,
hunting patch). Native CGObjMob_RollDropAdmission (SR_GameServer
0x004C1840) admits each prepared drop of a unique exactly like any other
monster's: past six levels above the monster the threshold falls by four
percent per level, so a level-90 hunter loots nothing from Tiger Girl
(level 20, threshold -180) and only same-level uniques such as Demon
Shaitan ever paid out. Four switches change that. The existing gap and
ticket rules default ON; the new equipment-level and seal rules are opt-in:

  - SRO_UNIQUE_GAP_EXEMPT: a unique's drops (spawn grade 3 or 8, the
    RollsUniqueDrops grades) bypass the 4C1840 threshold entirely.
  - SRO_UNIQUE_POP_TICKET: every unique kill leaves exactly one Magic
    Pop ticket (ITEM_MALL_GACHA_CARD). The native assigned rows already
    carry the card at probability 1.0 (vSRO _RefMonster_AssignedItemDrop;
    two for the Bone Lord and the Roc), so the rule retires every
    admitted native card and appends one guaranteed card instead: never
    zero, never two.
  - SRO_UNIQUE_MAX_LEVEL_LOOT: a unique in the installed catalog's top
    loot band (own level within ten of the highest installed level,
    item/loot.UniqueDropLevel: 90 native, 110 with the cap-110 catalog)
    rolls its equipment tables at that level instead of its own, so a
    top-band unique drops degree-9 gear at the native cap and degree-11
    gear once the cap moves to 110. Its ordinary (non-seal) picks also
    take the level's highest strength tier. Every other unique keeps its
    own level's table, as native. Default OFF.
  - SRO_UNIQUE_SEAL_DROPS: every unique kill leaves one Seal of Star of
    the highest degree the drop level offers and one Seal of Moon on a
    kill in ten. There is no guaranteed Sun: it stays on the ordinary
    rare roll. Appended after admission and the drop cap, so neither can
    take a guaranteed seal. Default OFF.

Any switch at "off" restores the exact native rule for its half; all
four off is byte-native. The zero value of UniqueRules is native, so a
Runtime that never wires the env behaves as the original server.

===========================================================================
*/
package progression

import (
	"os"
	"strings"
)

// EnvUniqueGapExempt exempts unique drops from the level-gap admission.
const EnvUniqueGapExempt = "SRO_UNIQUE_GAP_EXEMPT"

// EnvUniquePopTicket guarantees one Magic Pop ticket per unique kill.
const EnvUniquePopTicket = "SRO_UNIQUE_POP_TICKET"

// EnvUniqueMaxLevelLoot rolls unique equipment at the shard's highest level.
const EnvUniqueMaxLevelLoot = "SRO_UNIQUE_MAX_LEVEL_LOOT"

// EnvUniqueSealDrops guarantees the seal reward of every unique kill.
const EnvUniqueSealDrops = "SRO_UNIQUE_SEAL_DROPS"

/*
================
UniqueRules

The unique-reward switches. The zero value is native (all off).
================
*/
type UniqueRules struct {
	GapExempt bool
	PopTicket bool
	// MaxLevelLoot rolls a top-band unique's equipment tables at the
	// shard's highest installed level instead of the unique's own level
	// (action/uniqueDropTableLevel). Off is the native table.
	MaxLevelLoot bool
	// SealDrops guarantees one Seal of Star per unique kill plus the
	// Moon draw (action/prepareUniqueSeals). Off is native.
	SealDrops bool
}

/*
================
UniqueRulesFromEnv

The existing gap/ticket rules keep their approved default ON. The new
equipment-level and seal rules need explicit "on", "1" or "true"; unset
keeps native equipment selection and ordinary rare rolls for those rules.
================
*/
func UniqueRulesFromEnv() UniqueRules {
	return UniqueRules{
		GapExempt:    uniqueRuleOn(os.Getenv(EnvUniqueGapExempt), true),
		PopTicket:    uniqueRuleOn(os.Getenv(EnvUniquePopTicket), true),
		MaxLevelLoot: uniqueRuleOn(os.Getenv(EnvUniqueMaxLevelLoot), false),
		SealDrops:    uniqueRuleOn(os.Getenv(EnvUniqueSealDrops), false),
	}
}

/*
================
uniqueRuleOn

Explicit on/off values, otherwise the rule's own default. The old default-on
rules retain their permissive parsing; opt-in rules fail closed on a typo.
================
*/
func uniqueRuleOn(value string, defaultOn bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "1", "true":
		return true
	case "off", "0", "false":
		return false
	}
	return defaultOn
}
