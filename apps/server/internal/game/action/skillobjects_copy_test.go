/*
===========================================================================

skillobjects_copy_test.go - the skill-object pass pays one registry copy

AdvanceSkillObjects used to detach the whole object registry twice a
tick. A pass that writes nothing now reuses the first copy for the
post-pass publication; a pass that creates, retires or restamps an
object still pays the second.

===========================================================================
*/
package action

import (
	"runtime"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/skillobject"
)

/*
================
plantedTrapRuntime

One planted quest trap whose owner session is admitted, at the fixture
clock. durationMs is the trap's registry lifetime, scanMs its scan cadence.
================
*/
func plantedTrapRuntime(t *testing.T, durationMs, scanMs uint32) (*Runtime, *fakeClock, *enterworld.Character, skillobject.Object) {
	t.Helper()
	rt, clock, c, request := statItemFixture(t, enterworld.SkillTimedEffect{})
	skills := rt.deps.(*enterworld.Deps).Skills.(namedItemSkills)
	skill := skills.staticSkillSource[100]
	skill.CastGate.QuestTrap = enterworld.SkillQuestTrap{
		Present: true, DurationMs: durationMs, ScanMs: scanMs, Radius: 20, Targets: [3]uint32{14769},
	}
	skills.staticSkillSource[100] = skill
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		14769: {RefObjID: 14769, Codename: "MOB_AM_ROGUE", MaxHP: 100, TidWord: 0xc6},
	}, nil))
	rt.Monsters.SetTimeSource(clock.Now)
	if err := rt.admitPopulationSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	rt.CanPlaceQuestTrap = func(_ *enterworld.Character, _ string) ([]wire.Frame, bool) { return nil, true }
	rt.CaptureQuestTrap = func(_ *enterworld.Character, _, _ string, _ func() bool) ([]wire.Frame, bool) {
		return nil, false
	}
	rt.HandleItemUse(testDivision, c, request)
	objects := rt.SkillObjects.Snapshot()
	if len(objects) != 1 {
		t.Fatalf("planted %d objects, want 1", len(objects))
	}
	return rt, clock, c, objects[0]
}

