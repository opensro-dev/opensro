/*
===========================================================================

cosdeath_test.go - a dead transport's cargo and record

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestTransportDeathDropsCargoAndDeletesRecord

4C42F0 -> 4D1FD0: a transport killed in the field drops its goods
unowned around it, and 52A000's kind-1 release deletes its record; a pet
of the same death keeps its record for revival.
================
*/
func TestTransportDeathDropsCargoAndDeletesRecord(t *testing.T) {
	for _, band := range []uint16{cosBandTransport, cosBandAttackPet} {
		rt, clock, character, source := newCombatTestRuntime(t, 100)
		equipCombatTestPet(t, rt, character, band)
		pet := character.ActiveCOS
		potion := testItems()["ITEM_ETC_HP_POTION_01"]
		pet.Container = &domain.COSContainer{Capacity: 4, Rows: []enterworld.InventoryRow{{
			Slot: 0, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 7}}}
		pet.CurrentHP = 5
		record := abnormal.Record{Status: abnormal.Burn, Level: 1, Grade: 1, DurationMs: 10000,
			PeriodMs: 2000, SourceGID: source.Gid, Rate24: 10, Scale20: 1, Param38: 10}
		owner := rt.newCosAbnormalOwner(testDivision, character, clock.NowMs())
		owner.sources = rt.captureAbnormalSources(testDivision, owner.block, []abnormal.Record{record})
		if !owner.block.Apply(owner, record, clock.NowMs()) {
			t.Fatal("status refused")
		}
		owner.block.Update(owner, clock.NowMs())
		owner.commit()
		frames := rt.cosAbnormalPublication(pet.GID, owner)
		ground := rt.Ground.All(testDivision)
		if band == cosBandAttackPet {
			if character.ActiveCOS != pet || len(ground) != 0 {
				t.Fatalf("a dead pet lost its record or dropped %d items", len(ground))
			}
			continue
		}
		if character.ActiveCOS != nil {
			t.Fatal("a dead transport kept its record")
		}
		if len(ground) != 1 || ground[0].Codename != potion.Codename || ground[0].StackCount != 7 || ground[0].DroppedBy != "" {
			t.Fatalf("cargo %+v", ground)
		}
		if !saw(frames, wire.OpObjectDespawn) {
			t.Fatal("the dead transport stayed in view")
		}
	}
}
