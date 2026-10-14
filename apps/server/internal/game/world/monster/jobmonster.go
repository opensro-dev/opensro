/*
===========================================================================

jobmonster.go - the thief and hunter monsters of the job system

A job wearer killed by an opposing job monster dies a job death
(CGObjPC_ResolvePvpKillRelation 4E6590, kind 1), and a thief monster
halves an ordinary death's EXP cap (4E6A97). The server tests the TypeID
word: CGObj_IsThiefMonster (482640) is 1/2/1/2, CGObj_IsHunterMonster
(4826E0) 1/2/1/3. A job monster never strikes its own side
(JobTargetRefused).

===========================================================================
*/

package monster

const (
	// thiefMonsterTypeWord and hunterMonsterTypeWord are the regular word
	// with TID4 2 and 3 (NativeTypeWord bits 11-15).
	thiefMonsterTypeWord  = 0x10c6
	hunterMonsterTypeWord = 0x18c6
)

// The job states CGObjPC_GetJobState (4DDC80) returns: the dressed job, or
// 4 outside job mode (CGObjPC_InitPvpAndJobStateOnLoad 4E1120 sets 4 first,
// then 1..3 only for a worn suit; enterworld.NoJobClass).
const (
	jobStateTrader  = 1
	jobStateThief   = 2
	jobStateHunter  = 3
	jobStateSpecial = 4
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

/*
================
Instance.JobTargetRefused

CGObjChar_ValidateAttackTarget (529110), which every attack reaches through
the world context's CheckAttackPermission (604E2A, 6052A2): a hunter monster
refuses a target whose job state is 1, 3 or 4 (CGObjPC_IsJobStateIn134
4E59C0, vtable +0x544), a thief monster one in 2 or 4
(CGObjPC_IsJobStateIn24 4E59E0, +0x548). State 4 is a player outside job
mode, so neither job monster strikes an ordinary player. Every other
monster passes. A COS answers both with its owner's state
(CGObjCOS_ForwardOwnerVf544/548), so a companion is tested with its
owner's. job is the native state: callers map "no suit" to 4.
================
*/
func (i Instance) JobTargetRefused(job uint8) bool {
	switch {
	case i.HunterMonster():
		return job == jobStateTrader || job == jobStateHunter || job == jobStateSpecial
	case i.ThiefMonster():
		return job == jobStateThief || job == jobStateSpecial
	}
	return false
}
