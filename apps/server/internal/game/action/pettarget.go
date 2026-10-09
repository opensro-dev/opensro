/*
===========================================================================

pettarget.go - companion combat resolves player-owned victims

Guild soldiers use the same creature HP/status door as monster attacks on
companions. Their target identity remains separate from its owning player.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
petCombatTarget
================
*/
type petCombatTarget struct {
	combatTarget
	cos    *enterworld.CharacterCOS
	cosRef *enterworld.CharacterRef
}

/*
================
petAttackBodyAllowed

528F5D..528FA5 rejects a companion in body mode 2 and player victims in
modes 2 through 4 before consulting the owner's attack permission. Read
the companion's own mode: owner transitions do not always propagate to it.
================
*/
func petAttackBodyAllowed(pet *enterworld.CharacterCOS, target combatTarget) bool {
	if pet.NativeBodyStatus == untouchableBodyStatus {
		return false
	}
	return target.snapshot == nil || target.snapshot.NativeBodyStatus < mercenaryBodyProtectedFirst ||
		target.snapshot.NativeBodyStatus > mercenaryBodyProtectedLast
}

/*
================
resolvePetCombatTarget
================
*/
func (rt *Runtime) resolvePetCombatTarget(step petCombatStep, gid uint32) (petCombatTarget, bool) {
	if step.pet.NativeBodyStatus == untouchableBodyStatus {
		return petCombatTarget{}, false
	}
	target, found := rt.resolveCombatTarget(step.key.division, step.snapshot, gid, step.nowMs)
	if found {
		if !petAttackBodyAllowed(step.pet, target) {
			return petCombatTarget{}, false
		}
		if target.snapshot != nil {
			if step.ref.TidWord>>11 == domain.MercenaryBand &&
				(!mercenaryBodyVisible(target.snapshot.NativeBodyStatus) || !rt.worldPlayerEnemy(step.key.division, step.snapshot, target.snapshot)) {
				return petCombatTarget{}, false
			}
			if ride := ridingCOS(target.snapshot); ride != 0 {
				pet := target.snapshot.CompanionByGID(ride)
				ref, valid := rt.cosReference(pet)
				if !valid {
					return petCombatTarget{}, false
				}
				target.gid = ride
				return petCombatTarget{combatTarget: target, cos: pet, cosRef: ref}, true
			}
		}
		return petCombatTarget{combatTarget: target}, true
	}
	owner := rt.characterByCosGID(step.key.division, gid)
	snapshot := rt.characterSnapshot(step.key.division, owner)
	if snapshot == nil || snapshot.ID == step.snapshot.ID || snapshot.DeletePending ||
		domain.CharacterWorldInstance(snapshot) != domain.CharacterWorldInstance(step.snapshot) {
		return petCombatTarget{}, false
	}
	if step.ref.TidWord>>11 == domain.MercenaryBand && !rt.worldPlayerEnemy(step.key.division, step.snapshot, snapshot) {
		return petCombatTarget{}, false
	}
	pet := snapshot.CompanionByGID(gid)
	if pet == nil || !pet.Summoned || pet.Mounted || pet.CurrentHP == 0 || !mercenaryBodyVisible(pet.NativeBodyStatus) {
		return petCombatTarget{}, false
	}
	ref, found := rt.cosReference(pet)
	if !found || ref.TidWord>>11 == 4 {
		return petCombatTarget{}, false
	}
	at := rt.companionLiveSpawn(step.key.division, snapshot, pet, step.nowMs)
	return petCombatTarget{combatTarget: combatTarget{gid: gid, player: owner, snapshot: snapshot, at: at}, cos: pet, cosRef: ref}, true
}
