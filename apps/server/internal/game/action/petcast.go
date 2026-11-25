/*
===========================================================================

petcast.go - companion cast identity and cancellation

The companion's BATTLE intent owns preparation. It releases the same token
through B505 and retires it through the shared finalize queue.

===========================================================================
*/
package action

import (
	"fmt"
	"sync/atomic"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
petCastPrepared

Port-only echoes are independent casts, not another release of the windup.
================
*/
func petCastPrepared(step petCombatStep) bool {
	return step.state.combat != nil && step.state.combat.castToken != 0 &&
		!(step.trained != nil && step.trained.penguinRepeat)
}

/*
================
petStrikeToken
================
*/
func (rt *Runtime) petStrikeToken(step petCombatStep) uint32 {
	if petCastPrepared(step) {
		return step.state.combat.castToken
	}
	return atomic.AddUint32(&rt.castTokenCounter, 1)
}

/*
================
petStrikeFrame

The native cast lifecycle prepares a positive cast and releases its results.
================
*/
func (rt *Runtime) petStrikeFrame(step petCombatStep, result wire.SkillCastSingleTargetResult) wire.Frame {
	if petCastPrepared(step) {
		return wire.SkillCastReleaseResultFrame(result)
	}
	return wire.SkillCastSingleTargetResultFrame(result)
}

/*
================
cancelPetCombat

A prepared action closes once, even when its target or owner disappears.
The division operation lock protects the intent; the queue owns delivery.
================
*/
func (rt *Runtime) cancelPetCombat(key petOwnerKey, state *petSession, now int64) {
	if state == nil || state.combat == nil {
		return
	}
	rt.closePetCast(key, state.combat, now)
	state.combat = nil
	// Leaving BATTLE is a state change.
	resetPetStuck(state)
}

/*
================
pausePetCombat

A movement-blocking status (freeze, sleep, root, stun) holds the fight,
it does not end it: the prepared cast is interrupted, but the BATTLE
intent stays, so the pet takes its target up again when the status
expires, with no new order from the owner (#4214). The reference
server's abnormal block ends its records on its own clock and never
touches the creature's battle target; the port's intent is the same
record. A pet that is gone, dead or mounted still leaves BATTLE
through cancelPetCombat.
================
*/
func (rt *Runtime) pausePetCombat(key petOwnerKey, state *petSession, now int64) {
	if state == nil || state.combat == nil {
		return
	}
	rt.closePetCast(key, state.combat, now)
}

/*
================
closePetCast

Closes the intent's prepared cast once, if one is open, and clears it
from the intent: the one finalize both leaving BATTLE and a paused
fight send.
================
*/
func (rt *Runtime) closePetCast(key petOwnerKey, intent *petCombatIntent, now int64) {
	if intent.castToken == 0 {
		return
	}
	rt.queueSkillFinalize(key.division, fmt.Sprintf("@pet:%d", key.gid), key.gid, now, wire.SkillCastFinalizeFrame(intent.castToken))
	intent.skillID, intent.castToken, intent.releaseAtMs = 0, 0, 0
}
