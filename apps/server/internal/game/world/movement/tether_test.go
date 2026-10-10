/*
===========================================================================

tether_test.go - a trader stops at the edge of its transport's range

The ground walk refuses a step that leads further from a parked trade
transport once the trader stands past 1000 units, answers 0x342F [1] to
the trader and stops it with the usual B2F5 correction. Walking back toward
the transport stays free.

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
walkUntilGroundTerminal

Ticks the ground walk until it publishes its terminal frames.
================
*/
func walkUntilGroundTerminal(t *testing.T, rt *Runtime, character *enterworld.Character, fromMs int64) ([]simulation.DivisionFrames, int64) {
	t.Helper()
	for now := fromMs + 100; now <= fromMs+60000; now += 100 {
		setDirectionClock(rt, now)
		// Reading the world plane runs its ground steps.
		directionWorld(rt, character)
		if frames := rt.GroundTickHook()(now); len(frames) > 0 {
			return frames, now
		}
	}
	t.Fatal("the walk never ended")
	return nil, 0
}

/*
================
TestTraderStopsPastTheTransportRange
================
*/
func TestTraderStopsPastTheTransportRange(t *testing.T) {
	character := clipTestCharacter()
	rt := testRuntime(character)
	rt.EnableGroundWalk()
	// The transport stands 995 units west of the trader.
	anchor := simulation.Spawn{RegionID: 0x6B4F, X: 30 - 995, Z: 110}
	key := simulation.WorldKey("0", character.Name)
	rt.Worlds.ReplaceTethers(map[string]simulation.Tether{key: {Anchor: anchor,
		Range: simulation.TradeTransportTetherRange, Reason: simulation.TetherTradeTransport}})

	if outcome := rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, 400, 0, 110)); outcome.Refusal != nil {
		t.Fatalf("admission: %v", outcome.Refusal)
	}
	terminal, now := walkUntilGroundTerminal(t, rt, character, testStartMs)
	rest := directionWorld(rt, character).Spawn
	if d := simulation.WorldDistance2D(rest, anchor); d <= simulation.TradeTransportTetherRange || d > simulation.TradeTransportTetherRange+160 {
		t.Fatalf("the trader stopped %.1f from the transport at %+v, want one native step past 1000", d, rest)
	}
	if len(terminal) != 2 {
		t.Fatalf("terminal frames %+v, want the notice and the stop", terminal)
	}
	notice := terminal[0]
	if notice.OnlyCharacterID != character.ID || len(notice.Frames) != 1 || notice.Frames[0].Opcode != wire.OpCosDistanceError ||
		len(notice.Frames[0].Payload) != 1 || notice.Frames[0].Payload[0] != simulation.TetherTradeTransport {
		t.Fatalf("notice %+v, want 0x342F [1] to the trader only", notice)
	}
	if stop := terminal[1].Frames; len(stop) != 1 || stop[0].Opcode != wire.OpObjectSourceCorrection {
		t.Fatalf("stop %+v, want the B2F5 correction after the notice", terminal[1])
	}

	// Back toward the transport walks to its goal without a notice.
	if outcome := rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, 0, 0, 110)); outcome.Refusal != nil {
		t.Fatalf("walking back: %v", outcome.Refusal)
	}
	terminal, _ = walkUntilGroundTerminal(t, rt, character, now)
	if len(terminal) != 1 || terminal[0].Frames[0].Opcode != wire.OpObjectSourceCorrection {
		t.Fatalf("walking back ended with %+v, want only the arrival", terminal)
	}
	if rest := directionWorld(rt, character).Spawn; rest.X != 0 {
		t.Fatalf("walking back stopped at %+v, want its goal", rest)
	}
}

/*
================
TestUntetheredTraderWalksAway
================
*/
func TestUntetheredTraderWalksAway(t *testing.T) {
	character := clipTestCharacter()
	rt := testRuntime(character)
	rt.EnableGroundWalk()
	anchor := simulation.Spawn{RegionID: 0x6B4F, X: 30 - 995, Z: 110}
	key := simulation.WorldKey("0", character.Name)
	rt.Worlds.ReplaceTethers(map[string]simulation.Tether{key: {Anchor: anchor,
		Range: simulation.TradeTransportTetherRange, Reason: simulation.TetherTradeTransport}})
	// The next tick's set no longer holds the trader: the transport was
	// mounted, sent home or died.
	rt.Worlds.ReplaceTethers(nil)
	if outcome := rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, 400, 0, 110)); outcome.Refusal != nil {
		t.Fatalf("admission: %v", outcome.Refusal)
	}
	terminal, _ := walkUntilGroundTerminal(t, rt, character, testStartMs)
	if len(terminal) != 1 {
		t.Fatalf("frames %+v, want only the arrival", terminal)
	}
	if rest := directionWorld(rt, character).Spawn; rest.X != 400 {
		t.Fatalf("an untethered trader stopped at %+v", rest)
	}
}
