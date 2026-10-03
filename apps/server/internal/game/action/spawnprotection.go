/*
===========================================================================

spawnprotection.go - the untouchable grace after a revival

CGObjPC_TeleportToTown (4DF290), the revival arm (arg2 == 1), clears the
abnormal states, changes LIFE and motion, and then calls
CGObjChar_SetBodyModeAndScheduleRestore (4A9FC0, vtable +0x318) with body
mode 2 for 6.0 seconds (11.0 where CGameWorldMgr_CallGameWorldSlot31 marks
the world; the port runs only the field world, so 6). Body mode 2 makes the
character untouchable: monsters skip it as a target (540DE0) and attackers
are refused (5291D0), so a revived player is not killed again before they
can act. When the time is up the scheduled restore returns the body mode,
unless something else has replaced it meanwhile. The helper then adds one
HP; callers own further HP/MP recovery. Both self-rebirth and an accepted
resurrection skill use this protection owner.

===========================================================================
*/

package action

import (
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// reviveUntouchableMs is the 6.0 s float 4DF484 passes for the field world.
const reviveUntouchableMs = 6000

/*
================
bodyRestore

One scheduled restore: the character, the owner token of the body mode it
retires, and when.
================
*/
type bodyRestore struct {
	division string
	name     string
	owner    uint64
	dueMs    int64
}

/*
================
BodyRestoreQueue

The scheduled body-mode restores, one per character (a newer grant
replaces the older schedule, as a newer SetBodyMode does natively).
================
*/
type BodyRestoreQueue struct {
	mu      sync.Mutex
	pending map[string]bodyRestore
}

/*
================
NewBodyRestoreQueue
================
*/
func NewBodyRestoreQueue() *BodyRestoreQueue {
	return &BodyRestoreQueue{pending: make(map[string]bodyRestore)}
}

/*
================
schedule
================
*/
func (q *BodyRestoreQueue) schedule(restore bodyRestore) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending[selectionKey(restore.division, restore.name)] = restore
}

/*
================
due

Removes and returns the restores due at nowMs.
================
*/
func (q *BodyRestoreQueue) due(nowMs int64) []bodyRestore {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out []bodyRestore
	for key, restore := range q.pending {
		if nowMs >= restore.dueMs {
			out = append(out, restore)
			delete(q.pending, key)
		}
	}
	return out
}

/*
================
grantReviveUntouchable

Inside the revival's character door: set body mode 2 with a fresh owner
token and schedule its restore. A GM invincible or invisible mode (3, 4) is
the stronger state and is left alone. Returns the body-mode frame to
publish, or nil.
================
*/
func (rt *Runtime) grantReviveUntouchable(division string, character *enterworld.Character, nowMs int64) []wire.Frame {
	if character.NativeBodyStatus == 3 || character.NativeBodyStatus == 4 {
		return nil
	}
	owner := bodyEffectSequence.Add(1)
	if !character.TransitionBodyStatus(domain.BodyStatusTransition{Value: untouchableBodyStatus, Owner: owner}) &&
		character.BodyStatusOwner != owner {
		return nil
	}
	rt.bodyRestores.schedule(bodyRestore{
		division: division,
		name:     character.Name,
		owner:    owner,
		dueMs:    nowMs + reviveUntouchableMs,
	})
	return []wire.Frame{bodyStatusFrame(enterworld.ObjectIDForCharacter(character), untouchableBodyStatus)}
}

/*
================
advanceBodyRestores

The scheduled restores of 4A9FC0: retire each due body mode if its owner
still holds it, and publish the change to the character and its peers.
================
*/
func (rt *Runtime) advanceBodyRestores(nowMs int64) {
	for _, restore := range rt.bodyRestores.due(nowMs) {
		unlock := rt.lockDivision(restore.division)
		character := rt.findCharacter(restore.division, restore.name)
		var frames []wire.Frame
		if character != nil {
			rt.deps.Update(character, "body-mode-restore", func() bool {
				if !character.TransitionBodyStatus(domain.BodyStatusTransition{RetireOwner: restore.owner}) {
					return false
				}
				frames = append(frames, bodyStatusFrame(enterworld.ObjectIDForCharacter(character), 0))
				frames = append(frames, rt.refreshMovementEffects(restore.division, character, nowMs)...)
				return true
			})
		}
		rt.publishBodyStatus(restore.division, restore.name, frames)
		unlock()
	}
}
