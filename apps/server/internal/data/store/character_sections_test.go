/*
===========================================================================

character_sections_test.go - the section skip keeps every save

The commit may skip a dirty character's encode only when the record it
would write is already on disk. These tests pin the proof from every
side: unchanged rows are not re-encoded, a change in any section (the
trailing rest section included) writes byte-identical rows, ephemeral
fields never force an encode, in-place mutations are seen, and the
staged copies follow the digests' retain-only-on-commit discipline.

===========================================================================
*/
package store

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
stageCommittedCopy

Create one character and run one empty scoped door, so the store holds
its committed copy. Returns the character and its copy's pointer, the
witness later tests use to prove no encode ran (a re-encode re-stages).
================
*/
func stageCommittedCopy(t *testing.T, s *Store) (*domain.Character, *domain.Character) {
	t.Helper()
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "section-account", c); err != nil {
		t.Fatal(err)
	}
	s.MutateCharacter(c, "sections-test", func() {})
	key := committedCharacterKey{division: testDivision, id: c.ID}
	copy, ok := s.committedRows[key]
	if !ok {
		t.Fatal("no committed copy staged after the first commit")
	}
	return c, copy
}

/*
================
TestCharacterSectionsCoverEveryPersistedField

Every persisted field of domain.Character sits in exactly one section.
The sections come from reflection over the JSON tags, so this holds by
construction; the test keeps the construction honest (a "-" tag or a
duplicate index would silently drop a field from comparison).
================
*/
func TestCharacterSectionsCoverEveryPersistedField(t *testing.T) {
	characterType := reflect.TypeOf(domain.Character{})
	persisted := 0
	for i := 0; i < characterType.NumField(); i++ {
		if characterType.Field(i).Tag.Get("json") != "-" {
			persisted++
		}
	}
	seen := map[int]bool{}
	covered := 0
	for _, section := range characterSections {
		for _, index := range section {
			if seen[index] {
				t.Fatalf("field %d appears in two sections", index)
			}
			seen[index] = true
			covered++
		}
	}
	if covered != persisted {
		t.Fatalf("sections cover %d fields, Character persists %d", covered, persisted)
	}
}

/*
================
TestCommitSkipsCharacterWhoseSectionsMatch
================
*/
func TestCommitSkipsCharacterWhoseSectionsMatch(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c, copy := stageCommittedCopy(t, s)
	before := readCharacterRecordRaw(t, s, testDivision, strings.ToLower(c.Name))

	s.MutateCharacter(c, "sections-test", func() {})

	key := committedCharacterKey{division: testDivision, id: c.ID}
	if s.committedRows[key] != copy {
		t.Fatal("an unchanged character was re-encoded (its copy was re-staged)")
	}
	if after := readCharacterRecordRaw(t, s, testDivision, strings.ToLower(c.Name)); after != before {
		t.Fatalf("record changed without a mutation:\nbefore: %s\nafter:  %s", before, after)
	}
}

/*
================
TestCommitWritesChangedSectionsByteIdentical

One mutation per section, the trailing rest section included. Every
one must write, and the row written must be the production encoder's
bytes for the mutated record - the save format is untouched.
================
*/
func TestCommitWritesChangedSectionsByteIdentical(t *testing.T) {
	mutations := []struct {
		name  string
		apply func(c *domain.Character)
	}{
		{"vitals", func(c *domain.Character) { c.CurrentHP = int64Ptr(4242) }},
		{"progress", func(c *domain.Character) { c.Gold = int64Ptr(777) }},
		{"cooldowns", func(c *domain.Character) { c.SkillActionRecoveryUntilMs = 99 }},
		{"inventory", func(c *domain.Character) { c.MissionInventory[0].StackCount = 5 }},
		{"skills", func(c *domain.Character) { c.Skills = append(c.Skills, 777) }},
		{"social", func(c *domain.Character) { c.Friends = append(c.Friends, domain.FriendRecord{ID: 9, Name: "pal"}) }},
		{"rest", func(c *domain.Character) { c.BreakNotice = true }},
		{"identity", func(c *domain.Character) { c.AnimationSetName = "other" }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			s := openTest(t, t.TempDir(), newTestClock())
			c, _ := stageCommittedCopy(t, s)
			s.MutateCharacter(c, "sections-test", func() { mutation.apply(c) })
			got := readCharacterRecordRaw(t, s, testDivision, strings.ToLower(c.Name))
			want, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("persisted record differs from Marshal after %s mutation:\npersisted: %s\nmarshal:   %s", mutation.name, got, want)
			}
		})
	}
}

