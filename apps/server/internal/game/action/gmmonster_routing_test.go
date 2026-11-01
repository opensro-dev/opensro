/*
===========================================================================

gmmonster_routing_test.go - monster commands through the production ticker

The victim and the credited GM can have different observers. A new /ZOE
corpse can also be created after visibility ran but before action hooks.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
gmMonsterPush
================
*/
type gmMonsterPush map[string][]simulation.Frame

/*
================
PushToSession
================
*/
func (p gmMonsterPush) PushToSession(id string, frames []simulation.Frame) {
	p[id] = append(p[id], frames...)
}

/*
================
PushToDivision
================
*/
func (p gmMonsterPush) PushToDivision(string, []simulation.Frame, string) {}

/*
================
gmMonsterTicker
================
*/
func gmMonsterTicker(t *testing.T, f *mobKillFixture) (*simulation.Ticker, gmMonsterPush) {
	t.Helper()
	lease, ok := f.rt.EntryPopulationLease(testDivision, f.gm.Name)
	if !ok {
		t.Fatal("missing GM admission")
	}
	row, _ := f.rt.Monsters.Get(testDivision, f.gid)
	// The combat unit fixture stands in Jangan. The production monster leg
	// correctly removes monsters there; use the battlefield west of town.
	point := simulation.Spawn{RegionID: 0x60a8, X: row.Spawn.X, Y: row.Spawn.Y, Z: row.Spawn.Z}
	if !f.rt.Monsters.Defeat(testDivision, f.gid, f.clock.Now()) {
		t.Fatal("could not retire the town fixture")
	}
	gids := f.rt.Monsters.SpawnGMMonstersTracked(simulation.GMMonsterSpawn{
		Division: testDivision, Population: lease, RefObjID: row.Ref.RefObjID, Count: 1,
		Position: point, NowMs: f.clock.NowMs(),
	})
	if len(gids) != 1 {
		t.Fatal("could not create the field fixture")
	}
	f.gid = gids[0]
	gmPoint := point
	gmPoint.X = 0
	*f.gm.World.Spawn.X = 0
	*f.gm.World.Spawn.RegionID = int64(point.RegionID)
	f.rt.Worlds.Update(simulation.WorldKey(testDivision, f.gm.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(f.gm) },
		func(w *simulation.WorldState) { w.Spawn, w.SpawnSet = gmPoint, true })
	viewers := []simulation.SessionSnapshot{
		{SessionID: "gm", DivisionID: testDivision, CharacterID: f.gm.ID, Population: lease,
			World: simulation.WorldState{SpawnSet: true, Spawn: gmPoint}},
		{SessionID: "monster-viewer", DivisionID: testDivision, CharacterID: f.gm.ID + 1, Population: lease,
			World: simulation.WorldState{SpawnSet: true, Spawn: point}},
	}
	push := gmMonsterPush{}
	ticker := simulation.NewTicker(tickSessionSource{sessions: viewers}, push)
	ticker.Now = f.clock.Now
	ticker.Monsters = &simulation.MonsterMoverOps{Monsters: f.rt.Monsters, Rand: func() float64 { return 0 }}
	ticker.Hooks = []simulation.NamedTickHook{{Name: "action", Hook: f.rt.TickHook()}}
	return ticker, push
}

/*
================
TestGMMobKillPublishesDeathToVictimObservers
================
*/
func TestGMMobKillPublishesDeathToVictimObservers(t *testing.T) {
	f := newMobKillFixture(t)
	f.rt.UpdateExperience = func(c *enterworld.Character, _, _ int64, _ uint32) ([]wire.Frame, bool) {
		return []wire.Frame{
			{Opcode: wire.OpLevelUpEffect, Payload: binary.LittleEndian.AppendUint32(nil, enterworld.ObjectIDForCharacter(c))},
			{Opcode: wire.OpExpUpdate, Payload: []byte{1}},
		}, true
	}
	ticker, push := gmMonsterTicker(t, f)
	ticker.RunTick(f.clock.NowMs())
	if row, ok := f.rt.Monsters.Get(testDivision, f.gid); !ok || row.CurrentHP == 0 {
		t.Fatal("field fixture was not alive after initial tick")
	}
	clear(push)
	if !f.rt.KillGMMonster(testDivision, f.gm.Name, f.gid, true) {
		t.Fatal("kill refused")
	}
	ticker.RunTick(f.clock.NowMs() + 1)
	for id, wantDeath := range map[string]bool{"gm": false, "monster-viewer": true} {
		death, level := false, false
		for _, frame := range push[id] {
			if frame.Opcode == wire.OpObjectStateRefresh && len(frame.Payload) >= 6 &&
				binary.LittleEndian.Uint32(frame.Payload) == f.gid && frame.Payload[4] == wire.StateChannelLife {
				death = true
			}
			level = level || frame.Opcode == wire.OpLevelUpEffect
		}
		if death != wantDeath || level == wantDeath {
			t.Fatalf("%s: death=%v level=%v, want death=%v level=%v", id, death, level, wantDeath, !wantDeath)
		}
	}
}

