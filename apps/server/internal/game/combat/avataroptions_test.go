/*
===========================================================================

avataroptions_test.go - tests for avataroptions.go

===========================================================================
*/

package combat

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

// The MATTR_AVATAR_* rows magicoptionassign.txt gives TID 3.1.13 (v1.150).
var avatarOptionTags = []string{"stra", "inta", "hpa", "mpa", "hra", "era", "hprg", "mprg", "mdia", "dara", "drua"}

/*
================
avatarFixture

A level-10 character wearing one avatar dress in socket 1 with the given
options, and the catalogues that resolve it.
================
*/
func avatarFixture(options ...uint64) (*domain.Character, Catalogs) {
	dress := &enterworld.ItemRef{RefObjID: 7, Codename: "TEST_AVATAR_DRESS", TypeIDs: [4]int64{3, 1, 13, 2}}
	rows := optionRows()
	for i, tag := range avatarOptionTags {
		id := uint32(200 + i)
		rows[id] = &enterworld.MagicOptionRow{ParamID: id, OptionName: "MATTR_AVATAR_" + tag, Tag: optionTag(tag)}
	}
	c := &domain.Character{Level: pointer(10), Strength: pointer(40), Intellect: pointer(30),
		AvatarInventory: &domain.AvatarInventory{Capacity: 4, Rows: []domain.InventoryRow{{
			Slot: 1, RefObjID: dress.RefObjID, Codename: dress.Codename, MagicOptions: options,
		}}}}
	return c, Catalogs{Items: itemRefs{dress.Codename: dress}, MagicOptions: rows}
}

/*
================
TestWornAvatarOptionsRaiseTheStats

The server's avatar install adds each worn avatar's contributions like equipment's: a
dress blessed with avatar STR raises strength. Before, no avatar row was
ever read.
================
*/
func TestWornAvatarOptionsRaiseTheStats(t *testing.T) {
	c, catalogs := avatarFixture(encodedOption(200, 3))
	stats, _, err := PlayerStats(c, catalogs)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Strength != 43 {
		t.Fatalf("strength %v, want 40 + the dress's 3", stats.Strength)
	}
	c.AvatarInventory = nil
	bare, _, err := PlayerStats(c, catalogs)
	if err != nil || bare.Strength != 40 {
		t.Fatalf("without the dress strength %v (%v), want 40", bare.Strength, err)
	}
}

/*
================
TestWornAvatarHPOptionAddsFlatHP

The avatar HP and MP rows are flat increases, like the equipment hp
option (TestMagicOptionsWriteTheKeeper pins that one at +300): a dress
blessed with the fixed 150 HP raises max HP by exactly 150, not by a
percentage of the strength-fed pool. The MP row is its twin.
================
*/
func TestWornAvatarHPOptionAddsFlatHP(t *testing.T) {
	c, catalogs := avatarFixture(encodedOption(202, 150), encodedOption(203, 150))
	dressed, _, err := PlayerStats(c, catalogs)
	if err != nil {
		t.Fatal(err)
	}
	c.AvatarInventory = nil
	bare, _, err := PlayerStats(c, catalogs)
	if err != nil {
		t.Fatal(err)
	}
	dressedHP, _ := dressed.Param(3)
	bareHP, _ := bare.Param(3)
	if dressedHP != bareHP+150 {
		t.Fatalf("max HP with the dress %v, want the bare %v + 150", dressedHP, bareHP)
	}
	dressedMP, _ := dressed.Param(4)
	bareMP, _ := bare.Param(4)
	if dressedMP != bareMP+150 {
		t.Fatalf("max MP with the dress %v, want the bare %v + 150", dressedMP, bareMP)
	}
}

/*
================
TestAvatarRowWithoutReferenceRefuses

A worn avatar with options whose reference is missing is a broken record,
refused like an equipped one.
================
*/
func TestAvatarRowWithoutReferenceRefuses(t *testing.T) {
	c, catalogs := avatarFixture(encodedOption(200, 3))
	catalogs.Items = itemRefs{}
	if _, _, err := PlayerStats(c, catalogs); err == nil {
		t.Fatal("a worn avatar with no reference row was admitted")
	}
}

/*
================
TestAvatarOptionsNeverWriteActionSpeed

The action-speed-only evaluation (perf work on PlayerStatsWithModifiers)
assumes no item option writes parameter 0x8C. Every avatar option must keep
that true.
================
*/
func TestAvatarOptionsNeverWriteActionSpeed(t *testing.T) {
	for i, tag := range avatarOptionTags {
		c, catalogs := avatarFixture(encodedOption(uint32(200+i), 5))
		writes, err := avatarOptionWrites(c, catalogs.Items, catalogs.MagicOptions)
		if err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		if len(writes) == 0 {
			t.Fatalf("%s wrote nothing", tag)
		}
		for _, w := range writes {
			if w.Parameter == 0x8c {
				t.Fatalf("%s writes action speed 0x8C", tag)
			}
			if w.Source != avatarSourceBase+1 {
				t.Fatalf("%s write keyed to source %d, want the avatar socket's %d", tag, w.Source, avatarSourceBase+1)
			}
		}
	}
}