/*
================
TestSkillObjectQuietPassCopiesTheRegistryOnce

A hundred and twenty quiet objects each carrying a tracked set: the
pre-change pass detached the registry twice per tick, so its allocation
count sits far above the bound; the gated pass pays one copy.
================
*/
func TestSkillObjectQuietPassCopiesTheRegistryOnce(t *testing.T) {
	rt, clock, c, planted := plantedTrapRuntime(t, 300000, 300)
	for i := 0; i < 119; i++ {
		_, err := rt.SkillObjects.Create(skillobject.Object{
			Division: testDivision, Population: planted.Population,
			OwnerGID: enterworld.ObjectIDForCharacter(c), OwnerName: c.Name, CreatedMs: clock.NowMs(),
			Program: skillobject.Program{SkillID: 7108, DurationMs: 300000, ScanMs: 300, Radius: 20},
			Spawn:   planted.Spawn,
			Tracked: []skillobject.FieldRecipient{{Name: c.Name, GID: enterworld.ObjectIDForCharacter(c)}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	at := clock.NowMs() + 100 // before every object's first scan
	allocs := testing.AllocsPerRun(50, func() {
		if out := rt.AdvanceSkillObjects(at, nil); len(out) != 0 {
			t.Fatalf("quiet pass produced output: %+v", out)
		}
	})
	t.Logf("quiet AdvanceSkillObjects over 120 objects: %.0f allocs/run", allocs)
	if allocs > quietPassAllocBound {
		t.Fatalf("quiet pass allocated %.0f per run, bound %d (the second registry copy is back)", allocs, quietPassAllocBound)
	}
}

/*
================
TestSkillObjectOwnerCopyIsCombat

Guard for the #1943 skill-object slice: the per-object owner copy is
the combat copy, not the whole record. The owner's bag is bulked to
44 plain rows past loadCombatBag, where the copies actually differ:
a quiet pass over the 120 objects below measures 487,597 allocated
bytes through the combat copy and 1,098,159 through the full copy
(2,045 vs 2,405 allocs), so the bound sits between them. On an
unloaded bag the two copies cost the same, which is why this guard
bulks the bag instead of reusing the quiet-pass fixture's owner.
================
*/
func TestSkillObjectOwnerCopyIsCombat(t *testing.T) {
	rt, clock, c, planted := plantedTrapRuntime(t, 300000, 300)
	loadCombatBag(c)
	for slot := int64(20); slot < 60; slot++ {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: slot, RefObjID: 9999, Codename: "ITEM_QSP_TEST_01", StackCount: 1})
	}
	for i := 0; i < 119; i++ {
		_, err := rt.SkillObjects.Create(skillobject.Object{
			Division: testDivision, Population: planted.Population,
			OwnerGID: enterworld.ObjectIDForCharacter(c), OwnerName: c.Name, CreatedMs: clock.NowMs(),
			Program: skillobject.Program{SkillID: 7108, DurationMs: 300000, ScanMs: 300, Radius: 20},
			Spawn:   planted.Spawn,
			Tracked: []skillobject.FieldRecipient{{Name: c.Name, GID: enterworld.ObjectIDForCharacter(c)}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	at := clock.NowMs() + 100 // before every object's first scan
	run := func() {
		if out := rt.AdvanceSkillObjects(at, nil); len(out) != 0 {
			t.Fatalf("quiet pass produced output: %+v", out)
		}
	}
	for i := 0; i < 5; i++ {
		run()
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < 50; i++ {
		run()
	}
	runtime.ReadMemStats(&after)
	if got := (after.TotalAlloc - before.TotalAlloc) / 50; got > 750000 {
		t.Fatalf("quiet pass allocated %d bytes/run, want <= 750000 (whole-record owner copy back?)", got)
	}
}

/*
================
TestSkillObjectTeleportingOwnerRetires

The admission's teleport fact is the object pass's only gate on a
teleporting owner (#2012 review): while the owner is in teleport
mode its standing object retires instead of ticking. Dropping
NativeTeleportMode from the admission passes the whole suite
without this pin - the object would scan on.
================
*/
func TestSkillObjectTeleportingOwnerRetires(t *testing.T) {
	rt, clock, c, _ := plantedTrapRuntime(t, 300000, 300)
	c.NativeTeleportMode = 1
	if out := rt.AdvanceSkillObjects(clock.NowMs()+100, nil); len(out) != 0 {
		t.Fatalf("teleporting owner's object produced output: %+v", out)
	}
	if objects := rt.SkillObjects.Snapshot(); len(objects) != 0 {
		t.Fatalf("teleporting owner's object survived: %+v", objects)
	}
}

// quietPassAllocBound sits between the two measured passes over the
// fixture above: 2045 allocs/run gated, 2168 ungated - the difference
// is one registry copy (the slice plus one tracked-set copy per object).
// First measured at 1925/2048; #1965 then admits the owner copy through
// a closure, one allocation per present owner (+120 here) on both sides.
const quietPassAllocBound = 2106

/*
================
TestSkillObjectQuietPassStillPublishesSpawns

The reused pre-pass copy is the registry on a quiet pass, so a viewer
that has not published the object yet still receives its spawn.
================
*/
func TestSkillObjectQuietPassStillPublishesSpawns(t *testing.T) {
	rt, clock, c, planted := plantedTrapRuntime(t, 300000, 300)
	sessions := []simulation.SessionView{{
		DivisionID: testDivision, CharacterID: c.ID, Population: planted.Population,
		PublishedObjects: []uint32{}, World: simulation.SeedWorldState(c),
	}}
	out := rt.AdvanceSkillObjects(clock.NowMs()+100, sessions)
	spawns := 0
	for _, batch := range out {
		if batch.OnlyCharacterID != c.ID {
			continue
		}
		for _, f := range batch.Frames {
			if f.Opcode == wire.OpSingleObjectSpawn {
				spawns++
			}
		}
	}
	if spawns != 1 {
		t.Fatalf("quiet pass published %d spawns, want 1: %+v", spawns, out)
	}
}

/*
================
TestSkillObjectExpiryStillDespawnsAfterAWrittenPass

Expiry retires the object inside the commit pass, so the pass writes:
the post-pass copy is taken and the published viewer is despawned.
================
*/
func TestSkillObjectExpiryStillDespawnsAfterAWrittenPass(t *testing.T) {
	rt, clock, c, planted := plantedTrapRuntime(t, 300, 300)
	sessions := []simulation.SessionView{{
		DivisionID: testDivision, CharacterID: c.ID, Population: planted.Population,
		PublishedObjects: []uint32{planted.Spawn.GID}, World: simulation.SeedWorldState(c),
	}}
	out := rt.AdvanceSkillObjects(clock.NowMs()+301, sessions)
	despawns := 0
	for _, batch := range out {
		if batch.OnlyCharacterID != c.ID {
			continue
		}
		for _, f := range batch.Frames {
			if f.Opcode == wire.OpObjectDespawn {
				despawns++
			}
		}
	}
	if despawns != 1 {
		t.Fatalf("expiry pass published %d despawns, want 1: %+v", despawns, out)
	}
	if len(rt.SkillObjects.Snapshot()) != 0 {
		t.Fatal("expired object survived the pass")
	}
}
