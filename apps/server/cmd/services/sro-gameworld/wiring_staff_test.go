/*
===========================================================================

wiring_staff_test.go - the staff book and the host allowlist become one
projection at boot

===========================================================================
*/
package main

import (
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/staff"
)

/*
================
TestStaffSourcesProjectAtBoot

A book grant gives its character the GM bit and exactly its boxes; an
allowlist GM holds every box and wears the gm role; a revoked grant and
a character in neither source leave boot without the bit, even when the
record still carried it.
================
*/
func TestStaffSourcesProjectAtBoot(t *testing.T) {
	dir := t.TempDir()
	options := store.Options{DefaultSkills: func(string, []uint32) ([]uint32, error) { return []uint32{1}, nil }}
	authority, err := store.Open(dir, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(authority.Close)
	stale := &domain.Character{Name: "Stale", ModelCodename: "CHAR_CH_MAN_ADVENTURER", GMPrivilege: true, GMCapabilities: uint32(staff.All)}
	for _, c := range []*domain.Character{
		{Name: "Hundred", ModelCodename: "CHAR_CH_MAN_ADVENTURER"},
		{Name: "Host", ModelCodename: "CHAR_CH_MAN_ADVENTURER"},
		{Name: "Revoked", ModelCodename: "CHAR_CH_MAN_ADVENTURER"},
		stale,
	} {
		if err := authority.CreateCharacter(sweepTestShard, "alpha", c); err != nil {
			t.Fatal(err)
		}
	}
	book, err := staff.OpenBook(filepath.Join(dir, staff.FileName), time.Now, store.WriteFileAtomic)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Hundred", "Revoked"} {
		if _, _, err := book.Grant(staff.Request{Division: sweepTestShard, Character: name, Role: staff.GMRole,
			Capabilities: []string{"teleport", "find_user"}, Operator: "staff-a"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := book.Revoke(sweepTestShard, "Revoked"); err != nil {
		t.Fatal(err)
	}
	sources := staffSources{book: book, allowlist: []store.GMIdentity{{DivisionID: sweepTestShard, CharacterName: "Host", Capabilities: uint32(staff.All)}}}
	authority.ReconcileGMPrivilege(sources.identities(sweepTestShard))

	want := map[string]uint32{"Hundred": uint32(staff.TeleportSelf | staff.FindUser), "Host": uint32(staff.All), "Revoked": 0, "Stale": 0}
	authority.ReadCharacters(sweepTestShard, func(characters []*domain.Character) {
		for _, c := range characters {
			bits, ok := want[c.Name]
			if !ok {
				continue
			}
			if c.GMPrivilege != (bits != 0) || c.GMCapabilities != bits {
				t.Errorf("%s after boot: gm %v bits %b, want %b", c.Name, c.GMPrivilege, c.GMCapabilities, bits)
			}
		}
	})
	if !sources.isAllowlisted(sweepTestShard, "host") || sources.isAllowlisted(sweepTestShard, "Hundred") {
		t.Fatal("allowlist membership is wrong")
	}
	roster := book.Roster(sweepTestShard, sources.allowlisted(sweepTestShard))
	if len(roster) != 2 {
		t.Fatalf("roster = %+v", roster)
	}
}
