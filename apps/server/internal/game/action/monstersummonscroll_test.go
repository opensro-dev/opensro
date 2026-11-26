/*
===========================================================================

monstersummonscroll_test.go - the monster summon scrolls through the
item-use door (#3785, #4530)

A Rigid scroll lands its rigid unique beside the user, persistent, and
is spent; the family cooltime (600 s, the item rows' COOLTIME parameter)
then refuses every scroll without spending; a town refuses likewise.
The anti-bot stone lands MOB_AUTOMOB under the handler's own 60 s gate,
and is the one scroll a dungeon region admits.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// rigidTestScroll is the scroll whose monster the fixture's template holds.
const rigidTestScroll = "ITEM_RIGID_IVY_L2_SUMMON_SCROLL"

// rigidTestMonster is the rigid unique that scroll calls.
const rigidTestMonster = "MOB_AM_IVY_L2"

// rigidTestNowMs is the fixture clock's instant for a first use.
const rigidTestNowMs = 1_700_000_000_000

// scrollTestDungeon is a dungeon region (bit 0x8000) where combat is allowed.
const scrollTestDungeon = 0x8006

// The item type ids the shipped rows carry.
var (
	rigidScrollTypeIDs  = [4]int64{3, 3, 3, 23}
	autoMobStoneTypeIDs = [4]int64{3, 3, 3, 2}
)

/*
================
monsterScrollFixture

A level-90 user holding two of the named scroll, in a division whose
template holds the monster the scroll calls.
================
*/
func monsterScrollFixture(t *testing.T, region uint16, scroll string, typeIDs [4]int64) (*Runtime, *enterworld.Character, *enterworld.ItemRef) {
	t.Helper()
	called, ok := MonsterSummonScrollMonster(scroll)
	if !ok {
		t.Fatalf("%s is no monster summon scroll", scroll)
	}
	c := testCharacter()
	level := int64(90)
	c.Level, c.MaxLevel = &level, &level
	regionID, x, y, z := int64(region), 900.0, 85.0, 900.0
	c.World = &domain.CharacterWorld{Spawn: &domain.WorldSpawn{RegionID: &regionID, X: &x, Y: &y, Z: &z}}
	ref := &enterworld.ItemRef{Codename: scroll, RefObjID: 9960, TypeIDs: typeIDs, Country: 3,
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})}
	refs := testItems()
	refs[scroll] = ref
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 23, RefObjID: ref.RefObjID, Codename: scroll, TypeFlags: ref.TypeFlags(), StackCount: 2})
	rt, _ := newTestRuntime(c, refs)
	kind := monster.MonsterRef{RefObjID: 2, Codename: called, Name: called, TidWord: 0xc6,
		Level: 30, MaxHP: 50000, WalkSpeed: 8, RunSpeed: 20}
	nests := []monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: 2, RegionID: waveTestRegion, X: 900, Y: 85, Z: 900}, SightRange: 77}}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{2: kind}, nests))
	rt.Monsters.SetRandomSource(func() float64 { return 0.5 })
	rt.Monsters.StartDivision(testDivision)
	if err := rt.admitPopulationSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	return rt, c, ref
}

/*
================
rigidScrollFixture

monsterScrollFixture for the Rigid Ivy scroll.
================
*/
func rigidScrollFixture(t *testing.T, region uint16) (*Runtime, *enterworld.Character, *enterworld.ItemRef) {
	t.Helper()
	return monsterScrollFixture(t, region, rigidTestScroll, rigidScrollTypeIDs)
}

/*
================
rigidScrollUse
================
*/
func rigidScrollUse(c *enterworld.Character, ref *enterworld.ItemRef, nowMs int64) persistentSummonUse {
	for i, row := range c.MissionInventory {
		if row.Codename == ref.Codename {
			return persistentSummonUse{division: testDivision, character: c, ref: ref, row: i, nowMs: nowMs}
		}
	}
	return persistentSummonUse{}
}

