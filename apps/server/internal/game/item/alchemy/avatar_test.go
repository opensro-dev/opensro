/*
===========================================================================

avatar_test.go - the smith's avatar magic option grant

Expectations follow the native avatar bless and the v1.150
magicoption rows for avatar parts: STR is 1..1, HP 150..150.

===========================================================================
*/

package alchemy

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
avatarCatalog

A hat (two slots), a dress (four) and an avatar flag, with the hat and
dress assignment rows of magicoptionassign.txt.
================
*/
func avatarCatalog() *Catalog {
	return &Catalog{
		Items: map[string]Reference{
			"HAT":   {ID: 1, Name: "HAT", Flags: wire.PackTypeFlags(3, 1, 13, 1), Class: 1, MaxMagic: 2},
			"DRESS": {ID: 2, Name: "DRESS", Flags: wire.PackTypeFlags(3, 1, 13, 2), Class: 1, MaxMagic: 4},
			"FLAG":  {ID: 3, Name: "FLAG", Flags: wire.PackTypeFlags(3, 1, 13, 4), Class: 1, MaxMagic: 1},
		},
		Magic: map[uint16]Magic{
			246: {ID: 246, Name: "MATTR_AVATAR_STR", Degree: 1, Tag: 0x73747261, Params: [3]uint32{1, 1, 1}},
			247: {ID: 247, Name: "MATTR_AVATAR_INT", Degree: 1, Tag: 0x696e7461, Params: [3]uint32{1, 1, 1}},
			248: {ID: 248, Name: "MATTR_AVATAR_HR", Degree: 1, Tag: 0x687261, Params: [3]uint32{6, 5, 5}},
			250: {ID: 250, Name: "MATTR_AVATAR_HP", Degree: 1, Tag: 0x687061, Params: [3]uint32{1, 150, 150}},
		},
		AvatarOptions: map[uint8][]string{
			1: {"MATTR_AVATAR_STR", "MATTR_AVATAR_INT"},
			2: {"MATTR_AVATAR_STR", "MATTR_AVATAR_INT", "MATTR_AVATAR_HR", "MATTR_AVATAR_HP"},
		},
	}
}

/*
================
avatarItem
================
*/
func avatarItem(c *Catalog, name string, slot uint8, options ...uint64) inventory.Item {
	ref := c.Items[name]
	return inventory.Item{Slot: slot, RefObjID: ref.ID, Codename: name, TypeFlags: ref.Flags, Quantity: 1, MagicOptions: options}
}

/*
================
TestBlessAvatarGrantsAssignedOptions
================
*/
func TestBlessAvatarGrantsAssignedOptions(t *testing.T) {
	c := avatarCatalog()
	bag := []inventory.Item{avatarItem(c, "HAT", 13), avatarItem(c, "DRESS", 14)}
	// Every avatar row's range is a single value, so no draw happens.
	out, err := c.BlessAvatar(bag, 14, "MATTR_AVATAR_HP", nil)
	if err != nil || !out.Success || out.Target != 14 {
		t.Fatalf("grant = %+v, %v", out, err)
	}
	if got := out.Items[1].MagicOptions; len(got) != 1 || got[0] != 150<<32|250 {
		t.Fatalf("dress options = %#x", got)
	}
	if len(bag[1].MagicOptions) != 0 {
		t.Fatal("the plan mutated the caller's bag")
	}
	// A carried option is rewritten in place and costs no slot.
	full := []inventory.Item{avatarItem(c, "HAT", 13, 1<<32|246, 1<<32|247)}
	out, err = c.BlessAvatar(full, 13, "MATTR_AVATAR_STR", nil)
	if err != nil || len(out.Items[0].MagicOptions) != 2 {
		t.Fatalf("re-grant = %+v, %v", out, err)
	}
}

