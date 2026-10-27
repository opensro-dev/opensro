/*
===========================================================================

groundapproach.go - retire only the action whose ground approach collided

Movement queues terminal revisions without taking the action lock. The action
tick retires matching commands and enqueues their release before admitting a
later command, preserving both lock order and the native command-count lifetime.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
groundApproachStop
================
*/
type groundApproachStop struct {
	division, name string
	revision       uint64
}

// maxGroundApproachLaneRun bounds how many stops one division-lane hold
// retires before the lane is released and re-taken (#1820 D): the cached
// lane keeps a backed-up drain from re-queuing behind request work, and
// the cap keeps the worst-case lane hold at a run of stops (~200 ns each
// on the plain path) instead of the whole backed-up queue.
const maxGroundApproachLaneRun = 64

/*
================
RetireGroundApproach

Called under the movement owner lock. Coalescing retains the latest terminal
motion revision; only the current action can still own that revision.
================
*/
func (rt *Runtime) RetireGroundApproach(divisionID, characterName string, revision uint64) {
	if revision != 0 {
		rt.groundApproachStops.Store(simulation.WorldKey(divisionID, characterName), groundApproachStop{divisionID, characterName, revision})
	}
}

/*
================
retireGroundApproaches

The division lane is cached across consecutive stops of one division
(#1820 D): after a stalled tick the queue can hold thousands of stops,
and re-taking the lane (and the maintenance read gate inside it) for each
one multiplied both the lock churn and the chances to be descheduled
mid-drain. Range order is untouched, so stops retire in exactly the old
sequence; only the redundant lane acquisitions between neighbours of
the same division are gone. The run is capped
(maxGroundApproachLaneRun) so request work in the division waits at
most one bounded run, never the whole queue.
================
*/
func (rt *Runtime) retireGroundApproaches() {
	division := ""
	run := 0
	var unlock func()
	release := func() {
		if unlock != nil {
			unlock()
			unlock = nil
		}
	}
	defer release()
	rt.groundApproachStops.Range(func(key, value any) bool {
		stop := value.(groundApproachStop)
		if stop.division != division || run >= maxGroundApproachLaneRun {
			release()
			unlock = rt.lockDivision(stop.division)
			division = stop.division
			run = 0
		}
		run++
		// Forget and a newer terminal event may replace the queued owner while
		// this drain waits for the action door. Never consume their replacement.
		if !rt.groundApproachStops.CompareAndDelete(key, value) {
			return true
		}
		pickup := rt.Pending.ClearGroundRevision(grounditem.PendingKey(stop.division, stop.name), stop.revision)
		rt.basicAttackIntentsMu.Lock()
		intent, ok := rt.basicAttackIntents[key.(string)]
		combat := ok && intent.HasApproach && intent.ApproachMovementRevision == stop.revision
		if combat {
			delete(rt.basicAttackIntents, key.(string))
		}
		rt.basicAttackIntentsMu.Unlock()
		var frames []wire.Frame
		if pickup {
			// Inference: retiring the blocked pickup completes its command. The
			// native 75BAA0 count consumer needs the same release as other endings.
			state := wire.ReleaseActionState()
			state.State = rt.actionQueueCount(stop.division, stop.name)
			frames = append(frames, wire.Frame{Opcode: wire.OpActionState, Payload: state.Encode()})
		}
		if combat {
			if value, exists := rt.actionSessions.Load(key); exists {
				if frame := rt.retireActionSession(value.(actionSessionPublication)); frame != nil {
					frames = append(frames, *frame)
				}
			}
		}
		if len(frames) > 0 && rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(stop.division, stop.name, frames)
		}
		return true
	})
}