/*
================
TestRigidSummonScrollLandsItsUnique

The scroll lands its rigid unique beside the user and is spent; the
monster has no lifetime (it stays until killed).
================
*/
func TestRigidSummonScrollLandsItsUnique(t *testing.T) {
	rt, c, ref := rigidScrollFixture(t, waveTestRegion)
	var result OpResult
	if !rt.useMonsterSummonScroll(rigidScrollUse(c, ref, rigidTestNowMs), &result) {
		t.Fatalf("refused: %q %+v", result.DiagnosticRefusal, result.Frames)
	}
	actors := detachedActors(rt)
	if len(actors) != 1 || actors[0].Ref.Codename != rigidTestMonster {
		t.Fatalf("landed %+v, want one %s", actors, rigidTestMonster)
	}
	if summonScrollRowLeft(c, rigidTestScroll) != 1 {
		t.Fatal("the scroll was not spent once")
	}
}

/*
================
TestRigidSummonScrollCooldownIsTheFamily600Seconds

After a use, every scroll refuses inside 600 s (nothing spent) and
answers again at 600 s.
================
*/
func TestRigidSummonScrollCooldownIsTheFamily600Seconds(t *testing.T) {
	rt, c, ref := rigidScrollFixture(t, waveTestRegion)
	var result OpResult
	if !rt.useMonsterSummonScroll(rigidScrollUse(c, ref, rigidTestNowMs), &result) {
		t.Fatalf("first use refused: %q", result.DiagnosticRefusal)
	}
	result = OpResult{}
	if rt.useMonsterSummonScroll(rigidScrollUse(c, ref, rigidTestNowMs+rigidSummonCooldownMs-1), &result) {
		t.Fatal("a second scroll was served inside the family cooltime")
	}
	if summonScrollRowLeft(c, rigidTestScroll) != 1 {
		t.Fatal("the refused scroll was spent")
	}
	if len(detachedActors(rt)) != 1 {
		t.Fatal("the refused scroll landed a monster")
	}
	result = OpResult{}
	if !rt.useMonsterSummonScroll(rigidScrollUse(c, ref, rigidTestNowMs+rigidSummonCooldownMs), &result) {
		t.Fatalf("the scroll stayed refused at 600 s: %q", result.DiagnosticRefusal)
	}
	if summonScrollRowLeft(c, rigidTestScroll) != 0 {
		t.Fatal("the second scroll was not spent")
	}
	if len(detachedActors(rt)) != 2 {
		t.Fatal("the second unique did not land")
	}
}

/*
================
TestRigidSummonScrollTownRefusal

A town refuses the scroll and keeps it.
================
*/
func TestRigidSummonScrollTownRefusal(t *testing.T) {
	const jangan = 0x61a8
	rt, c, ref := rigidScrollFixture(t, jangan)
	var result OpResult
	if rt.useMonsterSummonScroll(rigidScrollUse(c, ref, rigidTestNowMs), &result) {
		t.Fatal("the scroll was served in a town")
	}
	if summonScrollRowLeft(c, rigidTestScroll) != 2 {
		t.Fatal("the refused scroll was spent")
	}
	if len(detachedActors(rt)) != 0 {
		t.Fatal("the refused scroll landed a monster")
	}
}

/*
================
TestAutoMobStoneLandsUnderTheHandlerGate

The stone lands MOB_AUTOMOB and is spent; the handler's own 60 s gate
then answers the reuse delay without spending, and opens at 60 s.
================
*/
func TestAutoMobStoneLandsUnderTheHandlerGate(t *testing.T) {
	rt, c, ref := monsterScrollFixture(t, waveTestRegion, AutoMobStoneItem, autoMobStoneTypeIDs)
	var result OpResult
	if !rt.useMonsterSummonScroll(rigidScrollUse(c, ref, rigidTestNowMs), &result) {
		t.Fatalf("refused: %q %+v", result.DiagnosticRefusal, result.Frames)
	}
	if actors := detachedActors(rt); len(actors) != 1 || actors[0].Ref.Codename != AutoMobMonster {
		t.Fatalf("landed %+v, want one %s", actors, AutoMobMonster)
	}
	result = OpResult{}
	if rt.useMonsterSummonScroll(rigidScrollUse(c, ref, rigidTestNowMs+monsterScrollGateMs-1), &result) {
		t.Fatal("a second stone was served inside the 60 s gate")
	}
	if code := itemUseFailureCode(result); code != wire.ErrCodeItemReuseDelay {
		t.Fatalf("the gate answered %#x, want the reuse delay %#x", code, wire.ErrCodeItemReuseDelay)
	}
	if summonScrollRowLeft(c, AutoMobStoneItem) != 1 {
		t.Fatal("the gated stone was spent")
	}
	result = OpResult{}
	if !rt.useMonsterSummonScroll(rigidScrollUse(c, ref, rigidTestNowMs+monsterScrollGateMs), &result) {
		t.Fatalf("the stone stayed refused at 60 s: %q", result.DiagnosticRefusal)
	}
}