/*
================
TestBlessAvatarRefusals
================
*/
func TestBlessAvatarRefusals(t *testing.T) {
	c := avatarCatalog()
	c.Magic[249] = Magic{ID: 249, Name: "MATTR_AVATAR_ER", Degree: 1, Tag: 0x657261, Params: [3]uint32{6, 5, 5}}
	bag := []inventory.Item{
		avatarItem(c, "HAT", 13, 1<<32|246),
		avatarItem(c, "FLAG", 15),
		avatarItem(c, "DRESS", 16, 1<<32|246, 5<<32|248, 150<<32|250, 5<<32|249),
	}
	cases := []struct {
		name     string
		slot     uint8
		codename string
		want     Refusal
	}{
		{"equipment slot", 5, "MATTR_AVATAR_STR", AvatarBadSlot},
		{"empty slot", 20, "MATTR_AVATAR_STR", AvatarNoItem},
		{"avatar flag", 15, "MATTR_AVATAR_STR", AvatarNotAvatar},
		{"unassigned to the hat", 13, "MATTR_AVATAR_HP", AvatarWrong},
		{"unknown codename", 13, "MATTR_NOTHING", AvatarWrong},
		{"no free slot", 16, "MATTR_AVATAR_INT", AvatarFull},
	}
	for _, tc := range cases {
		_, err := c.BlessAvatar(bag, tc.slot, tc.codename, nil)
		var got Refusal
		if !errors.As(err, &got) || got != tc.want {
			t.Errorf("%s: err = %v, want refusal %d", tc.name, err, tc.want)
		}
	}
}

