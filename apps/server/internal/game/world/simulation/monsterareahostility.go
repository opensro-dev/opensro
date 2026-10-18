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
	// JobState is the player's job state, or a companion's owner's
	// (JobTargetRefused).
	JobState uint8
}

/*
================
MonsterAreaStrikes

CSkillManager_IsHostileTargetEligible (5A1AD0) for a monster caster:
5A1ADE calls CGObjChar_EvaluateHostilityWithFirstAttackMode (vtable +0x58C,
4A98A0) with checkFirstAttack=0, which clears +0x1C6C and dispatches
CGObjMob_EvaluateHostility (5298C0). 5298C0 enforces 5299E0 (grab-pet
rejection, body status, Fear's source exclusion, bound target), RestrictionC44
and the fellow band, then returns 1 at 529909 before the first-attack
protection (Noise) check at 52994A..5299B4, followed by vtable +0x624
(529110, JobTargetRefused) at 5A1B24.
================
*/
func MonsterAreaStrikes(actor monster.Instance, candidate MonsterAreaCandidate) bool {
	pose := playerPose{Gid: candidate.Gid, OwnerGid: candidate.OwnerGid, Band: candidate.Band,
		NativeBodyStatus: candidate.NativeBodyStatus}
	if actor.JobTargetRefused(candidate.JobState) ||
		!monster.AllowsTargetStatus(actor.Ref.TidWord, actor.Nest.NativeTacticsFlags, pose.NativeBodyStatus) {
		return false
	}
	if pose.OwnerGid != 0 {
		return ordinaryCompanionHostility(actor, pose)
	}
	return ordinaryPlayerHostility(actor, pose)
}
