/*
===========================================================================
operator_test.go - recovery preserves progress and resets location owners
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestOperatorRescuePreservesCharacterAndRetiresMovement
================
*/
func TestOperatorRescuePreservesCharacterAndRetiresMovement(t *testing.T) {
	for _, hp := range []int64{0, 100} {
		c := rebirthTestCharacter(20, hp)
		rt, clock := newTestRuntime(c, testItems())
		installMidMove(rt, c, clock)
		destination := simulation.Spawn{RegionID: 26265, X: 957, Y: -80, Z: 1508}
		rt.portals = &portalCatalog{destinations: map[uint32]portalDestination{
			2: {id: 2, ref: 2095, code: "GATE_WC", building: true, recall: true, spawn: destination},
		}}
		before := c.Snapshot()
		rt.Selected.Set(testDivision, c.Name, 99)
		if err := rt.OperatorRescue(testDivision, c.Name, 999); err == nil {
			t.Fatal("unknown destination accepted")
		}
		if !reflect.DeepEqual(before, c.Snapshot()) {
			t.Fatal("refused rescue changed character")
		}
		if err := rt.OperatorRescue(testDivision, c.Name, 2); err != nil {
			t.Fatal(err)
		}
		if got := simulation.SeedWorldState(c).Spawn; got != destination {
			t.Fatalf("spawn = %+v", got)
		}
		if c.World.MoveSegment != nil || c.World.SavedReturn.RegionID != destination.RegionID {
			t.Fatal("stale movement or return anchor")
		}
		if *c.CurrentHP != *before.CurrentHP || *c.CurrentMP != *before.CurrentMP || c.GMPrivilege != before.GMPrivilege ||
			!reflect.DeepEqual(c.MissionInventory, before.MissionInventory) || !reflect.DeepEqual(c.Gold, before.Gold) {
			t.Fatal("rescue changed vitals, privileges or property")
		}
		if _, selected := rt.Selected.Get(testDivision, c.Name); selected {
			t.Fatal("selection survived rescue")
		}
		info, err := rt.OperatorCharacter(testDivision, c.Name)
		if err != nil || info["name"] != c.Name {
			t.Fatalf("diagnostic %v %v", info, err)
		}
	}
}

/*
================
TestOperatorTownsRejectInstancesAndNonRecallDestinations
================
*/
func TestOperatorTownsRejectInstancesAndNonRecallDestinations(t *testing.T) {
	c := rebirthTestCharacter(20, 100)
	rt, _ := newTestRuntime(c, testItems())
	rt.portals = &portalCatalog{destinations: map[uint32]portalDestination{
		1: {id: 1, ref: 1, recall: true, spawn: simulation.Spawn{RegionID: 23687}},
		2: {id: 2, ref: 2, recall: true, building: true, spawn: simulation.Spawn{RegionID: 23687}},
		3: {id: 3, ref: 3, recall: false, spawn: simulation.Spawn{RegionID: 23687}},
		4: {id: 4, ref: 4, recall: true, spawn: simulation.Spawn{RegionID: 0x8001}},
	}}
	towns := rt.OperatorTowns()
	if len(towns) != 2 || towns[0].ID != 1 || towns[1].ID != 2 {
		t.Fatalf("towns %+v", towns)
	}
}

/*
================
TestOperatorRescuePersistsThroughAuthorityRestart
================
*/
func TestOperatorRescuePersistsThroughAuthorityRestart(t *testing.T) {
	seed := rebirthTestCharacter(20, 100)
	seed.ActiveCOS = &domain.CharacterCOS{GID: 1234, RefObjID: 190, Codename: "COS_C_HORSE1", CurrentHP: 400, Summoned: true, Mounted: true}
	d := openDoorRuntime(t, t.TempDir(), seed)
	deps := d.rt.deps.(*enterworld.Deps)
	deps.UpdateCharacter = d.authority.UpdateCharacter
	deps.ReadCharacter = func(_ string, read func()) { d.authority.ReadState(read) }
	destination := simulation.Spawn{RegionID: 26265, X: 957, Y: -80, Z: 1508}
	d.rt.portals = &portalCatalog{destinations: map[uint32]portalDestination{
		2: {id: 2, ref: 2095, code: "GATE_WC", building: true, recall: true, spawn: destination},
	}}
	before := d.character.Snapshot()
	if err := d.rt.OperatorRescue(testDivision, d.character.Name, 2); err != nil {
		t.Fatal(err)
	}
	if health := d.authority.Health(); health.FailedWrites != 0 {
		t.Fatalf("storage: %+v", health)
	}
	reopened := d.reboot(t)
	c := reopened.character
	if simulation.SeedWorldState(c).Spawn != destination {
		t.Fatalf("location did not survive restart: %+v", c.World)
	}
	if !reflect.DeepEqual(before.ActiveCOS, c.ActiveCOS) || !reflect.DeepEqual(before.MissionInventory, c.MissionInventory) || *before.CurrentHP != *c.CurrentHP {
		t.Fatal("rescue changed companion, property or health")
	}
}

/*
================
TestOperatorRescueRefusesPendingDeletionBeforeCleanup
================
*/
func TestOperatorRescueRefusesPendingDeletionBeforeCleanup(t *testing.T) {
	c := rebirthTestCharacter(20, 100)
	c.DeletePending = true
	rt, _ := newTestRuntime(c, testItems())
	rt.portals = &portalCatalog{destinations: map[uint32]portalDestination{
		2: {id: 2, ref: 2095, recall: true, spawn: simulation.Spawn{RegionID: 26265}},
	}}
	rt.Selected.Set(testDivision, c.Name, 99)
	before := c.Snapshot()
	if err := rt.OperatorRescue(testDivision, c.Name, 2); err == nil {
		t.Fatal("deleted character rescue accepted")
	}
	if !reflect.DeepEqual(before, c.Snapshot()) {
		t.Fatal("refusal changed character")
	}
	if _, selected := rt.Selected.Get(testDivision, c.Name); !selected {
		t.Fatal("refusal cleaned up live runtime")
	}
}

/*
================
TestOperatorTownsFromPublishedCatalog

City gates are authored teleport buildings, not dungeon destinations. Validate
real data so a fabricated catalog cannot hide an unusable production selector.
================
*/
func TestOperatorTownsFromPublishedCatalog(t *testing.T) {
	licensed.RequireGameData(t)
	rt, _ := newTestRuntime(rebirthTestCharacter(20, 100), testItems())
	if err := rt.ConfigurePortals(gamedatatest.TextdataDir(t)); err != nil {
		t.Fatal(err)
	}
	towns := rt.OperatorTowns()
	if len(towns) != 5 {
		t.Fatalf("town census: %+v", towns)
	}
	for _, town := range towns {
		if town.ID != 2 {
			continue
		}
		if town.Code != "GATE_WC" || town.Position != (simulation.Spawn{RegionID: 26265, X: 957, Y: -80, Z: 1508}) {
			t.Fatalf("Donwhang destination: %+v", town)
		}
		return
	}
	t.Fatal("Donwhang is missing")
}
