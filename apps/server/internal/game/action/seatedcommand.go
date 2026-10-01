/*
===========================================================================

seatedcommand.go - an attack or skill pressed while seated

The v1.150 client stands a seated player instead of attacking
(sub_692CB0 -> CICPlayer_ToggleSitStand) but composes a skill press
without looking at the posture (CGInterface_ExecuteSelectedActionAtTarget
6FCD50). The v1.188 server then casts from the seat: no command-path gate
reads motion 4 outside msch mode 1 and ao/pw (58DED2, 58E0BF), and a
normal cast never changes the motion, so the caster keeps the seated
x8 recovery (4A9D00) through the cast and the next sit toggle stands
instead of sitting.

This server treats every seated press the way the client treats the
attack click: the player stands and the command is dropped, as moving
(4B11B6) or being struck (593AE8) already stand the player natively.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
standOutcome

What a stand request met: no seat, a sit/stand transition still running
(the request is dropped), or a seat the player rose from.
==================
*/
type standOutcome uint8

const (
	standNotSeated standOutcome = iota
	standInTransition
	standStood
)

/*
==================
standUp

The one stand mechanic every caller shares (a seated press, a landed hit):
a settled seat stands, and the stand reaches the player and every observer
as the 0x30BF move-channel push the sit toggle sends.
==================
*/
func (rt *Runtime) standUp(division string, c *enterworld.Character, nowMs int64) (wire.Frame, standOutcome) {
	if rt.Worlds == nil {
		return wire.Frame{}, standNotSeated
	}
	key := simulation.WorldKey(division, c.Name)
	seed := func() simulation.WorldState { return simulation.SeedWorldState(c) }
	world := rt.Worlds.Snapshot(key, seed)
	if !world.Sitting && nowMs >= world.PostureTransitionUntilMs {
		return wire.Frame{}, standNotSeated
	}
	stood := false
	rt.Worlds.Update(key, seed, func(w *simulation.WorldState) {
		stood = w.StandUp(nowMs)
	})
	if !stood {
		return wire.Frame{}, standInTransition
	}
	return wire.Frame{
		Opcode: wire.OpObjectStateRefresh,
		Payload: wire.ObjectStateRefresh{
			Gid:       enterworld.ObjectIDForCharacter(c),
			StateType: wire.StateChannelMove,
			Value:     wire.MoveStateStand,
		}.Encode(),
	}, standStood
}

/*
==================
standForSeatedCommand

Reports whether the press met a seat. A settled seat stands; a press during
the sit/stand transition is dropped.
==================
*/
func (rt *Runtime) standForSeatedCommand(division string, c *enterworld.Character, nowMs int64) (OpResult, bool) {
	push, outcome := rt.standUp(division, c, nowMs)
	switch outcome {
	case standNotSeated:
		return OpResult{}, false
	case standInTransition:
		return OpResult{DiagnosticRefusal: "posture-transition"}, true
	}
	return OpResult{Frames: []wire.Frame{push}, Broadcast: []wire.Frame{push}}, true
}
