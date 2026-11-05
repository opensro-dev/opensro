/*
===========================================================================

projectioncache_test.go - tests for projectioncache.go

The cache must be invisible: every answer equals a fresh computation
over the uncached rank rows, whatever writer changed the skill list or
the equipment, from any number of goroutines.

===========================================================================
*/
package combat

import (
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
)

/*
================
projectionFixture

Three passive groups learned to rank 2: setv power and reat in group 1,
a real resistance gated by the sword's reqi in group 2, damage return
and a weapon-kind critical in group 3.
================
*/
func projectionFixture() (passiveSkills, itemRefs, *enterworld.ItemRef) {
	sword := &enterworld.ItemRef{RefObjID: 71, Codename: "ITEM_PC_SWORD", TypeIDs: [4]int64{3, 1, 6, 2}, Combat: &enterworld.ItemCombatRef{}}
	power := func(value uint32) enterworld.SkillPassiveParameters {
		return enterworld.SkillPassiveParameters{Pinned: true,
			Mask: 1 << enterworld.ParameterTwoHandPower,
			Values: enterworld.SkillParameterValues{
				enterworld.ParameterTwoHandPower: value,
			}}
	}
	gated := power(0)
	gated.Real = enterworld.SkillPassiveReal{Mask: 0x17fafc0, Flat: 50, Grade: 9}
	skills := passiveSkills{
		101: {ID: 101, Group: 1, Level: 1, PassiveParameters: power(5)},
		102: {ID: 102, Group: 1, Level: 2, PassiveParameters: func() enterworld.SkillPassiveParameters {
			p := power(7)
			p.Reat = enterworld.SkillPassiveReat{Mask: 0x3, Value: 11}
			return p
		}()},
		201: {ID: 201, Group: 2, Level: 1, PassiveParameters: gated},
		202: {ID: 202, Group: 2, Level: 2, PassiveParameters: gated,
			Reqi: enterworld.SkillReqi{Present: true, Count: 1, Pairs: [5]enterworld.SkillReqiPair{{Kind: 6, Value: 2}}}},
		301: {ID: 301, Group: 3, Level: 1},
		302: {ID: 302, Group: 3, Level: 2,
			PassiveParameters: enterworld.SkillPassiveParameters{Pinned: true, DamageReturn: enterworld.SkillDamageReturn{Present: true, Chance: 20, Physical: 15, Magical: 10, Range: 3}},
			PassiveCritical:   enterworld.SkillPassiveCritical{Pinned: true, Flat: 5, WeaponKind: 2}},
	}
	return skills, itemRefs{sword.Codename: sword}, sword
}

/*
================
freshProjection

The cache's reference answer: the three projections over freshly built
rank rows, with no cache in between.
================
*/
func freshProjection(c *domain.Character, skills enterworld.SkillDataSource, items enterworld.ItemRefSource, weaponKind uint8) learnedProjection {
	rows := learnedRankRows(c, skills, nil)
	writes, power, err := learnedPassivesFrom(c, rows, items, weaponKind)
	want := learnedProjection{writes: writes, power: power, err: err}
	if err == nil {
		want.resistance = learnedStatusResistanceFrom(c, rows, items)
		want.damageReturn, want.damageReturnSkill = learnedDamageReturnFrom(c, rows, items)
	}
	return want
}

/*
================
assertProjectionFresh
================
*/
func assertProjectionFresh(t *testing.T, step string, c *domain.Character, skills enterworld.SkillDataSource, items enterworld.ItemRefSource, weaponKind uint8) learnedProjection {
	t.Helper()
	got := learnedProjections(c, skills, items, weaponKind)
	want := freshProjection(c, skills, items, weaponKind)
	if !reflect.DeepEqual(got.writes, want.writes) || !reflect.DeepEqual(got.power, want.power) ||
		got.resistance != want.resistance || got.damageReturn != want.damageReturn ||
		got.damageReturnSkill != want.damageReturnSkill || (got.err == nil) != (want.err == nil) {
		t.Fatalf("%s: cached %+v, fresh %+v", step, got, want)
	}
	return got
}

