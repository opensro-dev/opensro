/*
===========================================================================

groundwalk_test.go - real geometry stepping and fenced terminal delivery

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestGroundRuntimePublishesAcceptedArrivalAndCollision
================
*/
func TestGroundRuntimePublishesAcceptedArrivalAndCollision(t *testing.T) {
	for _, tc := range []struct {
		name        string
		destination int16
		blocked     bool
	}{{"open terrain", 60, false}, {"blocked terrain", 190, true}} {
		t.Run(tc.name, func(t *testing.T) {
			character := clipTestCharacter()
			rt := testRuntime(character)
			validator := NewWaterValidator(syntheticHeightRoot(t))
			rt.PathGuard = &PathGuard{Mode: PathGuardEnforce, Validator: validator}
			rt.ClientClip = &ClientClip{Mode: ClipApply, Validator: validator}
			rt.Nav = validator
			rt.EnableGroundWalk()
			pickupClears, combatClears := 0, 0
			blockedCalls := 0
			rt.GroundBlocked = func(division, name string, revision uint64) {
				if division != "0" || name != character.Name || revision != directionWorld(rt, character).GroundRevision() {
					t.Fatal("wrong blocked approach identity")
				}
				blockedCalls++
			}
			rt.ClearPendingPickup = func(string, string) { pickupClears++ }
			rt.ClearCombatIntent = func(string, string) { combatClears++ }
			outcome := rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, tc.destination, 0, 110))
			if outcome.Refusal != nil {
				t.Fatalf("admission: %v", outcome.Refusal)
			}
			pickupClears, combatClears = 0, 0
			if outcome.Authority.Spawn.X != float64(tc.destination) || *character.World.Spawn.X != 30 {
				t.Fatalf("intent admission persisted unchecked goal: %+v", outcome.Authority)
			}
			var terminal []simulation.DivisionFrames
			var previous float64 = 30
			for tick := int64(1); tick <= 100; tick++ {
				setDirectionClock(rt, testStartMs+tick*100)
				state := directionWorld(rt, character)
				pose := state.PersistedSpawn()
				if pose.X < previous || pose.X > float64(tc.destination) {
					t.Fatalf("invalid accepted advance %v -> %+v", previous, pose)
				}
				previous = pose.X
				frames := rt.GroundTickHook()(testStartMs + tick*100)
				if len(frames) > 0 {
					terminal = frames
					break
				}
				if !state.GroundActive() {
					t.Fatal("arrival lost terminal notification")
				}
				if *character.World.Spawn.X != 30 {
					t.Fatal("per-step runtime progress dirtied persisted character")
				}
			}
			state := directionWorld(rt, character)
			if state.GroundActive() || len(terminal) != 1 || len(terminal[0].Frames) != 1 {
				t.Fatalf("terminal state %+v frames=%+v", state, terminal)
			}
			if tc.blocked && state.Spawn.X >= 80 || !tc.blocked && state.Spawn.X != float64(tc.destination) {
				t.Fatalf("wrong accepted rest %+v", state.Spawn)
			}
			if *character.World.Spawn.X != state.Spawn.X {
				t.Fatal("terminal accepted pose not persisted")
			}
			wantClears := 0
			if tc.blocked {
				wantClears = 1
			}
			if blockedCalls != wantClears || pickupClears != 0 || combatClears != 0 {
				t.Fatalf("terminal action clears %d/%d want %d", pickupClears, combatClears, wantClears)
			}
			frame := terminal[0].Frames[0]
			if frame.Current == nil || !frame.Current() {
				t.Fatal("current terminal correction fenced out")
			}
			if len(rt.GroundTickHook()(testStartMs+10000)) != 0 {
				t.Fatal("duplicate terminal delivery")
			}
			setDirectionClock(rt, testStartMs+11000)
			next := rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, 40, 0, 110))
			if next.Refusal != nil || frame.Current() {
				t.Fatal("new input retained obsolete terminal correction")
			}
		})
	}
}

/*
================
TestGroundDirectionContinuesAfterStalledLegArrival
================
*/
func TestGroundDirectionContinuesAfterStalledLegArrival(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	rt.EnableGroundWalk()
	outcome := rt.HandleMove("0", character, encodeTurnBody(simulation.AngularFlagGo, headingEast))
	if outcome.Refusal != nil {
		t.Fatal(outcome.Refusal)
	}
	first := directionWorld(rt, character)
	for tick := int64(1); tick <= 20; tick++ {
		setDirectionClock(rt, testStartMs+tick*30000)
		directionWorld(rt, character)
	}
	if frames := rt.GroundTickHook()(testStartMs + 600000); len(frames) != 0 {
		t.Fatalf("continuous angular walk emitted stop: %+v", frames)
	}
	rt.DirectionTickHook()(testStartMs + 600000)
	next := directionWorld(rt, character)
	if !next.GroundActive() || next.Spawn == first.Spawn {
		t.Fatalf("continuous direction stopped at artificial leg endpoint: %+v", next)
	}
}

/*
================
TestGroundCollisionStatusStopsAtUnchangedEndpoint
================
*/
func TestGroundCollisionStatusStopsAtUnchangedEndpoint(t *testing.T) {
	from := spawnAt(0x6B4F, 30, 110)
	to := spawnAt(0x6B4F, 35, 110)
	rt := &Runtime{ClientClip: &ClientClip{Mode: ClipApply, Validator: &fakeClipValidator{report: ClipReport{Outcome: ClipBlocked, Rest: to, NativeResult: 1}}}}
	accepted, _, blocked := rt.groundStep(from, simulation.NavOwner{}, to)
	if accepted != to || !blocked {
		t.Fatalf("native stop status lost when endpoint unchanged: %+v blocked=%v", accepted, blocked)
	}
}

/*
================
TestGroundAdmissionRejectsDisconnectedDungeonSpaces
================
*/
func TestGroundAdmissionRejectsDisconnectedDungeonSpaces(t *testing.T) {
	rt := testRuntime(testCharacter())
	rt.EnableGroundWalk()
	from := simulation.Spawn{RegionID: 0x8001, X: 10, Y: 2, Z: 20}
	for _, region := range []uint16{0x8002, 0x6B4F} {
		to := from
		to.RegionID = region
		to.X = 100
		accepted, _, refusal := rt.AdmitGroundWalk("Walker", from, simulation.NavOwner{}, to)
		if refusal == nil || accepted != from {
			t.Fatalf("disconnected ground goal admitted: region=%x pose=%+v refusal=%v", region, accepted, refusal)
		}
	}
	to := from
	to.X = 100
	if accepted, _, refusal := rt.AdmitGroundWalk("Walker", from, simulation.NavOwner{}, to); refusal != nil || accepted != to {
		t.Fatalf("same-room intent refused: pose=%+v refusal=%v", accepted, refusal)
	}
}
