/*
===========================================================================

jobmonster.go - the thief and hunter monsters of the job system

A job wearer killed by an opposing job monster dies a job death
(CGObjPC_ResolvePvpKillRelation 4E6590, kind 1), and a thief monster
halves an ordinary death's EXP cap (4E6A97). The server tests the TypeID
word: CGObj_IsThiefMonster (482640) is 1/2/1/2, CGObj_IsHunterMonster
(4826E0) 1/2/1/3.

===========================================================================
*/

package monster

const (
	// thiefMonsterTypeWord and hunterMonsterTypeWord are the regular word
	// with TID4 2 and 3 (NativeTypeWord bits 11-15).
	thiefMonsterTypeWord  = 0x10c6
	hunterMonsterTypeWord = 0x18c6
)

/*
================
TradeAppearance

A thief or hunter reference has no model of its own (characterdata names
"xxx"): CICMonster_DeserializeSpawnPacket (861B00) reads its trade
variant and CICMonster_InitializeTradeEquipmentAndSkill (861720) dresses
a character body from the trade equipment table. The model bake skips
these; their look is the job appearance's, not a BSR's.
================
*/
func (r MonsterRef) TradeAppearance() bool {
	word := NativeTypeWord(r) & typeWordFlagMask
	return word == thiefMonsterTypeWord || word == hunterMonsterTypeWord
}

/*
================
ThiefMonster
================
*/
func (i Instance) ThiefMonster() bool {
	return NativeTypeWord(i.Ref)&typeWordFlagMask == thiefMonsterTypeWord
}

/*
================
HunterMonster
================
*/
func (i Instance) HunterMonster() bool {
	return NativeTypeWord(i.Ref)&typeWordFlagMask == hunterMonsterTypeWord
}