/*
================
TestLearnedProjectionsFollowEveryContentWriter

Each way the inputs move - learning, overwriting and deleting skills,
breaking or swapping the gated equipment, another weapon kind, another
source, a character without an ID - the cached answer equals a fresh
one, and a repeated read is served by the same backing store.
================
*/
func TestLearnedProjectionsFollowEveryContentWriter(t *testing.T) {
	skills, items, sword := projectionFixture()
	c := &domain.Character{ID: 980001, Level: pointer(80), Strength: pointer(300), Intellect: pointer(200),
		Skills: []uint32{101, 102, 201, 202, 301, 302},
		MissionInventory: []domain.InventoryRow{
			{Slot: 6, RefObjID: sword.RefObjID, Codename: sword.Codename, TypeFlags: sword.TypeFlags(), VarianceBits: "0", Durability: 90},
		}}
	defer ForgetLearnedProjections(c.ID)

	first := assertProjectionFresh(t, "seeded", c, skills, items, 2)
	again := assertProjectionFresh(t, "repeat (hit)", c, skills, items, 2)
	if len(first.writes) == 0 || &first.writes[0] != &again.writes[0] {
		t.Fatal("a repeated read recomputed the projections")
	}
	if first.resistance == ([17]abnormal.Resistance{}) {
		t.Fatal("the gated resistance never landed; the fixture is broken")
	}

	c.Skills = append(c.Skills, 103)
	skills[103] = enterworld.SkillRow{ID: 103, Group: 1, Level: 3, PassiveParameters: enterworld.SkillPassiveParameters{
		Pinned: true, Mask: 1 << enterworld.ParameterTwoHandPower,
		Values: enterworld.SkillParameterValues{enterworld.ParameterTwoHandPower: 9}}}
	learned := assertProjectionFresh(t, "learn appends", c, skills, items, 2)
	if learned.power == first.power {
		t.Fatal("the learned rank left the cached power unchanged")
	}
	c.Skills[len(c.Skills)-1] = 102
	assertProjectionFresh(t, "learn overwritten back", c, skills, items, 2)
	c.Skills = slices.Delete(c.Skills, 0, 1)
	assertProjectionFresh(t, "skill deleted", c, skills, items, 2)

	c.MissionInventory[0].Durability = 0
	broken := assertProjectionFresh(t, "gating weapon broken", c, skills, items, 2)
	if broken.resistance != ([17]abnormal.Resistance{}) {
		t.Fatalf("a broken sword still gates the resistance in: %+v", broken.resistance)
	}
	c.MissionInventory[0].Durability = 90
	assertProjectionFresh(t, "gating weapon repaired", c, skills, items, 2)
	staff := &enterworld.ItemRef{RefObjID: 72, Codename: "ITEM_PC_STAFF", TypeIDs: [4]int64{3, 1, 6, 9}, Combat: &enterworld.ItemCombatRef{}}
	items[staff.Codename] = staff
	c.MissionInventory[0] = domain.InventoryRow{Slot: 6, RefObjID: staff.RefObjID, Codename: staff.Codename, TypeFlags: staff.TypeFlags(), VarianceBits: "0", Durability: 90}
	assertProjectionFresh(t, "gating weapon swapped", c, skills, items, 2)

	assertProjectionFresh(t, "a staff's kind", c, skills, items, 9)
	assertProjectionFresh(t, "no weapon kind", c, skills, items, 0)

	other := projectionFixtureSkillsWithPower(t, skills, 42)
	assertProjectionFresh(t, "another skill source", c, other, items, 2)
	anonymous := *c
	anonymous.ID = 0
	assertProjectionFresh(t, "no character ID", &anonymous, skills, items, 2)
}

/*
================
TestLearnedProjectionsKeyOnEquipmentOnly

The key covers the equipment band only: a narrow equipment-only copy
of a character and its full copy (bag rows and all) hit the same
entry, a bag-only change keeps it, and an equipment change misses.
================
*/
func TestLearnedProjectionsKeyOnEquipmentOnly(t *testing.T) {
	skills, items, sword := projectionFixture()
	full := &domain.Character{ID: 980010, Level: pointer(80), Strength: pointer(300), Intellect: pointer(200),
		Skills: []uint32{101, 102, 201, 202, 301, 302},
		MissionInventory: []domain.InventoryRow{
			{Slot: 6, RefObjID: sword.RefObjID, Codename: sword.Codename, TypeFlags: sword.TypeFlags(), VarianceBits: "0", Durability: 90},
			{Slot: 20, RefObjID: 99, Codename: "ITEM_BAG_THING", VarianceBits: "0", Durability: 5},
		}}
	defer ForgetLearnedProjections(full.ID)

	first := assertProjectionFresh(t, "full copy seeds", full, skills, items, 2)
	narrow := full.SnapshotEquipment()
	if len(narrow.MissionInventory) != 1 {
		t.Fatalf("narrow copy kept %+v", narrow.MissionInventory)
	}
	again := assertProjectionFresh(t, "narrow copy reads", narrow, skills, items, 2)
	if &first.writes[0] != &again.writes[0] {
		t.Fatal("the narrow copy recomputed the projections")
	}

	full.MissionInventory[1].Codename = "ITEM_BAG_OTHER"
	still := assertProjectionFresh(t, "bag change reads", full, skills, items, 2)
	if &first.writes[0] != &still.writes[0] {
		t.Fatal("a bag-only change recomputed the projections")
	}

	narrow.MissionInventory[0].Durability = 0
	broken := assertProjectionFresh(t, "broken weapon reads", narrow, skills, items, 2)
	if broken.resistance != ([17]abnormal.Resistance{}) {
		t.Fatalf("a broken sword still gates the resistance in: %+v", broken.resistance)
	}
	if &first.writes[0] == &broken.writes[0] {
		t.Fatal("an equipment change kept the cached projections")
	}
}