/*
================
TestCommitIgnoresEphemeralFields

Fields tagged json:"-" are runtime state, not the saved row: churning
them must not force an encode.
================
*/
func TestCommitIgnoresEphemeralFields(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c, copy := stageCommittedCopy(t, s)

	s.MutateCharacter(c, "sections-test", func() {
		c.Aggressions = map[uint32]uint32{7: 3}
		c.BerserkUntilMs = 123456
		c.BattleUntilMs = 123456
	})

	key := committedCharacterKey{division: testDivision, id: c.ID}
	if s.committedRows[key] != copy {
		t.Fatal("an ephemeral-only change forced an encode")
	}
}

/*
================
TestCommitDetectsInPlaceMutations

The committed copy is detached: writing through the live record's own
slices and maps (no field reassignment) must still be seen. An aliased
copy would compare equal here and lose the save.
================
*/
func TestCommitDetectsInPlaceMutations(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c, _ := stageCommittedCopy(t, s)
	s.MutateCharacter(c, "sections-test", func() {
		c.Skills = []uint32{100, 200}
		c.ItemGroupCooldowns = map[uint32]int64{5: 50}
	})
	before := readCharacterRecordRaw(t, s, testDivision, strings.ToLower(c.Name))

	s.MutateCharacter(c, "sections-test", func() {
		c.Skills[0] = 999
		c.ItemGroupCooldowns[5] = 999
	})
	after := readCharacterRecordRaw(t, s, testDivision, strings.ToLower(c.Name))
	if after == before {
		t.Fatal("in-place slice and map mutations were not persisted")
	}
	want, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if after != string(want) {
		t.Fatalf("persisted record differs from Marshal:\npersisted: %s\nmarshal:   %s", after, want)
	}
}

/*
================
TestCommitDetectsInPlacePointerChildMutations

The committed copy must detach a persisted pointer child rather than
alias it: a door whose only change writes through such a pointer (the
kill door raising a most-wanted streak in place; the world's movement
mode is the same shape one level down) must still write the row. An
aliased copy compares the record against itself and the section skip
loses the save.
================
*/
func TestCommitDetectsInPlacePointerChildMutations(t *testing.T) {
	t.Run("most wanted streak", func(t *testing.T) {
		s := openTest(t, t.TempDir(), newTestClock())
		c, _ := stageCommittedCopy(t, s)
		s.MutateCharacter(c, "sections-test", func() { c.MostWanted = &domain.MostWanted{Streak: 1} })
		s.MutateCharacter(c, "sections-test", func() { c.MostWanted.Streak = 5 })
		got := readCharacterRecordRaw(t, s, testDivision, strings.ToLower(c.Name))
		want, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Fatalf("an in-place streak change was not persisted:\ngot:  %s\nwant: %s", got, want)
		}
	})
	t.Run("world movement mode", func(t *testing.T) {
		s := openTest(t, t.TempDir(), newTestClock())
		c, _ := stageCommittedCopy(t, s)
		s.MutateCharacter(c, "sections-test", func() { *c.World.MovementMode = 3 })
		got := readCharacterRecordRaw(t, s, testDivision, strings.ToLower(c.Name))
		want, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Fatalf("an in-place movement-mode change was not persisted:\ngot:  %s\nwant: %s", got, want)
		}
	})
}

/*
================
TestCommitDetectsMutationAfterDigestRefresh

The refresh staging (sections moved, bytes did not - a nil slice turned
empty) must stage a detached copy too: an aliased one would make the
next in-place mutation compare the record against itself and vanish.
================
*/
func TestCommitDetectsMutationAfterDigestRefresh(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c, _ := stageCommittedCopy(t, s)

	// nil -> empty: sections differ, the row's bytes do not
	// (omitempty drops both). The commit refreshes its copy without an
	// encode having to write anything.
	s.MutateCharacter(c, "sections-test", func() { c.Skills = []uint32{} })

	s.MutateCharacter(c, "sections-test", func() { c.MissionInventory[0].StackCount = 999 })

	want, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if got := readCharacterRecordRaw(t, s, testDivision, strings.ToLower(c.Name)); got != string(want) {
		t.Fatalf("mutation after a digest-refresh staging was lost:\ngot:  %s\nwant: %s", got, want)
	}
}

/*
================
TestReleaseCommittedCopyRestoresEncode
================
*/
func TestReleaseCommittedCopyRestoresEncode(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c, copy := stageCommittedCopy(t, s)

	s.ReleaseCommittedCopy(testDivision, c.ID)
	key := committedCharacterKey{division: testDivision, id: c.ID}
	if _, ok := s.committedRows[key]; ok {
		t.Fatal("release kept the committed copy")
	}

	s.MutateCharacter(c, "sections-test", func() {})
	if s.committedRows[key] == copy || s.committedRows[key] == nil {
		t.Fatal("a missing copy skipped the encode (it must mean encode-as-before)")
	}
}

