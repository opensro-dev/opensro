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
petStrikeToken
================
*/
func (rt *Runtime) petStrikeToken(step petCombatStep) uint32 {
	if intent := step.state.combat; intent != nil && intent.castToken != 0 {
		return intent.castToken
	}
	return atomic.AddUint32(&rt.castTokenCounter, 1)
}

/*
================
petStrikeFrame

58356C / 585BF8 prepare a positive cast; 585F69 releases its results.
================
*/
func (rt *Runtime) petStrikeFrame(step petCombatStep, result wire.SkillCastSingleTargetResult) wire.Frame {
	if intent := step.state.combat; intent != nil && intent.castToken != 0 {
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
	if token := state.combat.castToken; token != 0 {
		rt.queueSkillFinalize(key.division, fmt.Sprintf("@pet:%d", key.gid), key.gid, now, wire.SkillCastFinalizeFrame(token))
	}
	state.combat = nil
}
