/*
===========================================================================

companioncopy_bench_test.go - what one pet tick's owner read costs

Two rulers for #2139. BenchmarkCompanionCopyPerPetTick prices the
companion copy itself through the production admitted seam on a
settled high-level owner. BenchmarkPetFollowTick prices the whole
advancePet tick of a bound, following pet on the same kind of owner:
the tick the live profile pays per pet, which now judges on door-read
facts and takes the copy only when it commits through the record.

===========================================================================
*/
package action

import (
	"encoding/json"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

var companionCopyBenchSink *enterworld.Character

var petFollowBenchSink []simulation.Frame

/*
================
companionCopyBenchCOS

One stored or summoned companion with the container and rental rows a
played character's pets carry.
================
*/
func companionCopyBenchCOS(gid uint32, codename string, containerRows int) *enterworld.CharacterCOS {
	rows := make([]enterworld.InventoryRow, 0, containerRows)
	for i := 0; i < containerRows; i++ {
		rows = append(rows, enterworld.InventoryRow{
			Slot: int64(i), RefObjID: 62, Codename: "ITEM_ETC_SCROLL_RETURN_01", StackCount: 50,
			MagicOptions: []uint64{uint64(i), uint64(i + 100)},
		})
	}
	return &enterworld.CharacterCOS{
		GID: gid, RefObjID: 9, Codename: codename, CurrentHP: 5000, Summoned: true,
		Container: &domain.COSContainer{Capacity: 20, Rows: rows},
		Rentals:   []domain.COSRental{{Kind: 5, ID: gid, RemainingSeconds: 3600, Tag: 7, Flag: 1}},
	}
}

/*
================
companionCopyBenchSpawn

A fully recorded world point: every pointer a settled character's
record carries.
================
*/
func companionCopyBenchSpawn(region int64, x float64) *enterworld.WorldSpawn {
	y, z := 100.0, 200.0
	angle := int64(90)
	return &enterworld.WorldSpawn{RegionID: &region, X: &x, Y: &y, Z: &z, Angle: &angle}
}

/*
================
companionCopyBenchOwner

The rich owner record described in the file banner.
================
*/
func companionCopyBenchOwner() *enterworld.Character {
	level := int64(110)
	hp := int64(15234)
	mp := int64(9876)
	guild := int64(4242)
	mode := int64(1)
	floor := int64(3)
	packed := uint32(65537)

	inventory := make([]enterworld.InventoryRow, 0, 64)
	for slot := int64(0); slot < 13; slot++ {
		inventory = append(inventory, enterworld.InventoryRow{
			Slot: slot, RefObjID: 11459, Codename: "ITEM_CH_SWORD_11_A_RARE", StackCount: 1,
			MagicOptions: []uint64{11, 22, 33},
		})
	}
	inventory = append(inventory, enterworld.InventoryRow{
		Slot: int64(domain.JobSuitSlot), RefObjID: 24577, Codename: "ITEM_CH_M_TRADE_TRADER_01", StackCount: 1,
		MagicOptions: []uint64{44},
	})
	for i := 0; i < 4; i++ {
		inventory = append(inventory, enterworld.InventoryRow{
			Slot: int64(21 + i), RefObjID: 62, Codename: "ITEM_COS_PET", StackCount: 1,
			Summon: companionCopyBenchCOS(uint32(70000+i), "COS_P_BEAR_090", 8),
		})
	}
	for slot := int64(30); slot < 70; slot++ {
		inventory = append(inventory, enterworld.InventoryRow{
			Slot: slot, RefObjID: 62, Codename: "ITEM_ETC_GOLD_01", StackCount: 1000,
			MagicOptions: []uint64{uint64(slot), uint64(slot + 1)},
		})
	}

	brood := make([]*enterworld.CharacterCOS, 0, 4)
	for i := 0; i < 4; i++ {
		brood = append(brood, companionCopyBenchCOS(uint32(80000+i), "COS_P_BROOD", 6))
	}
	mercenaries := make([]*enterworld.CharacterCOS, 0, 2)
	for i := 0; i < 2; i++ {
		mercenaries = append(mercenaries, companionCopyBenchCOS(uint32(90000+i), "COS_MERC", 5))
	}

	return &enterworld.Character{
		ID: 77, AccountID: "benchacct", Name: "benchowner", ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		Level: &level, MaxLevel: &level, CurrentHP: &hp, CurrentMP: &mp, GuildID: &guild,
		Aggressions:      map[uint32]uint32{101: 5, 202: 9, 303: 2, 404: 7},
		MissionInventory: inventory,
		World: &enterworld.CharacterWorld{
			Spawn:              companionCopyBenchSpawn(23712, 642),
			SavedReturn:        &domain.SavedReturnLocation{Definition: 1, RegionID: 25000, X: 1, Y: 2, Z: 3},
			InstanceReturn:     &domain.InstanceReturnAnchor{PackedInstance: 65537, RegionID: 23712, X: 4, Y: 5, Z: 6},
			AuthoredAreaReturn: companionCopyBenchSpawn(23712, 100),
			RebirthPoint:       companionCopyBenchSpawn(25000, 200),
			LastRecallPoint:    &domain.WorldPoint{WorldSpawn: *companionCopyBenchSpawn(23712, 300), World: 1},
			LastDeathPoint:     &domain.WorldPoint{WorldSpawn: *companionCopyBenchSpawn(23712, 400), World: 1},
			MovementMode:       &mode,
			DungeonFloorIndex:  &floor,
			PackedInstance:     &packed,
			SpawnSet:           true,
			MoveSegment:        json.RawMessage(make([]byte, 96)),
		},
		ActiveCOS:   companionCopyBenchCOS(60001, "COS_P_BEAR_090", 12),
		CapturedCOS: companionCopyBenchCOS(95000, "COS_CAPTURED", 4),
		Mercenaries: mercenaries,
		Brood:       brood,
	}
}

/*
================
BenchmarkCompanionCopyPerPetTick

One admitted companion snapshot per iteration: the petai admission
judged on the live record, then the narrowed owner copy the pet chain
reads. Report allocs; the tick pays this per pet pass per owner.
================
*/
func BenchmarkCompanionCopyPerPetTick(b *testing.B) {
	owner := companionCopyBenchOwner()
	rt := &Runtime{deps: &enterworld.Deps{}}
	const petGID = uint32(60001)
	admit := func(live *enterworld.Character) bool {
		pet := live.CompanionByGID(petGID)
		return pet != nil && !live.DeletePending && enterworld.CurrentHP(live) != 0 &&
			pet.Summoned && !pet.Mounted && pet.CurrentHP != 0
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		companionCopyBenchSink = rt.characterSnapshotCompanionAdmitted(testDivision, owner, admit)
	}
	if companionCopyBenchSink == nil {
		b.Fatal("admission refused the bench owner")
	}
}

/*
================
petFollowFixture

A bound pet following a settled owner: the runtime, the pet's tick
key, and the tick clock the caller advances. The first ticks bind the
follower through the copy, as a summon does; later ticks are the
steady follow this file prices.
================
*/
func petFollowFixture(tb testing.TB) (*Runtime, petOwnerKey, int64) {
	tb.Helper()
	// The same rich owner the copy ruler prices: the follow bench must
	// pay the copy its input pays in production, or the A/B says nothing.
	c := companionCopyBenchOwner()
	refs := testCosSource(testItems())
	refs.characters["COS_P_BEAR_090"] = &enterworld.CharacterRef{Codename: "COS_P_BEAR_090", RefObjID: 9,
		TidWord: 0x21c6, RunSpeed: 80}
	rt, _ := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	rt.CompanionSurfaceHeight = func(_ uint16, _ float64, y float64, _ float64) (float64, bool) { return y, true }
	rt.ConstrainMovement = func(_ string, from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
	const petGID = uint32(60001)
	rt.BindPetSession(testDivision, c, 101)
	key := petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: petGID}
	now := int64(1000)
	for i := 0; i < 3; i++ {
		rt.advancePet(key, now)
		now += 100
	}
	state := rt.petSessionFor(testDivision, c.Name, petGID)
	if state == nil || state.mover.AnyFollower() == nil {
		tb.Fatal("pet never took up its follow; the bench would price the bind instead")
	}
	return rt, key, now
}

/*
================
BenchmarkPetFollowTick

One advancePet tick per iteration for the bound, following pet of
petFollowFixture, on the formation lane's 100 ms cadence.
================
*/
func BenchmarkPetFollowTick(b *testing.B) {
	rt, key, now := petFollowFixture(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now += 100
		petFollowBenchSink = rt.advancePet(key, now)
	}
}

/*
================
BenchmarkPetFollowTickForcedCopy

The same tick with the companion copy forced (the test seam stands
for the pre-#2139 tick, which paid it every tick): the A/B control
for BenchmarkPetFollowTick on identical code.
================
*/
func BenchmarkPetFollowTickForcedCopy(b *testing.B) {
	rt, key, now := petFollowFixture(b)
	rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		return character.SnapshotCompanion()
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now += 100
		petFollowBenchSink = rt.advancePet(key, now)
	}
}
