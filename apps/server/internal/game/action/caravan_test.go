/*
===========================================================================

caravan_test.go - a loaded transport draws bandits on a battlefield

Drives the caravan owner through the runtime: registration by summoning a
transport with goods, the native timer, the town refusal, and thief
bandits around the vehicle once it stands in a battlefield region.

===========================================================================
*/
package action

import (
	"fmt"
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// caravanBattlefield is a CHINA battlefield region (23196) of zone 0.
const caravanBattlefield = uint16(23196)

/*
================
caravanFixture

The combat fixture with thief references in its catalog, the COS item
source and a trader whose transport carries Chinese goods.
================
*/
func caravanFixture(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character) {
	t.Helper()
	rt, clock, c, target := newCombatTestRuntime(t, 100)
	refs := map[uint32]monster.MonsterRef{target.Ref.RefObjID: target.Ref}
	for level := uint8(1); level <= 40; level++ {
		id := 40000 + uint32(level)
		refs[id] = monster.MonsterRef{RefObjID: id, Codename: fmt.Sprintf("MOB_THIEF_NPC_%04d", level),
			TidWord: 0x00c6, TypeID4: 2, Level: level, MaxHP: 200, BodyRadius: 5, WalkSpeed: 16, RunSpeed: 50}
	}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(refs, []monster.NestRow{{
		SpawnPoint: target.Spawn, RetailEvidence: true, MaxCount: 1,
	}}))
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	deps := rt.deps.(*enterworld.Deps)
	deps.Items = testCosSource(deps.Items.(staticItemSource))
	// Level 20 is the cargo floor; a basis of 2784 prices the death EXP at 3480.
	levels := testCombatRewardLevels()
	levels.gold = map[int64]int64{20: 2784}
	deps.Levels = levels
	rt.CaravanRoll = func() (uint32, error) { return 50, nil }
	c.Job.Type = domain.JobTrader
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100,
		Summoned: true, Mounted: true, Container: &domain.COSContainer{Capacity: 40, Rows: []domain.InventoryRow{{
			Slot: 0, RefObjID: 9001, Codename: "ITEM_ETC_TRADE_CH_01",
			TypeFlags: wire.PackTypeFlags(3, 3, 8, 2), StackCount: 20,
		}}}}
	rt.BindPetSession(testDivision, c, 1)
	if err := rt.admitPopulationSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	return rt, clock, c
}

/*
================
rideCaravanTo

The trader rides the transport, so the vehicle stands where the rider does.
================
*/
func rideCaravanTo(rt *Runtime, c *enterworld.Character, pose simulation.Spawn) {
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState {
		return simulation.SeedWorldState(c)
	}, func(w *simulation.WorldState) { w.Spawn = pose })
}

/*
================
caravanBandits
================
*/
func caravanBandits(rt *Runtime, region uint16) []monster.Instance {
	var bandits []monster.Instance
	for _, instance := range rt.Monsters.InstancesInRegions(testDivision, []uint16{region}) {
		if instance.ThiefMonster() {
			bandits = append(bandits, instance)
		}
	}
	return bandits
}

/*
================
TestLoadedTransportDrawsThievesOnlyOnABattlefield
================
*/
func TestLoadedTransportDrawsThievesOnlyOnABattlefield(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	town := rt.cosLiveSpawn(testDivision, c, clock.Now().UnixMilli())
	rt.rememberTransportCOS(testDivision, c, town)
	if rt.caravans.Len() != 1 {
		t.Fatal("a loaded transport did not register a caravan")
	}
	rt.advanceCaravans(clock.Now().UnixMilli())
	// 60BC80: a roll of 50 draws float32(50/32767) x 60000 + 60000 ms.
	clock.Advance(60091 * time.Millisecond)
	rt.advanceCaravans(clock.Now().UnixMilli())
	if len(caravanBandits(rt, town.RegionID)) != 0 || rt.caravans.Len() != 1 {
		t.Fatal("a caravan in town drew bandits or was dropped")
	}

	field := simulation.Spawn{RegionID: caravanBattlefield, X: 960, Y: 20, Z: 960}
	rideCaravanTo(rt, c, field)
	clock.Advance(60091 * time.Millisecond)
	rt.advanceCaravans(clock.Now().UnixMilli())
	bandits := caravanBandits(rt, caravanBattlefield)
	if len(bandits) == 0 {
		t.Fatal("a loaded caravan on a battlefield drew no bandits")
	}
	for _, bandit := range bandits {
		// Even parity draws the last row: a trader's thieves use 2004.
		if bandit.Nest.NativeTacticsFlags != 542 || !bandit.NestDetached || bandit.Rarity() != 0 {
			t.Fatalf("bandit %+v", bandit)
		}
		if distance := math.Hypot(bandit.Spawn.X-field.X, bandit.Spawn.Z-field.Z); distance > 150 {
			t.Fatalf("bandit %.1f from the vehicle", distance)
		}
	}
}

/*
================
TestEmptiedTransportLeavesTheCaravanRegistry

60BD40 drops a caravan whose vehicle no longer carries goods.
================
*/
func TestEmptiedTransportLeavesTheCaravanRegistry(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	field := simulation.Spawn{RegionID: caravanBattlefield, X: 960, Y: 20, Z: 960}
	rideCaravanTo(rt, c, field)
	rt.rememberTransportCOS(testDivision, c, field)
	rt.advanceCaravans(clock.Now().UnixMilli())
	c.ActiveCOS.Container.Rows = nil
	clock.Advance(60091 * time.Millisecond)
	rt.advanceCaravans(clock.Now().UnixMilli())
	if rt.caravans.Len() != 0 || len(caravanBandits(rt, caravanBattlefield)) != 0 {
		t.Fatal("an emptied transport kept its caravan or drew bandits")
	}
}
