/*
===========================================================================

temptation.go - which monsters the Bard's Temptation turns on their own

Temptation and Curious Temptation (SKILL_EU_BARD_FORGETA_TARGET_A/B) roll
the Confusion status (ca, abnormal slot 16). The skill's text limits it to
"regular monsters and regular party Monsters": a tempted monster attacks
the monsters around it instead of players while the status lasts.

===========================================================================
*/

package monster

import "opensro.online/server/internal/game/abnormal"

const (
	// regularMonsterTypeWord is the full TypeID word (NativeTypeWord, TID4
	// in bits 11-15) of an ordinary MOB, TID4 1. Thieves, hunters and quest
	// monsters (TID4 2..4) carry other words. CGObjMob_EvaluateHostility
	// (5299E0) uses the same word, masked with ~1, to decide which monsters
	// honour a first-attack protection.
	regularMonsterTypeWord = 0x08c6

	// typeWordFlagMask drops bit 0 before the comparison, as 5299E0 does.
	typeWordFlagMask = 0xfffe

	// rarityGradeMask selects the spawn grade (0 normal, 1 champion,
	// 3 unique, 4 giant, 5 titan, 6 elite); the high nibble is the party
	// monster flag, which Temptation admits.
	rarityGradeMask = 0x0f
)

/*
================
RegularMonster

An ordinary MOB (TID4 1) of the normal grade, solo or party. Champions,
giants, titans, elites, uniques, and thief, hunter or quest monsters are
not regular.
================
*/
func (i Instance) RegularMonster() bool {
	return NativeTypeWord(i.Ref)&typeWordFlagMask == regularMonsterTypeWord &&
		i.Rarity()&rarityGradeMask == 0
}

/*
================
Tempted

A live regular monster whose Confusion slot is active. Owner's rule: the
status has no effect on any other monster, so the predicate, not only the
roll, refuses them.
================
*/
func (i Instance) Tempted() bool {
	return i.CurrentHP != 0 && i.Abnormal != nil && i.Abnormal.Slots[abnormal.Confusion].Active && i.RegularMonster()
}