/*
================
TestDoorTransactionRollbackRetainsNothing
================
*/
func TestDoorTransactionRollbackRetainsNothing(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c, copy := stageCommittedCopy(t, s)
	key := committedCharacterKey{division: testDivision, id: c.ID}

	tx, err := s.beginCharacterTxLocked()
	if err != nil {
		t.Fatal(err)
	}
	c.Gold = int64Ptr(31337)
	if err := tx.upsertCharacter(testDivision, c); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if s.committedRows[key] != copy {
		t.Fatal("a rolled-back door transaction retained its copy")
	}

	// The live record still carries the change, so the next ordinary
	// commit must write it (the stale copy convicts it).
	s.MutateCharacter(c, "sections-test", func() {})
	want, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if got := readCharacterRecordRaw(t, s, testDivision, strings.ToLower(c.Name)); got != string(want) {
		t.Fatalf("record after rollback differs from Marshal:\ngot:  %s\nwant: %s", got, want)
	}
}

/*
================
fatCharacter

A record at production row size (the bench fixture's Name-and-Gold
characters encode to a few hundred bytes; a played character's row is
ten kilobytes and up). The walk-versus-encode trade only shows there.
================
*/
func fatCharacter() *domain.Character {
	c := seededCharacter()
	c.Skills = make([]uint32, 0, 1000)
	for i := 0; i < 1000; i++ {
		c.Skills = append(c.Skills, uint32(1000+i))
	}
	c.CompletedQuestIds = make([]uint32, 0, 300)
	for i := 0; i < 300; i++ {
		c.CompletedQuestIds = append(c.CompletedQuestIds, uint32(5000+i))
	}
	row := c.MissionInventory[0]
	for i := 0; i < 49; i++ {
		row.RecordID = uint64(1000 + i)
		c.MissionInventory = append(c.MissionInventory, row)
	}
	return c
}

/*
================
BenchmarkCharacterSectionsUnchanged

The skip's whole cost on an unchanged fat row: the section walk.
================
*/
func BenchmarkCharacterSectionsUnchanged(b *testing.B) {
	c := fatCharacter()
	committed := domain.CloneCharacter(c)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if changed := characterSectionsChanged(c, committed); len(changed) != 0 {
			b.Fatalf("unchanged records differ in sections %v", changed)
		}
	}
}

/*
================
BenchmarkCharacterSectionsChangedRow

What a changed fat row pays on top of the encode it always paid: the
walk that convicts it plus the clone that becomes its committed copy.
================
*/
func BenchmarkCharacterSectionsChangedRow(b *testing.B) {
	c := fatCharacter()
	committed := domain.CloneCharacter(c)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.Gold = int64Ptr(int64(i))
		if changed := characterSectionsChanged(c, committed); len(changed) == 0 {
			b.Fatal("changed record compared equal")
		}
		committed = domain.CloneCharacter(c)
	}
}

/*
================
BenchmarkEncodeCharacterFat

The before column at the same row size: encode (plus SHA-256 in the
commit) is what an unchanged row used to cost every tick.
================
*/
func BenchmarkEncodeCharacterFat(b *testing.B) {
	c := fatCharacter()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := encodeCharacter(testDivision, c); err != nil {
			b.Fatal(err)
		}
	}
}

/*
================
TestDoorTransactionDeleteDropsCopy
================
*/
func TestDoorTransactionDeleteDropsCopy(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c, _ := stageCommittedCopy(t, s)
	key := committedCharacterKey{division: testDivision, id: c.ID}

	tx, err := s.beginCharacterTxLocked()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.deleteCharacter(testDivision, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.committedRows[key]; ok {
		t.Fatal("delete kept the committed copy")
	}
	if _, ok := s.committedDigests[key]; ok {
		t.Fatal("delete kept the committed digest")
	}
}

/*
================
TestDoorTransactionSkipsUnchangedRow
================
*/
func TestDoorTransactionSkipsUnchangedRow(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c, copy := stageCommittedCopy(t, s)
	key := committedCharacterKey{division: testDivision, id: c.ID}
	before := readCharacterRecordRaw(t, s, testDivision, strings.ToLower(c.Name))

	tx, err := s.beginCharacterTxLocked()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.upsertCharacter(testDivision, c); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if s.committedRows[key] != copy {
		t.Fatal("an unchanged door-transaction row was re-encoded")
	}
	if after := readCharacterRecordRaw(t, s, testDivision, strings.ToLower(c.Name)); after != before {
		t.Fatal("an unchanged door-transaction row was rewritten")
	}
}
