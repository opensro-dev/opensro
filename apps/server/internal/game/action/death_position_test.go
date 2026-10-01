package action

import (
	"reflect"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

func TestFatalMovementSettlesBeforeDelayedPresentRebirthAndReconnect(t *testing.T) {
	for _, delay := range []time.Duration{0, time.Second, time.Minute} {
		t.Run(delay.String(), func(t *testing.T) {
			rt, clock, c, mob := newCombatTestRuntime(t, 100)
			mob.Ref.DefaultSkillIDs[0] = 2
			c.CurrentHP = testInt64(1)
			skills := rt.deps.SkillData().(staticSkillSource)
			skill := skills[2]
			skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 100, 100, 100
			skills[2] = skill
			key := simulation.WorldKey(testDivision, c.Name)
			// Keep the fatal hit in actual attack reach, with a long uncompleted
			// move to the field and a non-integer deck height/facing.
			before := rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
				from := w.Spawn
				from.X, from.Y, from.Z = 960, 20.25, 458
				w.Spawn = from
				w.Spawn.X, w.Spawn.Y, w.Spawn.Angle = 1460, 30.25, 16384
				w.MoveSegment = &simulation.MoveSegment{From: from, StartedAtMs: clock.NowMs() - 50, ArrivesAtMs: clock.NowMs() + 9950}
			})
			want := before.LiveSpawnAt(clock.NowMs())
			result := rt.MonsterBasicAttack(testDivision, mob, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
			if !result.Accepted || result.TargetAlive {
				t.Fatalf("fatal attack: %+v", result)
			}
			dead := rt.Worlds.Snapshot(key, func() simulation.WorldState { panic("lost world") })
			if dead.MoveSegment != nil || dead.Spawn != want || dead.LiveSpawnAt(clock.NowMs()+60000) != want || dead.LifeRevision != before.LifeRevision+1 {
				t.Fatalf("corpse drifted: %+v, want %+v", dead, want)
			}
			// Reconnect's seed consumes the durable character, not a corpse cache.
			if persisted := simulation.SeedWorldState(c.Snapshot()); persisted.Spawn != want || persisted.MoveSegment != nil {
				t.Fatalf("persisted corpse: %+v", persisted)
			}
			clock.Advance(delay)
			reborn := rt.HandleLocalRebirth(testDivision, c, []byte{2})
			// Correction, vitals, LIFE and the untouchable body mode.
			if len(reborn.Frames) != 4 {
				t.Fatalf("rebirth: %+v", reborn)
			}
			correction, err := wire.DecodeObjectSourceCorrection(reborn.Frames[0].Payload)
			if err != nil || correction.Position.RegionID != want.RegionID || correction.Position.X != float32(want.X) || correction.Position.Y != float32(want.Y) || correction.Position.Z != float32(want.Z) || correction.Position.Heading != want.Angle {
				t.Fatalf("rebirth correction %+v/%v, want %+v", correction, err, want)
			}
		})
	}
}

type failedRebirthPreparation struct {
	Dependencies
	mutate func()
}

func (d failedRebirthPreparation) PrepareReentry(division string, c *enterworld.Character) (enterworld.PreparedReentry, bool) {
	if d.mutate != nil {
		d.mutate()
		return d.Dependencies.PrepareReentry(division, c)
	}
	return enterworld.PreparedReentry{}, false
}

func TestTownRebirthPreparationFailureDoesNotReviveOrRelocate(t *testing.T) {
	c := rebirthTestCharacter(20, 0)
	rt, _ := newTestRuntime(c, testItems())
	rt.deps = failedRebirthPreparation{Dependencies: rt.deps}
	before := c.Snapshot()
	result := rt.HandleLocalRebirth(testDivision, c, []byte{1})
	if len(result.Frames) != 0 || len(result.Broadcast) != 0 || !reflect.DeepEqual(c.Snapshot(), before) {
		t.Fatalf("failed preparation changed character: %+v", result)
	}
}

func TestTownRebirthPreparationCannotOverwriteConcurrentMutation(t *testing.T) {
	c := rebirthTestCharacter(20, 0)
	rt, _ := newTestRuntime(c, testItems())
	rt.deps = failedRebirthPreparation{Dependencies: rt.deps, mutate: func() { c.Gold = testInt64(12345) }}
	result := rt.HandleLocalRebirth(testDivision, c, []byte{1})
	if len(result.Frames) != 0 || enterworld.CharacterAlive(c) || *c.Gold != 12345 {
		t.Fatalf("stale prepared rebirth committed: %+v", result)
	}
}

func TestRebirthRefusesUnsettledCorpseInsteadOfSamplingClickTime(t *testing.T) {
	c := rebirthTestCharacter(1, 0)
	rt, _ := newTestRuntime(c, testItems())
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
		w.MoveSegment = &simulation.MoveSegment{From: w.Spawn, StartedAtMs: 1, ArrivesAtMs: 2}
	})
	if result := rt.HandleLocalRebirth(testDivision, c, []byte{2}); len(result.Frames) != 0 || enterworld.CharacterAlive(c) {
		t.Fatalf("invalid corpse was guessed: %+v", result)
	}
}
