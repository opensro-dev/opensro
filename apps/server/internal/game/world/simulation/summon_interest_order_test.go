/*
===========================================================================

summon_interest_order_test.go - summon visibility order against the before-hook

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

/*
==================
TestSummonEntersShownMonstersAfterTheBeforeHook

AdvanceSummons runs in the before-hook. Scope admission runs later, inside
RunMonsterLeg. A summon created on this tick cannot be in shownMonsters
until that leg returns.
==================
*/
func TestSummonEntersShownMonstersAfterTheBeforeHook(t *testing.T) {
	registry, parent, wave, ranges, now := summonFixture(t)
	if _, ok := registry.BeginSummon("summon", parent, wave, *now, *now+500, *now+500, ranges); !ok {
		t.Fatal("summon reservation refused")
	}
	lease, ok := registry.ObjectPopulation("summon", parent.Gid)
	if !ok {
		t.Fatal("parent has no population")
	}
	ops := &MonsterMoverOps{Monsters: registry, Rand: func() float64 { return 0 }}
	ops.AttackPlan = func(monster.Instance, uint32, AttackPick) (MonsterAttackPlan, bool) {
		return MonsterAttackPlan{}, false
	}
	push := &fakePusher{}
	source := &fakeSource{sessions: []SessionSnapshot{{
		SessionID: "viewer", DivisionID: "summon", CharacterID: 1, Population: lease, CombatEligible: true,
		World: WorldState{SpawnSet: true, Spawn: Spawn{RegionID: parent.Spawn.RegionID, X: parent.Spawn.X, Y: parent.Spawn.Y, Z: parent.Spawn.Z}},
	}}}
	ticker := newTestTicker(source, push)
	ticker.Monsters = ops
	var child uint32
	seen := map[int64]bool{}
	ticker.BeforeHooks = []TickHook{func(at int64) []DivisionFrames {
		registry.AdvanceSummons(at)
		for _, actor := range registry.MaterializedInstances("summon") {
			if actor.SummonerGID == parent.Gid {
				child = actor.Gid
			}
		}
		shown := false
		if state := ticker.states["summon"]; state != nil && state.monsters != nil && child != 0 {
			shown = state.monsters.shownMonsters["viewer"][child]
		}
		seen[at] = shown
		if child == 0 {
			return nil
		}
		return []DivisionFrames{{DivisionID: "summon", SourceGID: child, Frames: []Frame{{Opcode: wire.OpEndedEffectInstances, Payload: []byte{1}}}}}
	}}
	ticker.RunTick(*now)
	ticker.RunTick(*now + 500)
	ticker.RunTick(*now + 600)
	if child == 0 {
		t.Fatal("summon was not created")
	}
	if seen[*now] || seen[*now+500] {
		t.Fatalf("child %d was shown during the creation before-hook: %v", child, seen)
	}
	if !seen[*now+600] {
		t.Fatalf("child %d was still absent from shownMonsters on the tick after scope admission: %v", child, seen)
	}
	var spawn, ended int
	for _, batch := range push.toSession {
		for _, frame := range batch.frames {
			if frame.Opcode == wire.OpSingleObjectSpawn && frame.ScopeGID == child {
				spawn++
			}
			if frame.Opcode == wire.OpEndedEffectInstances {
				ended++
			}
		}
	}
	if spawn == 0 {
		t.Fatal("scope leg never spawned the summon")
	}
	if ended != 1 {
		t.Fatalf("observer frames = %d, want the post-admission hook only", ended)
	}
}
