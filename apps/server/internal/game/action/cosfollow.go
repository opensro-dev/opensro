/*
===========================================================================

cosfollow.go - returning a commanded pet to its existing follow owner

Native 6A271B checks the command class (gold/cash pet), then emits tag 9.
The command cancels outstanding target work; normal follower advancement owns
the return path and its collision checks. It never moves the player.

===========================================================================
*/
package action

import (
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
handleCosFollowCommand
================
*/
func (rt *Runtime) handleCosFollowCommand(division string, c *enterworld.Character, gid uint32) OpResult {
	snapshot, ref := rt.commandCOSSnapshot(division, c, gid)
	if snapshot == nil || snapshot.ActiveCOS.Mounted ||
		(ref.TidWord>>11 != 3 && ref.TidWord>>11 != 4) {
		return OpResult{}
	}
	rt.petMu.Lock()
	state := rt.petSessions[petOwnerKey{division, strings.ToLower(c.Name)}]
	rt.petMu.Unlock()
	if state == nil || state.character != c || state.follower == nil || state.follower.GID() != gid {
		return OpResult{}
	}
	result := OpResult{}
	if state.pickup != nil {
		result = finishPendingCosPickup(state, failureResult(wire.ErrCodeInvalidRequest))
	}
	for _, frame := range state.follower.Stop(rt.Now().UnixMilli()) {
		result.Frames = append(result.Frames, wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
	}
	return result
}
