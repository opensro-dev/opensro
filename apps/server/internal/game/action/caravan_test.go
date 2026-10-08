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
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/caravan"
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
		for family, prefix := range []string{"MOB_THIEF", "MOB_EU_THIEF", "MOB_HUNTER", "MOB_EU_HUNTER"} {
			id := 40000 + uint32(family)*100 + uint32(level)
			refs[id] = monster.MonsterRef{RefObjID: id, Codename: fmt.Sprintf("%s_NPC_%04d", prefix, level),
				TidWord: 0x00c6, TypeID4: uint8(2 + family/2), Level: level, MaxHP: 200,
				BodyRadius: 5, WalkSpeed: 16, RunSpeed: 50}
		}
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
TestCaravanUsesTraderContinentAndJobFamily

60BF30 passes the trader to 60BDD0, while placing each bandit around the
vehicle. Both imported tactics families must reach the population owner.
================
*/
func TestCaravanUsesTraderContinentAndJobFamily(t *testing.T) {
	for _, job := range []uint8{domain.JobTrader, domain.JobThief, domain.JobHunter} {
		for _, western := range []bool{false, true} {
			rt, clock, c := caravanFixture(t)
			c.Job.Type = job
			vehicle := simulation.Spawn{RegionID: caravanBattlefield, X: 960, Y: 20, Z: 960}
			player := vehicle
			prefix := "MOB_"
			if western {
				player.RegionID = 24900
				prefix += "EU_"
			}
			flags := uint32(542)
			if job == domain.JobTrader {
				prefix += "THIEF_"
			} else {
				prefix += "HUNTER_"
				flags = 666
			}
			rideCaravanTo(rt, c, player)
			if err := rt.spawnCaravanBandits(testDivision, c, vehicle, clock.Now().UnixMilli()); err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, actor := range rt.Monsters.InstancesInRegions(testDivision, []uint16{vehicle.RegionID}) {
				if !actor.ThiefMonster() && !actor.HunterMonster() {
					continue
				}
				count++
				if !strings.HasPrefix(actor.Ref.Codename, prefix) || actor.Nest.NativeTacticsFlags != flags {
					t.Fatalf("job %d west %v: wrong bandit %+v", job, western, actor)
				}
			}
			if count == 0 {
				t.Fatalf("job %d west %v spawned no bandits", job, western)
			}
		}
	}
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

/*
================
TestCaravanRejectsMissingContinentEvidence

An unknown reference region cannot silently borrow CHINA's bandit table.
================
*/
func TestCaravanRejectsMissingContinentEvidence(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	field := simulation.Spawn{RegionID: 1, X: 960, Y: 20, Z: 960}
	rideCaravanTo(rt, c, field)
	if err := rt.spawnCaravanBandits(testDivision, c, field, clock.Now().UnixMilli()); err == nil {
		t.Fatal("unknown region silently selected a bandit family")
	}
	if len(caravanBandits(rt, field.RegionID)) != 0 {
		t.Fatal("unknown region spawned Chinese bandits")
	}
}

/*
================
TestCaravanReferenceDrawFailureDoesNotSpawn

The reference selection draw is fallible in the port. A failed draw must
stop before heading, rarity and population mutation, not choose bucket zero.
================
*/
func TestCaravanReferenceDrawFailureDoesNotSpawn(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	field := simulation.Spawn{RegionID: caravanBattlefield, X: 960, Y: 20, Z: 960}
	rideCaravanTo(rt, c, field)
	want := errors.New("reference entropy unavailable")
	draws := 0
	rt.CaravanRoll = func() (uint32, error) {
		draws++
		// Two count draws, tactics parity, two level draws, then reference.
		if draws == 6 {
			return 0, want
		}
		return 50, nil
	}
	if err := rt.spawnCaravanBandits(testDivision, c, field, clock.Now().UnixMilli()); !errors.Is(err, want) {
		t.Fatalf("draw failure = %v, want %v", err, want)
	}
	if draws != 6 || len(caravanBandits(rt, field.RegionID)) != 0 {
		t.Fatalf("failed reference draw continued: draws=%d", draws)
	}
}

/*
================
TestThiefCannotRobALevelOneCaravan

52B760: a dressed thief is refused against a dressed trader carrying a
level-1 caravan (0x3024), and that trader against the thief (0x3006);
an empty transport protects nobody.
================
*/
func TestThiefCannotRobALevelOneCaravan(t *testing.T) {
	rt, _, trader := caravanFixture(t)
	if tier := caravan.DifficultyTier(rt.caravanCargoValue(trader)); tier > 1 {
		t.Fatalf("fixture cargo is tier %d, want a level-1 caravan", tier)
	}
	thief := *trader
	thief.ID, thief.Name, thief.ActiveCOS = trader.ID+1, "thief", nil
	items := rt.deps.ItemReferences().(cosTestItemSource).staticItemSource
	for i, c := range []*enterworld.Character{trader, &thief} {
		suit := &enterworld.ItemRef{RefObjID: uint32(9301 + i), Codename: []string{"ITEM_CH_M_TRADE_TRADER_05", "ITEM_CH_M_TRADE_THIEF_05"}[i],
			Country: 3, TypeIDs: [4]int64{3, 1, 7, int64(1 + i)}, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, Combat: &enterworld.ItemCombatRef{}}
		items[suit.Codename] = suit
		c.MissionInventory = append(append([]enterworld.InventoryRow(nil), c.MissionInventory...), enterworld.InventoryRow{
			Slot: int64(jobSuitSlot), RefObjID: suit.RefObjID, Codename: suit.Codename, TypeFlags: suit.TypeFlags(), StackCount: 1})
	}
	if code := rt.protectedCaravanRefusal(&thief, trader); code != 0x3024 {
		t.Fatalf("thief on a level-1 caravan = %#x, want 0x3024", code)
	}
	if code := rt.protectedCaravanRefusal(trader, &thief); code != 0x3006 {
		t.Fatalf("protected trader on a thief = %#x, want 0x3006", code)
	}
	trader.ActiveCOS.Container.Rows = nil
	if code := rt.protectedCaravanRefusal(&thief, trader); code != 0 {
		t.Fatalf("thief on an empty transport = %#x, want admitted", code)
	}
}
