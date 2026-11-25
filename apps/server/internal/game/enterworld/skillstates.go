/*
===========================================================================

skillstates.go - the later retail casting states of the 20% damage buffs

Skilldata column 19 packs up to three casting-state bytes. The native buff
admission refuses a buff whose states an active effect already holds
(CSkillManager_CheckCastingStateConflict), so the column decides which buffs
stack. The v1.150 client's data gives the 20% damage scroll states 5 and 4,
the same as the Chinese lightning (5) and fire (4) attack-up buffs, so the
scroll and those buffs refuse each other.

A later retail revision moved the 20% damage family onto its own state 94
(the ISRO-R shard backup and the later DB-to-media dumps): the Mall scroll
holds 94 alone and stacks with the attack-up buffs; the quest and event
damage-up buffs hold 94, 5 and 4, so they still refuse the attack-up buffs
and the scroll. The owner adopted that revision (2026-11-25, #4382; a later
native client is native). This table gives those rows their later value and
leaves every other row as the v1.150 data says.

===========================================================================
*/

package enterworld

// damageFamilyState is the later retail state every 20% damage buff holds.
const damageFamilyState = 94

// damageFamilyAttackStates are states 94, 5 and 4: the family's own state
// plus the attack-up buffs' states, which those rows keep.
const damageFamilyAttackStates = damageFamilyState | 5<<8 | 4<<16

// laterPackedStates maps a codename to its later retail column 19.
var laterPackedStates = map[string]uint32{
	"SKILL_MALL_DAMAGE_INC_20P_SCROLL_01": damageFamilyState,
	"SKILL_ETC_GNGWC_DAMAGE_UP_20_01":     damageFamilyAttackStates,
	"SKILL_QNO_CA_GORIA_6_01_01":          damageFamilyAttackStates,
	"SKILL_QNO_CA_GORIA_6_03_01":          damageFamilyAttackStates,
	"SKILL_QNO_CH_GENARAL_1_01_01":        damageFamilyAttackStates,
	"SKILL_QNO_CH_HWAN_1_4_04_01":         damageFamilyAttackStates,
	"SKILL_QNO_CH_SPECIAL_2_02_01":        damageFamilyAttackStates,
	"SKILL_QNO_CH_WHAN_2_1_01_01":         damageFamilyAttackStates,
	"SKILL_QNO_EU_EASTEU_14_02_01":        damageFamilyAttackStates,
	"SKILL_QNO_EU_EASTEU_21_02_01":        damageFamilyAttackStates,
	"SKILL_QNO_EU_GENERAL_1_01_01":        damageFamilyAttackStates,
	"SKILL_QNO_EU_IVY_2_01_01":            damageFamilyAttackStates,
	"SKILL_QNO_EU_WHAN_2_1_01_01":         damageFamilyAttackStates,
	"SKILL_QNO_WC_WAREHOUSE_W_2_04_01":    damageFamilyAttackStates,
}

/*
================
packedStatesFor

The casting states a skill row admits with: the later retail value for the
20% damage family, else the row's own column 19.
================
*/
func packedStatesFor(codename string, column uint32) uint32 {
	if later, ok := laterPackedStates[codename]; ok {
		return later
	}
	return column
}
