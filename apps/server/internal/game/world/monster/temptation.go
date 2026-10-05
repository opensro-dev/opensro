/*
===========================================================================

temptation.go - active Confusion participation in monster combat

Cast admission owns the base-grade restriction. The native 4A4F70
callback starts the AI event on any live monster carrying Confusion.

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
Tempted

4A4F70 tests monster identity and alive state; it does not repeat target
admission or restrict the RefObj's TypeID4.
================
*/
func (i Instance) Tempted() bool {
	return i.CurrentHP != 0 && i.Abnormal != nil && i.Abnormal.Slots[abnormal.Confusion].Active
}