/*
================
TestLearnedProjectionsFingerprintIsTwoSided

#1943: the key covers exactly what the projection reads. Every keyed
field of every equipment slot below inventory.EquipmentSlotEnd misses
when it moves; every field of a bag row keeps the hit. A future
projection read of a field the key skips shows up here as a stale hit.
================
*/
func TestLearnedProjectionsFingerprintIsTwoSided(t *testing.T) {
	skills, items, sword := projectionFixture()
	seed := func() *domain.Character {
		c := &domain.Character{ID: 980020, Level: pointer(80), Strength: pointer(300), Intellect: pointer(200),
			Skills: []uint32{101, 102, 201, 202, 301, 302}}
		for slot := int64(0); slot < int64(inventory.EquipmentSlotEnd); slot++ {
			row := domain.InventoryRow{Slot: slot, RefObjID: uint32(500 + slot), Codename: fmt.Sprintf("ITEM_EQUIP_%d", slot), VarianceBits: "0", Durability: 50}
			if slot == 6 {
				row = domain.InventoryRow{Slot: 6, RefObjID: sword.RefObjID, Codename: sword.Codename, TypeFlags: sword.TypeFlags(), VarianceBits: "0", Durability: 90}
			}
			c.MissionInventory = append(c.MissionInventory, row)
		}
		for slot := int64(inventory.EquipmentSlotEnd); slot < int64(inventory.EquipmentSlotEnd)+3; slot++ {
			c.MissionInventory = append(c.MissionInventory, domain.InventoryRow{Slot: slot, RefObjID: 99, Codename: "ITEM_BAG_THING", VarianceBits: "0", Durability: 5, StackCount: 3})
		}
		return c
	}
	end := int(inventory.EquipmentSlotEnd)
	hit := func(step string, mutate func(*domain.Character), want bool) {
		t.Helper()
		c := seed()
		ForgetLearnedProjections(c.ID)
		defer ForgetLearnedProjections(c.ID)
		first := assertProjectionFresh(t, step+" (seed)", c, skills, items, 2)
		mutate(c)
		again := assertProjectionFresh(t, step, c, skills, items, 2)
		if got := len(first.writes) > 0 && &first.writes[0] == &again.writes[0]; got != want {
			t.Errorf("%s: cache hit = %v, want %v", step, got, want)
		}
	}
	for i := 0; i < end; i++ {
		hit(fmt.Sprintf("equipment %d codename", i), func(c *domain.Character) { c.MissionInventory[i].Codename += "_X" }, false)
		hit(fmt.Sprintf("equipment %d durability", i), func(c *domain.Character) { c.MissionInventory[i].Durability-- }, false)
		hit(fmt.Sprintf("equipment %d slot", i), func(c *domain.Character) {
			c.MissionInventory[i].Slot = int64((i + 1) % end)
		}, false)
	}
	for i := end; i < end+3; i++ {
		hit(fmt.Sprintf("bag %d count", i), func(c *domain.Character) { c.MissionInventory[i].StackCount++ }, true)
		hit(fmt.Sprintf("bag %d plus", i), func(c *domain.Character) { c.MissionInventory[i].Plus++ }, true)
		hit(fmt.Sprintf("bag %d durability", i), func(c *domain.Character) { c.MissionInventory[i].Durability++ }, true)
		hit(fmt.Sprintf("bag %d codename", i), func(c *domain.Character) { c.MissionInventory[i].Codename = "ITEM_BAG_OTHER" }, true)
	}
}

/*
================
projectionFixtureSkillsWithPower

The fixture's skill source with group 1's rank-2 power replaced, standing
for a reload that brings different rows behind the same identity slot.
================
*/
func projectionFixtureSkillsWithPower(t *testing.T, skills passiveSkills, value uint32) passiveSkills {
	t.Helper()
	other := passiveSkills{}
	for id, row := range skills {
		other[id] = row
	}
	row := other[102]
	row.PassiveParameters.Values = enterworld.SkillParameterValues{enterworld.ParameterTwoHandPower: value}
	other[102] = row
	return other
}

