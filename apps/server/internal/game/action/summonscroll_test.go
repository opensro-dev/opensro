/*
===========================================================================

summonscroll_test.go - the mall Monster Summon Scrolls through the
item-use door

Port-only, not native (issues #3257, #4030): with SRO_MALL_CATEGORIES on,
the 1, 2 and 5 people scrolls land 1, 2 and 5 strong monsters of the
user's level around the user and are spent; below level 10, in a town,
or with the setting off the scroll is refused and kept.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// summonScrollTestLevel is the user's level: the strong table's Yeti row.
const summonScrollTestLevel = 59

/*
================
summonScrollFixture

A level-59 user in region holding the three scrolls, in a division
whose template holds the Yeti (the table's level-59 kind).
================
*/
func summonScrollFixture(t *testing.T, region uint16) (*Runtime, *enterworld.Character, map[string]*enterworld.ItemRef) {
	t.Helper()
	c := testCharacter()
	level := int64(summonScrollTestLevel)
	c.Level, c.MaxLevel = &level, &level
	regionID, x, y, z := int64(region), 900.0, 85.0, 900.0
	c.World = &domain.CharacterWorld{Spawn: &domain.WorldSpawn{RegionID: &regionID, X: &x, Y: &y, Z: &z}}
	refs := testItems()
	scrolls := map[string]*enterworld.ItemRef{}
	for i, code := range []string{SummonScrollItem, SummonScrollOneItem, SummonScrollTwoItem} {
		ref := &enterworld.ItemRef{Codename: code, RefObjID: uint32(9950 + i), TypeIDs: [4]int64{3, 3, 9, 0}, Country: 3,
			NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})}
		refs[code] = ref
		scrolls[code] = ref
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
			Slot: int64(23 + i), RefObjID: ref.RefObjID, Codename: code, TypeFlags: ref.TypeFlags(), StackCount: 2})
	}
	rt, _ := newTestRuntime(c, refs)
	yeti := monster.MonsterRef{RefObjID: 1, Codename: "MOB_KK_YETI", Name: "Yeti", TidWord: 0xc6,
		Level: summonScrollTestLevel, MaxHP: 7059, WalkSpeed: 8, RunSpeed: 20}
	nests := []monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: waveTestRegion, X: 900, Y: 85, Z: 900}, SightRange: 77}}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{1: yeti}, nests))
	rt.Monsters.SetRandomSource(func() float64 { return 0.5 })
	rt.Monsters.StartDivision(testDivision)
	if err := rt.admitPopulationSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	return rt, c, scrolls
}

/*
================
summonScrollUse
================
*/
func summonScrollUse(c *enterworld.Character, ref *enterworld.ItemRef) persistentSummonUse {
	for i, row := range c.MissionInventory {
		if row.Codename == ref.Codename {
			return persistentSummonUse{division: testDivision, character: c, ref: ref, row: i}
		}
	}
	return persistentSummonUse{}
}

/*
================
summonScrollRowLeft
================
*/
func summonScrollRowLeft(c *enterworld.Character, codename string) int64 {
	for _, row := range c.MissionInventory {
		if row.Codename == codename {
			return row.StackCount
		}
	}
	return 0
}

/*
================
TestSummonScrollsLandTheirPeoplesMonsters

#4030: each scroll is "N people use" and lands N grade-7 strong monsters
of the user's level, spending one scroll; it summoned guild soldiers and
no monster before.
================
*/
func TestSummonScrollsLandTheirPeoplesMonsters(t *testing.T) {
	t.Setenv(commerce.EnvMallCategories, "on")
	for _, tc := range []struct {
		code string
		want int
	}{{SummonScrollOneItem, 1}, {SummonScrollTwoItem, 2}, {SummonScrollItem, 5}} {
		rt, c, scrolls := summonScrollFixture(t, waveTestRegion)
		var result OpResult
		if !rt.useSummonScroll(summonScrollUse(c, scrolls[tc.code]), &result) {
			t.Fatalf("%s refused: %q %+v", tc.code, result.DiagnosticRefusal, result.Frames)
		}
		actors := detachedActors(rt)
		if len(actors) != tc.want {
			t.Fatalf("%s landed %d monsters, want %d", tc.code, len(actors), tc.want)
		}
		for _, actor := range actors {
			if actor.Ref.Codename != "MOB_KK_YETI" || actor.Rarity() != partySummonGrade {
				t.Fatalf("%s landed %s at grade %d", tc.code, actor.Ref.Codename, actor.Rarity())
			}
		}
		if summonScrollRowLeft(c, tc.code) != 1 {
			t.Fatalf("%s was not spent once", tc.code)
		}
		if len(c.Mercenaries) != 0 {
			t.Fatalf("%s still stands soldiers", tc.code)
		}
	}
}

/*
================
TestSummonScrollRefusesBeforeSpending

Off, below the item's level 10, and in a town the scroll answers and
stays in the bag; a level-10 user gets the table's lowest row.
================
*/
func TestSummonScrollRefusesBeforeSpending(t *testing.T) {
	t.Setenv(commerce.EnvMallCategories, "off")
	rt, c, scrolls := summonScrollFixture(t, waveTestRegion)
	var result OpResult
	if rt.useSummonScroll(summonScrollUse(c, scrolls[SummonScrollOneItem]), &result) || summonScrollRowLeft(c, SummonScrollOneItem) != 2 {
		t.Fatal("the scroll was served with the setting off")
	}

	t.Setenv(commerce.EnvMallCategories, "on")
	low := int64(summonScrollMinLevel - 1)
	c.Level = &low
	result = OpResult{}
	if rt.useSummonScroll(summonScrollUse(c, scrolls[SummonScrollOneItem]), &result) || summonScrollRowLeft(c, SummonScrollOneItem) != 2 {
		t.Fatal("a level-9 user spent the scroll")
	}
	if len(detachedActors(rt)) != 0 {
		t.Fatal("a refused scroll landed a monster")
	}

	const jangan = 0x61a8
	rt, c, scrolls = summonScrollFixture(t, jangan)
	result = OpResult{}
	if rt.useSummonScroll(summonScrollUse(c, scrolls[SummonScrollOneItem]), &result) || summonScrollRowLeft(c, SummonScrollOneItem) != 2 {
		t.Fatal("the scroll was spent in a town")
	}

	all := func(string) bool { return true }
	if kind, ok := strongSummonKind(summonScrollMinLevel, all); !ok || kind != "MOB_AM_CRAB_CLON" {
		t.Fatalf("a level-10 user summons %q, want the table's lowest row", kind)
	}
	if kind, _ := strongSummonKind(summonScrollTestLevel, all); kind != "MOB_KK_YETI" {
		t.Fatalf("level 59 summons %q", kind)
	}
}

/*
================
TestSummonScrollsAreAdmittedByCodename

The three rows, and only they, leave the quest-tool type for the scroll
family, with their people counts.
================
*/
func TestSummonScrollsAreAdmittedByCodename(t *testing.T) {
	for code, want := range map[string]int{SummonScrollOneItem: 1, SummonScrollTwoItem: 2, SummonScrollItem: 5} {
		if SummonScrollCount(code) != want {
			t.Fatalf("%s calls %d", code, SummonScrollCount(code))
		}
		if family := admittedItemUseFamily(&enterworld.ItemRef{Codename: code, TypeIDs: [4]int64{3, 3, 9, 0}}); family != itemUseSummonScroll {
			t.Fatalf("%s is admitted as family %d", code, family)
		}
	}
	if SummonScrollCount(PartySummonScrollItem) != 0 {
		t.Fatal("the party scroll counts as a mall scroll")
	}
}
