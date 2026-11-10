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
	"strconv"
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
TestWorldSnapshotReusesAppearanceForUnsortedWornRows

The builder sorts a copy of the captured worn rows, never the
capture: a peer whose stored rows sit out of slot order still
reuses its appearance. Sorting the capture in place would leave
the kept fingerprint holding slot order while every fresh capture
arrives in stored order - never equal, so the peer would rebuild
on every tick and lose this whole change's saving.
================
*/
func TestWorldSnapshotReusesAppearanceForUnsortedWornRows(t *testing.T) {
	provider, deps, character, _ := appearanceFixture(t)

	deps.Mutate(character, "memo-test-unsorted", func() {
		rows := character.MissionInventory
		rows[0], rows[1] = rows[1], rows[0]
	})

	first := provider.WorldSnapshot()
	second := provider.WorldSnapshot()
	if first.Appearance == nil {
		t.Fatal("the snapshot lost its appearance")
	}
	if first.Appearance != second.Appearance {
		t.Fatalf("unsorted worn rows rebuilt the appearance: %p then %p", first.Appearance, second.Appearance)
	}
	if len(first.Appearance.Equipment) != 2 ||
		first.Appearance.Equipment[0].RefObjID != 3632 || first.Appearance.Equipment[1].RefObjID != 3653 {
		t.Fatalf("equipment = %+v, want the two worn rows in slot order", first.Appearance.Equipment)
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
TestWorldSnapshotRebuildsForAvatarAndJobSuit

The two worn faces that reach the appearance only through the
store's commit door: an avatar dress and a job suit. Each committed
change rebuilds the next snapshot's appearance (#3100: the kept
capture is keyed on the record's CommitRevision, which the door
bumps), and the rebuilt appearance settles again afterwards.
================
*/
func TestWorldSnapshotRebuildsForAvatarAndJobSuit(t *testing.T) {
	provider, deps, character, _ := appearanceFixture(t)
	before := provider.WorldSnapshot().Appearance
	if before == nil {
		t.Fatal("the snapshot lost its appearance")
	}
	if len(before.Avatars) != 0 || before.JobType != 0 {
		t.Fatalf("fresh appearance carries avatars %v and job %d, want none", before.Avatars, before.JobType)
	}

	deps.Mutate(character, "memo-test-avatar", func() {
		character.AvatarInventory = &enterworld.AvatarInventory{Capacity: 4,
			Rows: []enterworld.InventoryRow{{Slot: 0, RefObjID: 23401, TypeFlags: 0x1234}}}
	})
	dressed := provider.WorldSnapshot().Appearance
	if dressed == before {
		t.Fatal("an avatar change kept the old appearance")
	}
	if len(dressed.Avatars) != 1 || dressed.Avatars[0].RefObjID != 23401 {
		t.Fatalf("avatars = %+v, want the one dress", dressed.Avatars)
	}

	deps.Mutate(character, "memo-test-suit", func() {
		character.MissionInventory = append(character.MissionInventory,
			enterworld.InventoryRow{Slot: enterworld.JobSuitSlot, RefObjID: 4200, TypeFlags: 0x0bac})
		character.Job.Alias = "TraderJoe"
	})
	suited := provider.WorldSnapshot().Appearance
	if suited == dressed {
		t.Fatal("a job suit change kept the old appearance")
	}
	if suited.JobType != 1 || suited.Name != "TraderJoe" {
		t.Fatalf("suited appearance job = %d name = %q, want job 1 as TraderJoe", suited.JobType, suited.Name)
	}
	if settled := provider.WorldSnapshot().Appearance; settled != suited {
		t.Fatal("the rebuilt appearance did not settle")
	}
}

const benchSnapshotSessionCount = 100

/*
================
snapshotProvidersFixture

One hundred bound session worlds over one stored division, each with
the worn set the appearance fixture wears: the synthetic crowd the
tick's prepare leg snapshots every tick (#3100).
================
*/
func snapshotProvidersFixture(b *testing.B) []worldsession.SnapshotIntoProvider {
	b.Helper()
	dir := filepath.Join(b.TempDir(), "authority")
	authority, err := store.Open(dir, store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		b.Fatalf("store.Open: %v", err)
	}
	b.Cleanup(authority.Close)
	modelRef, bodyShape := int64(1907), int64(1)
	for i := 0; i < benchSnapshotSessionCount; i++ {
		seed := &enterworld.Character{Name: "benchhero" + strconv.Itoa(i), ModelCodename: "CHAR_EU_MAN", ModelRef: &modelRef,
			BodyShapeByte: &bodyShape, MissionInventory: raceEquipSet(0)}
		if err := authority.CreateCharacter(doorTestDivision, "test-account-"+strconv.Itoa(i), seed); err != nil {
			b.Fatalf("CreateCharacter: %v", err)
		}
	}
	deps := &enterworld.Deps{Characters: authority.Characters()}
	deps.MutateCharacter = func(c *enterworld.Character, label string, fn func()) {
		authority.MutateCharacter(c, label, fn)
	}
	deps.ReadCharacter = func(divisionID string, fn func()) {
		authority.ReadCharacters(divisionID, func([]*enterworld.Character) { fn() })
	}
	deps.Guilds = authority.Guilds()
	rt := NewRuntime(deps, simulation.NewWorldStore())
	rt.ActionSpeed = func(string, string) float32 { return 125 }
	rt.SpawnSkills = func(string, string) []enterworld.EntrySkill { return nil }
	providers := make([]worldsession.SnapshotIntoProvider, 0, benchSnapshotSessionCount)
	for _, character := range authority.Characters().CharactersForDivision(doorTestDivision) {
		recorded := &recordedWorld{}
		rt.WorldBound(recorded, doorTestDivision, character)
		provider, ok := recorded.snapshot.(worldsession.SnapshotIntoProvider)
		if !ok {
			b.Fatalf("the session world offers no retained snapshot: %T", recorded.snapshot)
		}
		providers = append(providers, provider)
	}
	if len(providers) != benchSnapshotSessionCount {
		b.Fatalf("bound %d session worlds, want %d", len(providers), benchSnapshotSessionCount)
	}
	return providers
}

/*
================
BenchmarkWorldSnapshot100Sessions

The prepare leg's per-tick bill at 100 synthetic sessions (#3100):
every session's retained snapshot slot refilled once, appearances
settled. The number this pins is what the kept-appearance and
kept-companion work has to hold down as the crowd grows.
================
*/
func BenchmarkWorldSnapshot100Sessions(b *testing.B) {
	providers := snapshotProvidersFixture(b)
	slots := make([]simulation.SessionSnapshot, benchSnapshotSessionCount)
	for i, provider := range providers {
		provider.WorldSnapshotInto(&slots[i])
		if slots[i].Appearance == nil {
			b.Fatal("the snapshot lost its appearance")
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		for i, provider := range providers {
			provider.WorldSnapshotInto(&slots[i])
		}
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
