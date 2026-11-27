/*
===========================================================================

followtrace_test.go - the Trace numbers the sweep already keeps (U8-18)

The original's follow handler gives up beyond 1,000 (a 3D distance),
re-aims every 100 ms, and switches a walking tracer to run. The port
keeps all three (followMaximumDistance and the shared 3D relative
vector, the 100 ms coordination tick with its 100 ms resteer floor, and
commitIntentMovement's run switch); these tests pin the three at the
follow lane so a change to any one of them fails by name.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestFollowGivesUpBeyond1000In3D
================
*/
func TestFollowGivesUpBeyond1000In3D(t *testing.T) {
	rt, _, actor, target := followFixture(t)
	key := simulation.WorldKey(testDivision, target.Name)
	seed := func() simulation.WorldState { return simulation.SeedWorldState(target) }
	// The fixture stands the target 300 across. 1,000 up makes about
	// 1,044 in space: the original's 1,000 is the spatial distance, so
	// this trace is refused where a plane-only reading (300) admits it.
	rt.Worlds.Update(key, seed, func(w *simulation.WorldState) { w.Spawn.Y += 1000 })
	result := rt.HandleTargetInteract(testDivision, actor, wire.FollowTarget{TargetGid: enterworld.ObjectIDForCharacter(target)}.Encode())
	if len(result.Frames) != 0 || len(rt.combatIntentSnapshot()) != 0 {
		t.Fatalf("trace beyond 1,000 (3D) admitted: %+v", result)
	}

	// 900 up is about 949 in space: the trace begins.
	rt.Worlds.Update(key, seed, func(w *simulation.WorldState) { w.Spawn.Y -= 100 })
	result = rt.HandleTargetInteract(testDivision, actor, wire.FollowTarget{TargetGid: enterworld.ObjectIDForCharacter(target)}.Encode())
	if result.DiagnosticRefusal != "" || len(rt.combatIntentSnapshot()) != 1 {
		t.Fatalf("trace inside 1,000 (3D) refused: %+v", result)
	}
}

/*
================
TestFollowSwitchesAWalkingTracerToRun
================
*/
func TestFollowSwitchesAWalkingTracerToRun(t *testing.T) {
	rt, _, actor, target := followFixture(t)
	key := simulation.WorldKey(testDivision, actor.Name)
	seed := func() simulation.WorldState { return simulation.SeedWorldState(actor) }
	rt.Worlds.Update(key, seed, func(w *simulation.WorldState) { w.MovementMode = simulation.WalkMode })

	result := rt.HandleTargetInteract(testDivision, actor, wire.FollowTarget{TargetGid: enterworld.ObjectIDForCharacter(target)}.Encode())
	if result.DiagnosticRefusal != "" || len(result.Frames) < 2 {
		t.Fatalf("follow admission = %+v, want the ack and the run switch", result)
	}
	if result.Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("first frame = %#x, want the movement ack", result.Frames[0].Opcode)
	}
	switchFrame := result.Frames[1]
	if switchFrame.Opcode != wire.OpObjectStateRefresh {
		t.Fatalf("second frame = %#x, want the movement state push", switchFrame.Opcode)
	}
	push, err := wire.DecodeObjectStateRefresh(switchFrame.Payload)
	if err != nil || push.StateType != wire.StateChannelMove || push.Value != simulation.RunMode {
		t.Fatalf("state push = %+v (%v), want the run value on the move channel", push, err)
	}
	world := rt.Worlds.Snapshot(key, seed)
	if world.MovementMode != simulation.RunMode {
		t.Fatalf("tracer mode = %d, want run", world.MovementMode)
	}
}

/*
================
TestFollowReaimsNoSoonerThan100Ms
================
*/
func TestFollowReaimsNoSoonerThan100Ms(t *testing.T) {
	rt, clock, actor, target := followFixture(t)
	rt.HandleTargetInteract(testDivision, actor, wire.FollowTarget{TargetGid: enterworld.ObjectIDForCharacter(target)}.Encode())
	targetKey := simulation.WorldKey(testDivision, target.Name)
	seed := func() simulation.WorldState { return simulation.SeedWorldState(target) }

	// The target steps sideways 50 ms after the leg was issued: too
	// young to re-aim, so the tick stays silent.
	rt.Worlds.Update(targetKey, seed, func(w *simulation.WorldState) { w.Spawn.Z += 120 })
	clock.now = clock.now.Add(50 * time.Millisecond)
	if frames := rt.advanceBasicAttackIntents(clock.NowMs(), nil); len(frames) != 0 {
		t.Fatalf("re-aim before 100 ms published %+v", frames)
	}

	// At 100 ms the same geometry re-aims: a fresh movement ack.
	clock.now = clock.now.Add(50 * time.Millisecond)
	frames := rt.advanceBasicAttackIntents(clock.NowMs(), nil)
	if len(frames) != 1 || len(frames[0].Frames) == 0 || frames[0].Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("re-aim at 100 ms = %+v, want one movement ack", frames)
	}
}
