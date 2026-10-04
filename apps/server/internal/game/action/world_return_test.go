package action

import (
	"encoding/json"
	"fmt"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

func TestReturnLocationUsesLiveRegionBinding(t *testing.T) {
	rt, c := newActiveEffectTestRuntime(t, staticSkillSource{})
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	key := simulation.WorldKey(testDivision, c.Name)
	start := simulation.Spawn{RegionID: 0x5c5c, X: 1800, Y: 20, Z: 100}
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) { w.Spawn = start })
	if err := rt.AdmitCharacterSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	initial := *c.World.SavedReturn
	goal := start
	goal.RegionID++
	goal.X = 300
	rt.Worlds.Update(key, func() simulation.WorldState { panic("unexpected seed") }, func(w *simulation.WorldState) {
		w.Spawn = goal
		w.MoveSegment = &simulation.MoveSegment{From: start, StartedAtMs: 1000, ArrivesAtMs: 2000}
	})
	rt.advanceResidentRegions(1100)
	if *c.World.SavedReturn != initial {
		t.Fatal("future goal or same-region motion overwrote return")
	}
	rt.advanceResidentRegions(1500)
	saved := *c.World.SavedReturn
	if saved.RegionID != goal.RegionID || saved.X == float32(goal.X) || saved.Definition != 1 {
		t.Fatalf("did not capture live crossing: %+v", saved)
	}
	rt.advanceResidentRegions(2000)
	if *c.World.SavedReturn != saved {
		t.Fatal("arrival within bound region overwrote saved crossing")
	}
	snapshot := c.Snapshot()
	c.World.SavedReturn.X++
	if *snapshot.World.SavedReturn != saved {
		t.Fatal("return aliases character snapshot")
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var restored domain.Character
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if *restored.World.SavedReturn != saved {
		t.Fatal("return did not persist")
	}
}

func TestReturnLocationDefinitionAndLayerGate(t *testing.T) {
	for _, id := range []instance.ID{instance.Pack(2, 1), instance.Pack(10, 2), instance.Pack(10, 1)} {
		t.Run(fmt.Sprintf("%d/%d", id.Definition(), id.Layer()), func(t *testing.T) {
			rt, c := newActiveEffectTestRuntime(t, staticSkillSource{})
			rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
			packed := uint32(id)
			old := domain.SavedReturnLocation{Definition: 1, RegionID: 99, X: 123}
			c.World = &domain.CharacterWorld{PackedInstance: &packed, SavedReturn: &old}
			// The shard opens type-0 worlds (INS_FORT_JA) at boot; others on demand.
			rt.Monsters.StartDivision(testDivision)
			if _, open := rt.Monsters.PopulationLease(testDivision, id); !open {
				if _, status := rt.Monsters.AllocatePopulation(testDivision, id); status != instance.Success {
					t.Fatal(status)
				}
			}
			if err := rt.AdmitCharacterSession(testDivision, c.Name, 1); err != nil {
				t.Fatal(err)
			}
			if id == instance.Pack(2, 1) {
				if c.World.SavedReturn.Definition != 2 {
					t.Fatal("type-0 fortress omitted")
				}
			} else if *c.World.SavedReturn != old {
				t.Fatal("ineligible world overwrote return")
			}
		})
	}
}

func TestReturnLocationRejectsRetiredPopulation(t *testing.T) {
	rt, c := newActiveEffectTestRuntime(t, staticSkillSource{})
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	id := instance.Pack(2, 1)
	packed := uint32(id)
	c.World = &domain.CharacterWorld{PackedInstance: &packed}
	rt.Monsters.StartDivision(testDivision)
	lease, open := rt.Monsters.PopulationLease(testDivision, id)
	if !open {
		t.Fatal("the shard did not open the fortress world at boot")
	}
	if err := rt.AdmitCharacterSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	saved := *c.World.SavedReturn
	if !rt.Monsters.ReleasePopulation(testDivision, lease) {
		t.Fatal("release")
	}
	if replacement, status := rt.Monsters.AllocatePopulation(testDivision, id); status != instance.Success || replacement == lease {
		t.Fatal("replacement")
	}
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { panic("seed") }, func(w *simulation.WorldState) { w.Spawn.RegionID++; w.Spawn.X = 999 })
	rt.AdvanceResidentRegion(testDivision, c.Name, rt.Now().UnixMilli())
	if *c.World.SavedReturn != saved {
		t.Fatal("stale resident saved a replacement population's location")
	}
}