/*
================
TestLoadCatalogLoadsV188AvatarOptions

Checks that LoadCatalog always loads ids 370..376 with their native tags,
appends the seven dress codenames to the v1.150 dress row while leaving hat
and attachment rows unchanged, and refuses id collisions on magicoption.txt.
================
*/
func TestLoadCatalogLoadsV188AvatarOptions(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)

	catalog, err := LoadCatalog(dir, items)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	for _, overlay := range enterworld.AvatarV188MagicOptions() {
		m, exists := catalog.Magic[uint16(overlay.ParamID)]
		if !exists || m.Name != overlay.OptionName || m.Tag != overlay.Tag || m.Degree != int(overlay.Degree) {
			t.Fatalf("catalog missing/wrong id %d: got %+v exists=%v", overlay.ParamID, m, exists)
		}
	}

	rawAssign := map[uint8][]string{}
	for _, a := range enterworld.ReadTextdataFile(filepath.Join(dir, "magicoptionassign.txt")) {
		if len(a) < 5 || a[0] != "1" || a[2] != "13" {
			continue
		}
		var part uint8
		switch a[3] {
		case "1":
			part = 1
		case "2":
			part = 2
		case "3":
			part = 3
		default:
			continue
		}
		var names []string
		for _, name := range a[4:] {
			if name == "xxx" || name == "" {
				break
			}
			names = append(names, name)
		}
		rawAssign[part] = names
	}
	wantDress := append(slices.Clone(rawAssign[2]), enterworld.AvatarV188DressOptions()...)
	if !slices.Equal(catalog.AvatarOptions[2], wantDress) {
		t.Fatalf("dress options = %v, want %v", catalog.AvatarOptions[2], wantDress)
	}
	if !slices.Equal(catalog.AvatarOptions[1], rawAssign[1]) {
		t.Fatalf("hat options changed: got %v, want %v", catalog.AvatarOptions[1], rawAssign[1])
	}
	if !slices.Equal(catalog.AvatarOptions[3], rawAssign[3]) {
		t.Fatalf("attachment options changed: got %v, want %v", catalog.AvatarOptions[3], rawAssign[3])
	}

	// Refuse any id collision when magicoption.txt already defines id 373.
	tmp := t.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		if entry.Name() != "magicoption.txt" && !strings.HasPrefix(entry.Name(), "itemdata") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name() == "magicoption.txt" {
			dup := "\n" + strings.Join([]string{
				"1", "373", "MATTR_AVATAR_LUCK", "+", "1", "1.0", "0", "1819632481",
				"1", "1", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1819632481",
				"0", "0", "0", "0", "0", "avatar_mail", "1", "xxx", "0", "xxx", "0", "xxx", "0", "xxx", "0",
				"xxx", "0", "xxx", "0", "xxx", "0", "xxx", "0", "xxx", "0",
			}, "\t") + "\n"
			if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
				for _, r := range dup {
					data = append(data, byte(r), byte(r>>8))
				}
			} else {
				data = append(data, dup...)
			}
		}
		if err := os.WriteFile(filepath.Join(tmp, entry.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LoadCatalog(tmp, items); err == nil || !strings.Contains(err.Error(), "373") {
		t.Fatalf("expected id 373 collision error, got %v", err)
	}
}

/*
================
TestBlessAvatarV188StackingAndRebless

Checks that BlessAvatar treats tag+paramID as the option identity, as the
reference server's bless does: a dress accepts LUCK..LUCK_4 or MDIA..MDIA_4 as four distinct slots,
refuses a fifth option with AvatarFull (MaxMagic 4), and rewrites _2 in place.
================
*/
func TestBlessAvatarV188StackingAndRebless(t *testing.T) {
	c := avatarCatalog()
	c.Magic[254] = Magic{ID: 254, Name: "MATTR_AVATAR_MDIA", Degree: 1, Tag: 1835297121, Params: [3]uint32{1, 1, 1}}
	c.AvatarOptions[2] = append(c.AvatarOptions[2], "MATTR_AVATAR_MDIA")
	for _, overlay := range enterworld.AvatarV188MagicOptions() {
		id := uint16(overlay.ParamID)
		c.Magic[id] = Magic{
			ID: id, Name: overlay.OptionName, Degree: int(overlay.Degree), Tag: overlay.Tag, Params: *overlay.RangeWords,
		}
	}
	c.AvatarOptions[2] = append(c.AvatarOptions[2], enterworld.AvatarV188DressOptions()...)

	for _, tc := range []struct {
		name      string
		codenames []string
		wantIDs   []uint64
		rebless   string
		extra     string
	}{
		{
			name:      "4x luck",
			codenames: []string{"MATTR_AVATAR_LUCK", "MATTR_AVATAR_LUCK_2", "MATTR_AVATAR_LUCK_3", "MATTR_AVATAR_LUCK_4"},
			wantIDs:   []uint64{373, 374, 375, 376},
			rebless:   "MATTR_AVATAR_LUCK_2",
			extra:     "MATTR_AVATAR_MDIA",
		},
		{
			name:      "4x mdia",
			codenames: []string{"MATTR_AVATAR_MDIA", "MATTR_AVATAR_MDIA_2", "MATTR_AVATAR_MDIA_3", "MATTR_AVATAR_MDIA_4"},
			wantIDs:   []uint64{254, 370, 371, 372},
			rebless:   "MATTR_AVATAR_MDIA_2",
			extra:     "MATTR_AVATAR_LUCK",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bag := []inventory.Item{avatarItem(c, "DRESS", 14)}
			for _, name := range tc.codenames {
				out, err := c.BlessAvatar(bag, 14, name, nil)
				if err != nil {
					t.Fatalf("bless %s: %v", name, err)
				}
				bag = out.Items
			}
			got := bag[0].MagicOptions
			if len(got) != 4 {
				t.Fatalf("options count = %d, want 4 (%#x)", len(got), got)
			}
			for i, wantID := range tc.wantIDs {
				if got[i] != 1<<32|wantID {
					t.Fatalf("option[%d] = %#x, want %#x", i, got[i], 1<<32|wantID)
				}
			}

			// A fifth distinct option is refused by MaxMagic 4.
			_, err := c.BlessAvatar(bag, 14, tc.extra, nil)
			var refusal Refusal
			if !errors.As(err, &refusal) || refusal != AvatarFull {
				t.Fatalf("5th option err = %v, want AvatarFull", err)
			}

			// Re-blessing _2 rewrites slot 1 in place instead of needing a 5th slot.
			out, err := c.BlessAvatar(bag, 14, tc.rebless, nil)
			if err != nil {
				t.Fatalf("rebless %s: %v", tc.rebless, err)
			}
			if !slices.Equal(out.Items[0].MagicOptions, got) {
				t.Fatalf("rebless %s altered options: got %#x, want %#x", tc.rebless, out.Items[0].MagicOptions, got)
			}
		})
	}
}
