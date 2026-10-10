/*
===========================================================================

follow_test.go - Trace command geometry, lifecycle and combat isolation

Exercise the real target dispatcher and continuation loop with two admitted
players. No combat damage, pickup reply or invented follow opcode is allowed.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
followFixture
================
*/
func followFixture(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, *enterworld.Character) {
	t.Helper()
	rt, clock, actor, _ := newCombatTestRuntime(t, 100)
	target := *actor
	target.ID, target.Name = 4, "follow-mate"
	world, spawn := *actor.World, *actor.World.Spawn
	world.Spawn = &spawn
	target.World = &world
	deps := rt.deps.(*enterworld.Deps)
	fixtureCharacters(deps.Characters)[testDivision] = append(
		fixtureCharacters(deps.Characters)[testDivision], &target,
	)
	for _, c := range []*enterworld.Character{actor, &target} {
		if err := rt.AdmitCharacterSession(testDivision, c.Name, uint64(c.ID)); err != nil {
			t.Fatal(err)
		}
	}
	rt.Worlds.Update(simulation.WorldKey(testDivision, target.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(&target) },
		func(w *simulation.WorldState) { w.Spawn.X += 300 },
	)
	return rt, clock, actor, &target
}

/*
================
TestFollowPursuesHoldsAndResumesWithoutCombat
================
*/
func TestFollowPursuesHoldsAndResumesWithoutCombat(t *testing.T) {
	rt, clock, actor, target := followFixture(t)
	result := rt.HandleTargetInteract(testDivision, actor, wire.FollowTarget{TargetGid: enterworld.ObjectIDForCharacter(target)}.Encode())
	if result.DiagnosticRefusal != "" || len(result.Frames) == 0 || result.Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("follow admission = %+v", result)
	}
	intents := rt.combatIntentSnapshot()
	if len(intents) != 1 || !intents[0].FollowTarget || intents[0].SkillID != 0 || rt.hasOpenSkillCast(testDivision, actor.Name) {
		t.Fatalf("follow entered combat: %+v", intents)
	}
	key := simulation.WorldKey(testDivision, actor.Name)
	seed := func() simulation.WorldState { return simulation.SeedWorldState(actor) }
	world := rt.Worlds.Snapshot(key, seed)
	to := rt.liveSpawn(simulation.WorldKey(testDivision, target.Name), target, clock.NowMs())
	if got := simulation.WorldDistance2D(world.Spawn, to); got != 58 {
		t.Fatalf("stand-off = %v, want two radii + 50", got)
	}
	clock.now = time.UnixMilli(world.MoveSegment.ArrivesAtMs + 1)
	if frames := rt.advanceBasicAttackIntents(clock.NowMs(), nil); len(frames) != 0 || len(rt.combatIntentSnapshot()) != 1 {
		t.Fatal("in-range follow did not hold silently", frames)
	}
	rt.Worlds.Update(simulation.WorldKey(testDivision, target.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(target) },
		func(w *simulation.WorldState) { w.Spawn.X += 200 },
	)
	frames := rt.advanceBasicAttackIntents(clock.NowMs(), nil)
	if len(frames) != 1 || len(frames[0].Frames) == 0 || frames[0].Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatal("target movement did not resume follow", frames)
	}
}

/*
================
TestFollowAdmitsARider

A rider may Trace: 4ACD7A refuses only attack and skill commands while
mounted. The ridden pursuit moves the player's own world state.
================
*/
func TestFollowAdmitsARider(t *testing.T) {
	rt, clock, actor, target := followFixture(t)
	gid, _ := enterworld.CosObjectIDForCharacter(actor)
	actor.ActiveCOS = &enterworld.CharacterCOS{GID: gid, CurrentHP: 100, Summoned: true, Mounted: true}
	result := rt.HandleTargetInteract(testDivision, actor, wire.FollowTarget{TargetGid: enterworld.ObjectIDForCharacter(target)}.Encode())
	if result.DiagnosticRefusal != "" || len(result.Frames) == 0 || result.Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("mounted follow admission = %+v", result)
	}
	intents := rt.combatIntentSnapshot()
	if len(intents) != 1 || !intents[0].FollowTarget {
		t.Fatalf("mounted follow intents = %+v", intents)
	}
	world := rt.Worlds.Snapshot(simulation.WorldKey(testDivision, actor.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(actor) })
	if world.MoveSegment.ArrivesAtMs <= clock.NowMs() {
		t.Fatalf("rider did not start the pursuit: %+v", world.MoveSegment)
	}
}

