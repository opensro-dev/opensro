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
================
*/
func (rt *Runtime) retireGroundApproaches() {
	rt.groundApproachStops.Range(func(key, value any) bool {
		stop := value.(groundApproachStop)
		unlock := rt.lockDivision(stop.division)
		defer unlock()
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
