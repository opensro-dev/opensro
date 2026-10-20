/*
===========================================================================

uniquerules.go - unique-monster reward rules (port-only, not native)

Port-only, not native (owner-approved TUNING deviation, 2026-10-20,
hunting patch). Native CGObjMob_RollDropAdmission (SR_GameServer
0x004C1840) admits each prepared drop of a unique exactly like any other
monster's: past six levels above the monster the threshold falls by four
percent per level, so a level-90 hunter loots nothing from Tiger Girl
(level 20, threshold -180) and only same-level uniques such as Demon
Shaitan ever paid out. Two switches change that, both default ON:

  - SRO_UNIQUE_GAP_EXEMPT: a unique's drops (spawn grade 3 or 8, the
    RollsUniqueDrops grades) bypass the 4C1840 threshold entirely.
  - SRO_UNIQUE_POP_TICKET: every unique kill leaves exactly one Magic
    Pop ticket (ITEM_MALL_GACHA_CARD). The native assigned rows already
    carry the card at probability 1.0 (vSRO _RefMonster_AssignedItemDrop;
    two for the Bone Lord and the Roc), so the rule retires every
    admitted native card and appends one guaranteed card instead: never
    zero, never two.

Either switch at "off" restores the exact native rule for its half; both
off is byte-native. The zero value of UniqueRules is native, so a Runtime
that never wires the env behaves as the original server.

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

/*
================
UniqueRules

The two unique-reward switches. The zero value is native (both off);
UniqueRulesFromEnv is the default-on operator reading.
================
*/
type UniqueRules struct {
	GapExempt bool
	PopTicket bool
}

/*
================
UniqueRulesFromEnv

Both switches default ON (owner, 2026-10-20); only an explicit "off",
"0" or "false" restores the native rule for that switch.
================
*/
func UniqueRulesFromEnv() UniqueRules {
	return UniqueRules{
		GapExempt: uniqueRuleOn(os.Getenv(EnvUniqueGapExempt)),
		PopTicket: uniqueRuleOn(os.Getenv(EnvUniquePopTicket)),
	}
}

/*
================
uniqueRuleOn

A default-on switch: anything but an explicit off value is on, so an
unset or mistyped variable keeps the owner's hunting patch.
================
*/
func uniqueRuleOn(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "0", "false":
		return false
	}
	return true
}