/*
================
TestFollowInvalidationRetiresTheCommand
================
*/
func TestFollowInvalidationRetiresTheCommand(t *testing.T) {
	for _, reason := range []string{"cancel", "manual move", "death", "target death", "target logout", "target reconnect", "target teleport", "distance", "world transition"} {
		t.Run(reason, func(t *testing.T) {
			rt, clock, actor, target := followFixture(t)
			rt.HandleTargetInteract(testDivision, actor, wire.FollowTarget{TargetGid: enterworld.ObjectIDForCharacter(target)}.Encode())
			switch reason {
			case "cancel":
				rt.HandleTargetInteract(testDivision, actor, wire.TargetInteract{Cancel: true}.Encode())
			case "manual move":
				// The production movement composition calls this same cancellation
				// hook before committing a new manual destination.
				rt.ClearCombatIntent(testDivision, actor.Name)
			case "death":
				actor.CurrentHP = testInt64(0)
			case "target death":
				target.CurrentHP = testInt64(0)
			case "target logout":
				rt.ForgetCharacterSession(testDivision, target.Name, uint64(target.ID))
			case "target reconnect":
				if err := rt.AdmitCharacterSession(testDivision, target.Name, uint64(target.ID)+1); err != nil {
					t.Fatal(err)
				}
			case "target teleport":
				target.NativeTeleportMode = 1
			case "distance", "world transition":
				rt.Worlds.Update(simulation.WorldKey(testDivision, target.Name),
					func() simulation.WorldState { return simulation.SeedWorldState(target) },
					func(w *simulation.WorldState) {
						if reason == "distance" {
							w.Spawn.X += 1100
						} else {
							w.Spawn.RegionID = 0x8001
						}
					},
				)
			}
			rt.advanceBasicAttackIntents(clock.NowMs()+100, nil)
			if len(rt.combatIntentSnapshot()) != 0 || rt.hasOpenSkillCast(testDivision, actor.Name) {
				t.Fatal("invalidated follow survived or became a cast")
			}
		})
	}
}

/*
================
TestFollowCancellationAndPickupSettleMovement

Retiring the command alone is insufficient: a near pickup or explicit
cancel must also stop the movement leg that pursuit already committed.
================
*/
func TestFollowCancellationAndPickupSettleMovement(t *testing.T) {
	for _, pickup := range []bool{false, true} {
		rt, clock, actor, target := followFixture(t)
		rt.HandleTargetInteract(testDivision, actor, wire.FollowTarget{TargetGid: enterworld.ObjectIDForCharacter(target)}.Encode())
		clock.now = clock.now.Add(100 * time.Millisecond)
		key := simulation.WorldKey(testDivision, actor.Name)
		live := rt.liveSpawn(key, actor, clock.NowMs())
		request := wire.TargetInteract{Cancel: true}
		if pickup {
			item := rt.Ground.Add(testDivision, grounditem.Item{GoldAmount: 1, Position: grounditem.Point{
				RegionID: live.RegionID, X: float32(live.X), Z: float32(live.Z),
			}})
			request = wire.TargetInteract{Gid: item.Gid}
		}
		result := rt.HandleTargetInteract(testDivision, actor, request.Encode())
		world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(actor) })
		if world.MoveSegment.Valid() || len(rt.combatIntentSnapshot()) != 0 {
			t.Fatalf("pickup=%v retained pursuit: %+v", pickup, world)
		}
		if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpObjectSourceCorrection {
			t.Fatalf("pickup=%v did not publish stop: %+v", pickup, result)
		}
	}
}

/*
================
TestFollowRejectsNonPlayersOfflineAndMalformedTargets
================
*/
func TestFollowRejectsNonPlayersOfflineAndMalformedTargets(t *testing.T) {
	rt, _, actor, target := followFixture(t)
	rt.ForgetCharacterSession(testDivision, target.Name, uint64(target.ID))
	for _, payload := range [][]byte{
		wire.FollowTarget{TargetGid: enterworld.ObjectIDForCharacter(target)}.Encode(),
		wire.FollowTarget{TargetGid: enterworld.ObjectIDForCharacter(actor)}.Encode(),
		wire.FollowTarget{TargetGid: 400001}.Encode(),
		{1, 3},
	} {
		result := rt.HandleTargetInteract(testDivision, actor, payload)
		if len(result.Frames) != 0 || len(result.Broadcast) != 0 || len(rt.combatIntentSnapshot()) != 0 {
			t.Fatalf("invalid follow % X changed state: %+v", payload, result)
		}
	}
}

/*
================
TestFollowNativeDistanceBands
================
*/
func TestFollowNativeDistanceBands(t *testing.T) {
	spacing := simulation.CombatSpacing{ActorBodyRadius: 4, TargetBodyRadius: 4, ActionReach: followInnerGap}
	from := simulation.Spawn{RegionID: 0x62a8, X: 100, Y: 20, Z: 100}
	for _, row := range []struct {
		distance float64
		moving   bool
		x        float64
	}{
		{0, false, 100}, {3, true, 92}, {4, false, 100}, {58, false, 100}, {88, false, 100}, {89, true, 131},
	} {
		target := from
		target.X += row.distance
		goal, moving := followGoal(from, target, spacing)
		if moving != row.moving || goal.X != row.x {
			t.Fatalf("distance %v: goal %+v, moving %v", row.distance, goal, moving)
		}
	}
}
