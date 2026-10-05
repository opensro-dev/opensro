/*
===========================================================================

avatar_test.go - the smith's avatar magic option grant

Expectations follow CBless_AvatarItemWithNPC (505810/5079C0) and the
v1.150 magicoption rows for avatar parts: STR is 1..1, HP 150..150.

===========================================================================
*/

package alchemy

import (
	"errors"
	"testing"

	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
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