/*
================
TestLearnedProjectionsSharedResultsSurviveStatsCalls

The cached writes are shared: two full stat snapshots must leave them
byte for byte as computed.
================
*/
func TestLearnedProjectionsSharedResultsSurviveStatsCalls(t *testing.T) {
	skills, items, sword := projectionFixture()
	c := &domain.Character{ID: 980002, Level: pointer(80), MaxLevel: pointer(80), Strength: pointer(300), Intellect: pointer(200),
		Skills:           []uint32{101, 102, 201, 202, 301, 302},
		MissionInventory: []domain.InventoryRow{{Slot: 6, RefObjID: sword.RefObjID, Codename: sword.Codename, TypeFlags: sword.TypeFlags(), VarianceBits: "0", Durability: 90}}}
	defer ForgetLearnedProjections(c.ID)
	before := assertProjectionFresh(t, "seeded", c, skills, items, 2)
	stored := slices.Clone(before.writes)
	for i := range 2 {
		stats, _, err := PlayerStats(c, Catalogs{Items: items, Skills: skills})
		if err != nil {
			t.Fatal(err)
		}
		if stats.StatusResistance != before.resistance || stats.DamageReturn != before.damageReturn ||
			stats.DamageReturnSkill != before.damageReturnSkill || stats.SkillParameters != before.power {
			t.Fatalf("call %d: stats %+v, projection %+v", i, stats, before)
		}
	}
	after := assertProjectionFresh(t, "after stats", c, skills, items, 2)
	if !reflect.DeepEqual(after.writes, stored) {
		t.Fatal("a stats call mutated the shared cached writes")
	}
}

/*
================
TestLearnedProjectionsAreSafeForConcurrentReaders

Readers on many goroutines, each with its own snapshot of one character,
while the snapshots keep changing the list (run under -race).
================
*/
func TestLearnedProjectionsAreSafeForConcurrentReaders(t *testing.T) {
	skills, items, sword := projectionFixture()
	const id = 980003
	defer ForgetLearnedProjections(id)
	lists := [][]uint32{{101}, {101, 201}, {102, 202}, {103, 203}, {302}}
	skills[103] = enterworld.SkillRow{ID: 103, Group: 1, Level: 3}
	skills[203] = enterworld.SkillRow{ID: 203, Group: 2, Level: 3}
	var wait sync.WaitGroup
	errors := make(chan string, 64)
	for reader := range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for i := range 200 {
				c := &domain.Character{ID: id, Skills: slices.Clone(lists[(reader+i)%len(lists)]),
					MissionInventory: []domain.InventoryRow{{Slot: 6, RefObjID: sword.RefObjID, Codename: sword.Codename, TypeFlags: sword.TypeFlags(), VarianceBits: "0", Durability: 90}}}
				got, want := learnedProjections(c, skills, items, 2), freshProjection(c, skills, items, 2)
				if !reflect.DeepEqual(got.writes, want.writes) || got.resistance != want.resistance {
					errors <- fmt.Sprintf("list %v: cached %+v, fresh %+v", c.Skills, got, want)
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errors)
	for message := range errors {
		t.Fatal(message)
	}
}

/*
================
TestLearnedProjectionsEvictAndForget
================
*/
func TestLearnedProjectionsEvictAndForget(t *testing.T) {
	skills := rankFixture()
	const first = 990000
	for i := range projectionCacheMax + 1 {
		learnedProjections(&domain.Character{ID: first + int64(i), Skills: []uint32{101}}, skills, itemRefs{}, 0)
	}
	projections.mu.Lock()
	_, evicted := projections.entries[first]
	_, kept := projections.entries[first+projectionCacheMax]
	size := projections.order.Len()
	projections.mu.Unlock()
	if evicted || !kept || size > projectionCacheMax {
		t.Fatalf("LRU: oldest kept=%v newest kept=%v size=%d (max %d)", evicted, kept, size, projectionCacheMax)
	}
	ForgetLearnedProjections(first + projectionCacheMax)
	projections.mu.Lock()
	_, still := projections.entries[first+projectionCacheMax]
	projections.mu.Unlock()
	if still {
		t.Fatal("ForgetLearnedProjections kept the entry")
	}
	for i := range projectionCacheMax + 1 {
		ForgetLearnedProjections(first + int64(i))
	}
}
