/*
===========================================================================

fortress_tax.go - manager tax collection and its committed balance reply

NPC service admission is shared with the other manager operations. The
fortress authority and store own period, membership and atomic payment.

===========================================================================
*/
package action

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
)

/*
================
fortressTaxCollect

6234D5 updates gold before acknowledging collection. v1.150's 754A40 reads
the actual collected i64, not the remaining treasury, after [action, 1].
================
*/
func (rt *Runtime) fortressTaxCollect(division string, c *enterworld.Character, request siege.Interaction) OpResult {
	if rt.Fortresses == nil {
		return fortressRefusal(request.Action, fortressErrUnknown)
	}
	amount, code, err := rt.Fortresses.CollectTax(division, request.Fortress, c.ID, request.Gold)
	if err != nil {
		log.WithError(err).Error("fortress tax collection")
		code = fortressErrUnknown
	}
	if code != 0 {
		return fortressRefusal(request.Action, code)
	}
	var frames []wire.Frame
	// 4E4B60 returns without a point update for a zero credit. Tax completion
	// still acknowledges success, including a negative request clamped to zero.
	if amount != 0 {
		frames = append(frames, goldFrame(rt.characterSnapshot(division, c)))
	}
	frames = append(frames, wire.Frame{Opcode: opFortressInteractionResult,
		Payload: wire.NewWriter(10).U8(request.Action).U8(1).U64(uint64(amount)).Payload()})
	return OpResult{Frames: frames}
}
