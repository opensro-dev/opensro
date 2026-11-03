/*
===========================================================================

opresult.go - what one applied operation hands back

The result vocabulary every request handler and tick step returns: the
frames for the actor, the public projection, the observed routes and the
pending approach. Runtime owns the state an operation changes; this file
owns only the shape of the answer, and the two refusal results that have
no state of their own.

===========================================================================
*/

package action

import (
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
PendingPickup

PendingPickup describes an approach still in flight. No frames ride a
duplicate request; the simulation tick owns arrival and grant completion.
==================
*/
type PendingPickup struct {
	ItemGid uint32
	Eta     time.Duration
}

/*
==================
OpResult

OpResult is one applied operation: Frames is the acting session's complete
synchronous burst and Broadcast is the public peer projection. ActorPrivate
is the actor-only tail needed when the SAME operation is produced by the
simulation tick instead of a request handler: the tick publishes Broadcast to
everyone first, then routes ActorPrivate to the actor in the same turn.
Frames already includes both portions; request handlers must never send
ActorPrivate separately. Pending marks an approach when nothing rides the
wire at all.
==================
*/
type OpResult struct {
	Recipients   []RecipientFrames
	Frames       []wire.Frame
	Broadcast    []wire.Frame
	ActorPrivate []wire.Frame
	// Around is published around other objects than the actor, each to
	// every session observing it (the recipient's own included).
	Around  []ObservedFrames
	Pending *PendingPickup

	// DiagnosticRefusal is an internal, non-wire reason for a silent native
	// refusal. The hub logs it at debug level; protocol behavior stays exact.
	DiagnosticRefusal string
}

/*
================
ObservedFrames

Public frames for the sessions that observe one object, as native
the reference server's nearby-session send sends around its argument: a
heal's effect add goes around each recipient, not around the caster.
================
*/
type ObservedFrames struct {
	SourceGID uint32
	Frames    []wire.Frame
}

/*
================
observedDivisionFrames

The Around routes of a result as tick batches, one per object.
================
*/
func observedDivisionFrames(division string, around []ObservedFrames) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for _, route := range around {
		if len(route.Frames) == 0 {
			continue
		}
		batch := simulation.DivisionFrames{DivisionID: division, SourceGID: route.SourceGID}
		for _, f := range route.Frames {
			batch.Frames = append(batch.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
		}
		out = append(out, batch)
	}
	return out
}

/*
==================
failureResult
==================
*/
func failureResult(errorCode uint8) OpResult {
	return OpResult{
		Frames: []wire.Frame{
			{
				Opcode:  wire.OpItemMoveResponse,
				Payload: wire.EncodeItemMoveError(errorCode),
			},
		},
	}
}

// ---- 0x72CD pickup ----

/*
==================
pickupRefusal
==================
*/
func pickupRefusal(errorCode uint8) OpResult {
	return OpResult{Frames: wire.PickupRefusalFrames(errorCode)}
}