/*
================
TestDungeonAdmitsOnlyTheAutoMobStone

In a dungeon region a Rigid scroll answers UIIT_MSG_COS_CREATE_DUNGEON
and is kept; the anti-bot stone is still served.
================
*/
func TestDungeonAdmitsOnlyTheAutoMobStone(t *testing.T) {
	rt, c, ref := rigidScrollFixture(t, scrollTestDungeon)
	var result OpResult
	if rt.useMonsterSummonScroll(rigidScrollUse(c, ref, rigidTestNowMs), &result) {
		t.Fatal("a Rigid scroll was served in a dungeon")
	}
	if code := itemUseFailureCode(result); code != wire.ErrCodeSummonInDungeon {
		t.Fatalf("the dungeon answered %#x, want %#x", code, wire.ErrCodeSummonInDungeon)
	}
	if summonScrollRowLeft(c, rigidTestScroll) != 2 {
		t.Fatal("the refused scroll was spent")
	}
	rt, c, ref = monsterScrollFixture(t, scrollTestDungeon, AutoMobStoneItem, autoMobStoneTypeIDs)
	result = OpResult{}
	if !rt.useMonsterSummonScroll(rigidScrollUse(c, ref, rigidTestNowMs), &result) {
		t.Fatalf("the stone was refused in a dungeon: %q %+v", result.DiagnosticRefusal, result.Frames)
	}
	if summonScrollRowLeft(c, AutoMobStoneItem) != 1 {
		t.Fatal("the stone was not spent")
	}
	// The cave floor refuses the outdoor ring, so the monster lands on the
	// user's own point instead of being lost with the spent stone.
	actors := detachedActors(rt)
	if len(actors) != 1 || actors[0].Ref.Codename != AutoMobMonster || actors[0].Spawn.RegionID != scrollTestDungeon {
		t.Fatalf("landed %+v, want one %s in region %#x", actors, AutoMobMonster, scrollTestDungeon)
	}
	failed := exchangeBanner(partySummonFailedLine)
	for _, frame := range result.Frames {
		if frame.Opcode == failed.Opcode && string(frame.Payload) == string(failed.Payload) {
			t.Fatal("the dungeon use answered the failed-summon banner")
		}
	}
}

/*
================
TestMonsterSummonScrollsAreAdmittedByCodename

The shipped rows, and only they, leave the unsupported answer for the
monster summon family.
================
*/
func TestMonsterSummonScrollsAreAdmittedByCodename(t *testing.T) {
	for code, scroll := range monsterSummonScrolls {
		if got, ok := MonsterSummonScrollMonster(code); !ok || got != scroll.monster {
			t.Fatalf("%s calls %q, %v", code, got, ok)
		}
		typeIDs := rigidScrollTypeIDs
		if code == AutoMobStoneItem {
			typeIDs = autoMobStoneTypeIDs
		}
		if family := admittedItemUseFamily(&enterworld.ItemRef{Codename: code, TypeIDs: typeIDs}); family != itemUseMonsterSummonScroll {
			t.Fatalf("%s is admitted as family %d", code, family)
		}
	}
	if got, _ := MonsterSummonScrollMonster(AutoMobStoneItem); got != AutoMobMonster {
		t.Fatalf("the stone calls %q, want %s", got, AutoMobMonster)
	}
	if _, ok := MonsterSummonScrollMonster(PartySummonScrollItem); ok {
		t.Fatal("the party scroll counts as a monster summon scroll")
	}
	if family := admittedItemUseFamily(&enterworld.ItemRef{Codename: "ITEM_ETC_OTHER", TypeIDs: rigidScrollTypeIDs}); family == itemUseMonsterSummonScroll {
		t.Fatal("an unknown 3/3/3/23 item counts as a monster summon scroll")
	}
}
