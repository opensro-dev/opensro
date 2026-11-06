/*
===========================================================================

sessionworld_appearance_test.go - the tick's peer appearance, reused

One tick's full snapshot per session builds the peer appearance
(equipment, avatars, guild, stall, action speed, spawn skills). The
appearance changes rarely next to a 10 Hz tick, so WorldSnapshot
keeps the last build and its fingerprint: an unchanged peer returns
the kept appearance, and any moved input rebuilds it. These tests
pin both halves over the production WorldSnapshot path.

===========================================================================
*/
package movement

import (
	"path/filepath"
	"testing"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport/worldsession"
)

// appearanceHooks is the knob set behind the fixture's presentation
// hooks: what the next snapshot must reflect when a knob moves.
type appearanceHooks struct {
	speed  float32
	skills []enterworld.EntrySkill
}

/*
================
appearanceFixture

A bound session world over a stored character with a worn set, plus
its provider and the hooks its snapshots read.
================
*/
func appearanceFixture(t testing.TB) (worldsession.SnapshotProvider, *enterworld.Deps, *enterworld.Character, *appearanceHooks) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "authority")
	authority, err := store.Open(dir, store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(authority.Close)
	modelRef, bodyShape := int64(1907), int64(1)
	seed := &enterworld.Character{Name: "memohero", ModelCodename: "CHAR_EU_MAN", ModelRef: &modelRef,
		BodyShapeByte: &bodyShape, MissionInventory: raceEquipSet(0)}
	if err := authority.CreateCharacter(doorTestDivision, "test-account", seed); err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}
	character := authority.Characters().CharactersForDivision(doorTestDivision)[0]
	deps := &enterworld.Deps{Characters: authority.Characters()}
	deps.MutateCharacter = func(c *enterworld.Character, label string, fn func()) {
		authority.MutateCharacter(c, label, fn)
	}
	deps.ReadCharacter = func(divisionID string, fn func()) {
		authority.ReadCharacters(divisionID, func([]*enterworld.Character) { fn() })
	}
	deps.Guilds = authority.Guilds()
	rt := NewRuntime(deps, simulation.NewWorldStore())
	hooks := &appearanceHooks{speed: 125}
	rt.ActionSpeed = func(string, string) float32 { return hooks.speed }
	rt.SpawnSkills = func(string, string) []enterworld.EntrySkill { return hooks.skills }
	recorded := &recordedWorld{}
	rt.WorldBound(recorded, doorTestDivision, character)
	provider, ok := recorded.snapshot.(worldsession.SnapshotProvider)
	if !ok {
		t.Fatalf("the session world offers no snapshot: %T", recorded.snapshot)
	}
	return provider, deps, character, hooks
}

/*
================
TestWorldSnapshotReusesUnchangedAppearance

Two snapshots of an unmoved peer return the same built appearance:
the tick pays for the fingerprint compare, not a rebuild.
================
*/
func TestWorldSnapshotReusesUnchangedAppearance(t *testing.T) {
	provider, _, _, _ := appearanceFixture(t)
	first := provider.WorldSnapshot()
	second := provider.WorldSnapshot()
	if first.Appearance == nil {
		t.Fatal("the snapshot lost its appearance")
	}
	if first.Appearance != second.Appearance {
		t.Fatalf("unchanged peer rebuilt its appearance: %p then %p", first.Appearance, second.Appearance)
	}
	if second.Appearance.ActionSpeed != 125 {
		t.Fatalf("appearance action speed = %v, want 125", second.Appearance.ActionSpeed)
	}
}

/*
================
TestWorldSnapshotRebuildsWhenInputsMove

Every face of the appearance rebuilds when its input moves - the
action speed, the spawn skills, and the worn set - and settles again
on the new build afterwards.
================
*/
func TestWorldSnapshotRebuildsWhenInputsMove(t *testing.T) {
	provider, deps, character, hooks := appearanceFixture(t)
	before := provider.WorldSnapshot().Appearance

	hooks.speed = 150
	moved := provider.WorldSnapshot().Appearance
	if moved == before {
		t.Fatal("an action-speed move kept the old appearance")
	}
	if moved.ActionSpeed != 150 {
		t.Fatalf("appearance action speed = %v, want 150", moved.ActionSpeed)
	}
	if settled := provider.WorldSnapshot().Appearance; settled != moved {
		t.Fatal("the rebuilt appearance did not settle")
	}

	hooks.skills = []enterworld.EntrySkill{{ID: 42, Status: 3, HasStatus: true}}
	skilled := provider.WorldSnapshot().Appearance
	if skilled == moved {
		t.Fatal("a spawn-skill move kept the old appearance")
	}
	if len(skilled.SpawnSkills) != 1 || skilled.SpawnSkills[0].SkillID != 42 || skilled.SpawnSkills[0].Status != 3 {
		t.Fatalf("spawn skills = %+v, want the one new entry", skilled.SpawnSkills)
	}

	deps.Mutate(character, "memo-test-equip", func() {
		character.MissionInventory[0].Plus = 9
	})
	equipped := provider.WorldSnapshot().Appearance
	if equipped == skilled {
		t.Fatal("an equipment move kept the old appearance")
	}
	found := false
	for _, item := range equipped.Equipment {
		if item.OptLevel == 9 {
			found = true
		}
	}
	if !found {
		t.Fatalf("equipment = %+v, want the +9 row", equipped.Equipment)
	}
}

/*
================
BenchmarkWorldSnapshotPeerAppearance

One session's full tick snapshot with a worn set, spawn skills and
an action speed riding it: the per-session cost SnapshotSessions
pays once per tick per in-world session.
================
*/
func BenchmarkWorldSnapshotPeerAppearance(b *testing.B) {
	provider, _, _, hooks := appearanceFixture(b)
	hooks.skills = []enterworld.EntrySkill{{ID: 42, Status: 3, HasStatus: true}, {ID: 43}}
	if provider.WorldSnapshot().Appearance == nil {
		b.Fatal("the snapshot lost its appearance")
	}
	b.ReportAllocs()
	for b.Loop() {
		provider.WorldSnapshot()
	}
}
