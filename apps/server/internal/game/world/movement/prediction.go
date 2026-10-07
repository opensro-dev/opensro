/*
===========================================================================

prediction.go - ordered movement command receipts for explicit prediction IDs

The actor movement lock spans admission and response enqueue, so the terminal
correction cannot overtake the ID-bearing receipt that admits its path.

===========================================================================
*/
package movement

import (
	"encoding/binary"
	"encoding/json"
	"sync"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

// The replacement client's explicit extension. The native movement payload is
// unchanged. IDs are strictly increasing per logical transport session; resume
// retains the high-water mark. A command is never executed twice.
/*
================
registerPredictedMovement
================
*/
func (rt *Runtime) registerPredictedMovement(hub *transport.Hub) {
	var mu sync.Mutex
	last := make(map[uint64]uint32)
	hub.OnSessionClose(func(s *transport.Session, _ error) { mu.Lock(); delete(last, s.ID); mu.Unlock() })
	hub.Handle(transport.OpPredictedMove, func(s *transport.Session, _ uint16, p []byte) {
		if len(p) < 5 || (p[0] != 1 && p[0] != 2) {
			return
		}
		id := binary.LittleEndian.Uint32(p[1:5])
		character, division, bound := enterworld.SessionCharacter(rt.deps, s)
		if !bound || id == 0 {
			return
		}
		mu.Lock()
		previous := last[s.ID]
		if id > previous {
			last[s.ID] = id
		}
		mu.Unlock()
		if id <= previous {
			return
		} // Reliable transport does not need client retransmit.
		if p[0] == 1 && rt.AdvanceResidentRegion != nil {
			rt.AdvanceResidentRegion(division, character.Name, rt.Now().UnixMilli())
		}
		unlock := rt.lockCharacter(division, character.Name)
		var result MoveOutcome
		retireEffects := false
		defer func() {
			unlock()
			if retireEffects {
				rt.RetireMoveEffects(division, character.Name, result.ServerTimeMs)
			}
		}()
		if p[0] == 2 {
			command, err := wire.DecodeCosCommand(p[5:])
			if err != nil || command.Tag != wire.CosCommandMovementTag || command.CosGid == 0 {
				result = rt.handleMoveLocked(division, character, []byte{99}, 0)
			} else {
				result = rt.handleMoveLocked(division, character, command.Movement, command.CosGid)
			}
		} else {
			result = rt.handleMoveLocked(division, character, p[5:], 0)
			// 4B0EA0: the accepted move's event retires the mover's
			// move-cancelled effects (the rider's own move only; a mount
			// move moves the vehicle).
			if result.Refusal == nil && result.Result != nil && rt.RetireMoveEffects != nil {
				retireEffects = true
			}
		}
		for _, frame := range predictionFeedback(result) {
			if s.Send(frame.Opcode, frame.Payload) != nil {
				return
			}
		}
		body, err := json.Marshal(struct {
			Version      int         `json:"v"`
			ID           uint32      `json:"id"`
			GID          uint32      `json:"gid"`
			Accepted     bool        `json:"accepted"`
			Error        string      `json:"error,omitempty"`
			ServerTimeMs int64       `json:"serverTimeMs"`
			World        interface{} `json:"world"`
		}{1, id, enterworld.ObjectIDForCharacter(character), result.Refusal == nil, predictionError(result), result.ServerTimeMs, result.Authority})
		if err == nil {
			_ = s.Send(transport.OpPredictedMoveResult, body)
		}
	})
}

// Prediction has one movement authority: the ID-bearing receipt below. Sending
// B738 as well installs an untagged path before that receipt and can rewind a
// newer click. Native callers retain their ordinary B738 response unchanged.
/*
================
predictionFeedback
================
*/
func predictionFeedback(result MoveOutcome) []wire.Frame {
	frames := make([]wire.Frame, 0, len(result.Frames))
	for _, frame := range result.Frames {
		if frame.Opcode != simulation.OpMovementAck {
			frames = append(frames, frame)
		}
	}
	return frames
}

/*
================
predictionError
================
*/
func predictionError(outcome MoveOutcome) string {
	if outcome.Refusal != nil {
		return outcome.Refusal.Reason
	}
	return ""
}
