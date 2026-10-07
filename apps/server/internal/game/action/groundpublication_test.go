/*
===========================================================================

groundpublication_test.go - delayed approach paths cannot undo terminal stops

The real reliable writer checks the committed ground revision at delivery.
Pickup retirement releases its command under the action door before retry.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
	"time"
)

/*
================
groundPublicationFixture
================
*/
type groundPublicationFixture struct {
	runtime   *Runtime
	clock     *fakeClock
	character *enterworld.Character
	result    OpResult
	blocked   *bool
	item      uint32
}

/*
================
makeGroundPublication
================
*/
func makeGroundPublication(t *testing.T, kind string) groundPublicationFixture {
	t.Helper()
	c := testCharacter()
	rt, clock := newTestRuntime(c, testItems())
	blocked := false
	rt.Worlds.ConfigureGroundWalk(simulation.GroundWalkConfig{Now: clock.NowMs, Step: func(from simulation.Spawn, owner simulation.NavOwner, to simulation.Spawn) (simulation.Spawn, simulation.NavOwner, bool) {
		if blocked {
			return from, owner, true
		}
		return to, owner, false
	}})
	rt.AdmitGroundWalk = func(_ string, _ simulation.Spawn, _ simulation.NavOwner, to simulation.Spawn) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
		return to, simulation.NavWalk{}, nil
	}
	from := simulation.SeedWorldState(c).Spawn
	goal := from
	goal.X += 100
	f := groundPublicationFixture{runtime: rt, clock: clock, character: c, blocked: &blocked}
	if kind == "pickup" {
		heap := rt.Ground.Add(testDivision, PlanGoldDrop(GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2}, 1000, goal, "someone", clock.Now()))
		f.item = heap.Gid
		f.result = rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: heap.Gid}.Encode())
	} else {
		f.result = rt.commitIntentMovement(c, c.Snapshot(), intentMovement{intent: basicAttackIntent{DivisionID: testDivision, CharacterName: c.Name, TargetGid: 101, FollowTarget: kind == "follow"}, from: from, target: goal, goal: goal, nowMs: clock.NowMs()})
	}
	if len(f.result.Frames) == 0 {
		t.Fatal("approach did not publish")
	}
	return f
}

/*
================
TestGroundApproachDelayedPublicationCannotRestartRetiredPath
================
*/
func TestGroundApproachDelayedPublicationCannotRestartRetiredPath(t *testing.T) {
	for _, kind := range []string{"combat", "follow", "pickup"} {
		for _, terminal := range []string{"collision", "new input"} {
			t.Run(kind+"/"+terminal, func(t *testing.T) {
				f := makeGroundPublication(t, kind)
				rt, c, clock := f.runtime, f.character, f.clock
				key := simulation.WorldKey(testDivision, c.Name)
				seed := func() simulation.WorldState { return simulation.SeedWorldState(c) }
				initial := rt.Worlds.Snapshot(key, seed)
				clock.Advance(100 * time.Millisecond)
				rt.Worlds.Snapshot(key, seed)
				rt.Worlds.Update(key, seed, func(w *simulation.WorldState) { w.UpdateMovementSpeeds(20, 100, clock.NowMs()) })
				for _, frame := range f.result.Frames {
					if frame.Current == nil || !frame.Current() {
						t.Fatal("progress or speed retime erased initiating path")
					}
				}
				server := wireStartServer(t, rt)
				client := wireConnect(t, server, testDivision, c.Name)
				session, _ := server.Hub.Session(client.sessionID)
				SendFrames(session, f.result.Frames)
				for _, frame := range f.result.Frames {
					client.expectFrame(t, frame.Opcode, frame.Payload)
				}
				if terminal == "collision" {
					*f.blocked = true
					clock.Advance(100 * time.Millisecond)
					rt.Worlds.Snapshot(key, seed)
				} else {
					rt.Worlds.Update(key, seed, func(w *simulation.WorldState) {
						from := w.LiveSpawnAt(clock.NowMs())
						goal := from
						goal.Z += 80
						w.Spawn = goal
						w.MoveSegment = w.TravelSegment(from, goal, w.MovementMode, clock.NowMs())
					})
				}
				for _, frame := range f.result.Frames {
					if frame.Current() {
						t.Fatal("retired path still deliverable")
					}
				}
				state := rt.Worlds.Snapshot(key, seed)
				stop := sourceCorrectionFrame(c, state.PersistedSpawn())
				SendFrames(session, []wire.Frame{stop})
				marker := wire.Frame{Opcode: wire.OpItemMoveResponse, Payload: []byte{2, wire.ErrCodeInvalidRequest}}
				SendFrames(session, append(append([]wire.Frame{}, f.result.Frames...), marker))
				client.expectFrame(t, stop.Opcode, stop.Payload)
				client.expectFrame(t, marker.Opcode, marker.Payload)
				if terminal == "collision" && kind == "pickup" {
					published := 0
					rt.PushCharacterFrames = func(division, name string, frames []wire.Frame) {
						lane := rt.operations.lane(division, &rt.maintenance)
						if lane.TryLock() {
							lane.Unlock()
							t.Fatal("release escaped action publication door")
						}
						published++
						SendFrames(session, frames)
					}
					rt.RetireGroundApproach(testDivision, c.Name, initial.GroundRevision())
					rt.retireGroundApproaches()
					if published != 1 {
						t.Fatalf("terminal releases=%d", published)
					}
					client.expectFrame(t, wire.OpActionState, wire.ReleaseActionState().Encode())
					retry := rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: f.item}.Encode())
					if retry.Pending == nil || len(retry.Frames) != 2 {
						t.Fatalf("same-item retry remained latched: %+v", retry)
					}
				}
			})
		}
	}
}