/*
================
TestGMZoeCreatedAfterScopeIsPublishedDeadOnNextTick

A request can arrive between the scope phase and hook drainage. Unknown GIDs
must not receive LIFE before their create; the next scope snapshot is dead.
================
*/
func TestGMZoeCreatedAfterScopeIsPublishedDeadOnNextTick(t *testing.T) {
	f := newMobKillFixture(t)
	installSmallGoldRef(f.rt)
	f.rt.DropRoll = goldOnlyMonsterDropRoll(0, 0)
	ticker, push := gmMonsterTicker(t, f)
	created := false
	ticker.Hooks = []simulation.NamedTickHook{
		{Hook: func(int64) []simulation.DivisionFrames {
			if !created {
				created = true
				if !f.rt.KillGMZoe(testDivision, f.gm.Name, 1933, 1) {
					t.Fatal("zoe refused")
				}
			}
			return nil
		}},
		{Name: "action", Hook: f.rt.TickHook()},
	}
	ticker.RunTick(f.clock.NowMs())
	drops := f.rt.Ground.All(testDivision)
	if len(drops) != 1 {
		t.Fatalf("expected one deterministic loot drop, got %d", len(drops))
	}
	countDrop := func(frames []simulation.Frame) int {
		count := 0
		for _, frame := range frames {
			if frame.Opcode == wire.OpSingleObjectSpawn && len(frame.Payload) >= 4 &&
				binary.LittleEndian.Uint32(frame.Payload) == drops[0].RefObjID {
				row, err := wire.DecodeGroundItemRow(frame.Payload, drops[0].TypeFlags, true)
				if err != nil {
					t.Fatal(err)
				}
				if row.Gid == drops[0].Gid {
					count++
				}
			}
		}
		return count
	}
	dropCreates := countDrop(push["gm"])
	var corpse uint32
	for _, row := range f.rt.Monsters.InstancesInRegions(testDivision, []uint16{uint16(*f.gm.World.Spawn.RegionID)}) {
		if row.Gid != f.gid && row.CurrentHP == 0 {
			corpse = row.Gid
		}
	}
	if corpse == 0 {
		t.Fatal("zoe corpse absent")
	}
	for _, frame := range push["gm"] {
		if frame.Opcode == wire.OpObjectStateRefresh && len(frame.Payload) >= 4 && binary.LittleEndian.Uint32(frame.Payload) == corpse {
			t.Fatal("LIFE arrived before the corpse was in scope")
		}
	}
	clear(push)
	ticker.RunTick(f.clock.NowMs() + 1)
	dropCreates += countDrop(push["gm"])
	if dropCreates != 1 {
		t.Fatalf("GM received %d creates for the deterministic drop, want exactly one", dropCreates)
	}
	found := false
	for _, frame := range push["gm"] {
		if frame.Opcode != wire.OpSingleObjectSpawn || len(frame.Payload) < 8 {
			continue
		}
		if binary.LittleEndian.Uint32(frame.Payload[4:]) != corpse {
			continue
		}
		found = true
		row, ok := f.rt.Monsters.Get(testDivision, corpse)
		if !ok || row.CurrentHP != 0 {
			t.Fatal("scope presented a living zoe victim")
		}
		def := simulation.MonsterWireDefFromInstance(row, f.clock.NowMs()+1)
		if def.LifeState != wire.LifeStateDead {
			t.Fatal("corpse snapshot does not encode LIFE-dead")
		}
		mover, _ := f.rt.Monsters.Mover(testDivision, corpse)
		pose := mover.LivePoseAt(f.clock.NowMs()+1, nil)
		want := simulation.BuildMonsterSpawnSingle(def, corpse, simulation.Spawn{
			RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z, Angle: pose.Heading,
		})
		if !bytes.Equal(frame.Payload, want) {
			t.Fatal("delivered corpse snapshot differs from the dead authoritative row")
		}
	}
	if !found {
		t.Fatal("next scope pass did not publish the zoe corpse")
	}
}
