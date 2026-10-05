/*
===========================================================================

casthalt_test.go - a cast stops its caster's walk (59B5F6)

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
startRunning

Puts c halfway along a 4 s run and records every frame pushed to it and to
its peers.
================
*/
func startRunning(t *testing.T, rt *Runtime, clock *fakeClock, c *enterworld.Character) (simulation.WorldState, *[]wire.Frame, *[]wire.Frame) {
	t.Helper()
	var own, peers []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { own = append(own, frames...) }
	rt.PushDivisionPeerFrames = func(_, _ string, frames []wire.Frame) { peers = append(peers, frames...) }
	now := clock.NowMs()
	world := rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			from := w.Spawn
			goal := from
			goal.X += 200
			w.Spawn = goal
			w.MoveSegment = &simulation.MoveSegment{From: from, StartedAtMs: now - 2000, ArrivesAtMs: now + 2000}
		})
	return world, &own, &peers
}

/*
================
findCorrection
================
*/
func findCorrection(frames []wire.Frame) (wire.ObjectSourceCorrection, bool) {
	for _, f := range frames {
		if f.Opcode == wire.OpObjectSourceCorrection {
			correction, err := wire.DecodeObjectSourceCorrection(f.Payload)
			return correction, err == nil
		}
	}
	return wire.ObjectSourceCorrection{}, false
}

/*
================
TestSelfBuffCastStopsTheRunningCaster

Guard of Ice (activity 2) pressed on the run: the walk ends at the live
point and the caster and its peers get the stop.
================
*/
func TestSelfBuffCastStopsTheRunningCaster(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_CH_COLD_GANGGI_A_01")
	if !skill.HaltsWalk() {
		t.Fatalf("Guard of Ice activity %d", skill.ActionKind)
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(enterworld.DerivedMaxMP(c))
	before, own, peers := startRunning(t, rt, clock, c)
	want := before.LiveSpawnAt(clock.NowMs())

	start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	start = assertAndSeparateActionSession(t, start)
	assertOpcodes(t, start.Frames, wire.OpSkillCastResult)

	after := rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { panic("lost world") })
	if after.MoveSegment != nil || after.Spawn != want {
		t.Fatalf("caster still walking: %+v, want %+v", after, want)
	}
	for name, frames := range map[string][]wire.Frame{"caster": *own, "peers": *peers} {
		correction, ok := findCorrection(frames)
		if !ok || correction.Gid != enterworld.ObjectIDForCharacter(c) || correction.Position.X != float32(want.X) || correction.Position.Z != float32(want.Z) {
			t.Fatalf("%s stop %+v (%v), want %+v", name, correction, ok, want)
		}
	}
}

/*
================
TestInstantImbueKeepsTheWalk

Fire Force is an instant activity: 4AD870 runs it beside the walk.
================
*/
func TestInstantImbueKeepsTheWalk(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	row := installFireImbue(t, rt, c)
	if row.HaltsWalk() {
		t.Fatal("Fire Force is not instant")
	}
	before, own, peers := startRunning(t, rt, clock, c)
	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	assertOpcodes(t, result.Frames, wire.OpSkillCastResult, simulation.OpVitalsUpdate, wire.OpSkillEffectControl, wire.OpSkillEffectControl, wire.OpAttachedEffect)
	after := rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { panic("lost world") })
	if after.MoveSegment == nil || after.Spawn != before.Spawn {
		t.Fatalf("imbue stopped the walk: %+v", after)
	}
	if _, ok := findCorrection(append(*own, *peers...)); ok {
		t.Fatal("imbue published a stop")
	}
}
