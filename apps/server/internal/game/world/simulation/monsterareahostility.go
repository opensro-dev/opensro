/*
===========================================================================

monsterareahostility.go - which candidates a monster's area attack strikes

The action owner gathers a monster area's candidates (players and their
companions) at release; this file owns the hostility it filters them by,
the same rules the acquisition scan reads.

===========================================================================
*/

package simulation

import "opensro.online/server/internal/game/world/monster"

/*
================
MonsterAreaCandidate

One player, or one of its companions (OwnerGid set), inside a monster
area's search volume.
================
*/
type MonsterAreaCandidate struct {
	Gid              uint32
	OwnerGid         uint32
	Band             uint8
	NativeBodyStatus uint8
	Guard            monster.FirstAttackGuard
}

/*
================
MonsterAreaStrikes

CSkillManager_IsHostileTargetEligible (5A1AD0) for a monster caster: the
monster's own hostility (vtable +0x624, mode 1, 5298C0) decides, the same
test that admits a player or companion target (5299E0: body status, Fear's
source exclusion, the fellow band, the first-attack protection).
================
*/
func MonsterAreaStrikes(actor monster.Instance, candidate MonsterAreaCandidate) bool {
	pose := playerPose{Gid: candidate.Gid, OwnerGid: candidate.OwnerGid, Band: candidate.Band,
		NativeBodyStatus: candidate.NativeBodyStatus, Guard: candidate.Guard}
	if !monster.AllowsTargetStatus(actor.Ref.TidWord, actor.Nest.NativeTacticsFlags, pose.NativeBodyStatus) {
		return false
	}
	if pose.OwnerGid != 0 {
		return ordinaryCompanionHostility(actor, pose)
	}
	return ordinaryPlayerHostility(actor, pose)
}
