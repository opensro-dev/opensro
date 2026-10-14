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
	if !found {
		return petCombatTarget{}, false
	}
	if target.cos != nil {
		// A pet never targets a ridden COS: its rider is struck and the ride
		// takes the hit (ridingCOS below).
		if target.cos.Mounted {
			return petCombatTarget{}, false
		}
		// 529304..529324 checks the COS owner's body as well as the victim's.
		// Recheck here so protection acquired during preparation cancels the cast.
		if !petAttackBodyAllowed(step.pet, combatTarget{snapshot: target.snapshot}) {
			return petCombatTarget{}, false
		}
		if step.ref.TidWord>>11 == domain.MercenaryBand && !rt.worldPlayerEnemy(step.key.division, step.snapshot, target.snapshot) {
			return petCombatTarget{}, false
		}
		cos, ref := target.cos, target.cosRef
		target.cos, target.cosRef = nil, nil
		return petCombatTarget{combatTarget: target, cos: cos, cosRef: ref}, true
	}
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
