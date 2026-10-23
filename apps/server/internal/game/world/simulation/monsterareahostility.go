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

The native monster-caster hostility chain: the character hostility
evaluation runs with checkFirstAttack=0, dispatching to the monster
evaluation, which enforces the source exclusion (grab-pet rejection,
body status, Fear's source exclusion, bound target), the restriction
check and the fellow band, then returns before the first-attack
protection (Noise) check, followed by the job-target refusal.
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
