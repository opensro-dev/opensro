/*
===========================================================================

petname_test.go - a summoned pet is named once, by native rules

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	petNameTestGID  uint32 = 0x01020304
	petNameOtherGID uint32 = 0x0a0b0c0d
	petNameTestName        = "Fluffy"
)

/*
================
petNamePair

Two players of one division, each with a summoned, unnamed pet.
================
*/
func petNamePair(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.Character) {
	t.Helper()
	rt, _, owner, other := newPvpPair(t)
	owner.ActiveCOS = &enterworld.CharacterCOS{GID: petNameTestGID, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true}
	other.ActiveCOS = &enterworld.CharacterCOS{GID: petNameOtherGID, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true}
	return rt, owner, other
}

/*
================
petNameRequest

0x7571 [u32 cos gid][u16 len][ascii name].
================
*/
func petNameRequest(gid uint32, name string) []byte {
	return wire.NewWriter(6 + len(name)).U32(gid).Str(name).Payload()
}

/*
================
TestPetNameIsSetOnceAndPublished

A valid name is stored and published as 0x3508 kind 5 to the owner and
around the pet, with no 0xB571; a second name is refused with error 5 and
the first stays. The bytes are the client half's test vector.
================
*/
func TestPetNameIsSetOnceAndPublished(t *testing.T) {
	rt, owner, _ := petNamePair(t)
	out := rt.HandlePetName(testDivision, owner, petNameRequest(petNameTestGID, petNameTestName))
	want := []byte{0x04, 0x03, 0x02, 0x01, 0x05, 0x06, 0x00, 'F', 'l', 'u', 'f', 'f', 'y'}
	if len(out.Frames) != 1 || out.Frames[0].Opcode != 0x3508 || !bytes.Equal(out.Frames[0].Payload, want) {
		t.Fatalf("owner frames %+v, want 0x3508 % X", out.Frames, want)
	}
	if len(out.Around) != 1 || out.Around[0].SourceGID != petNameTestGID || len(out.Around[0].Frames) != 1 ||
		!bytes.Equal(out.Around[0].Frames[0].Payload, want) {
		t.Fatalf("around %+v, want the same frame around the pet", out.Around)
	}
	if owner.ActiveCOS.Name != petNameTestName {
		t.Fatalf("stored name %q", owner.ActiveCOS.Name)
	}
	again := rt.HandlePetName(testDivision, owner, petNameRequest(petNameTestGID, "Rex"))
	if len(again.Frames) != 1 || again.Frames[0].Opcode != 0xB571 || !bytes.Equal(again.Frames[0].Payload, []byte{2, 5, 0}) {
		t.Fatalf("rename answered %+v, want 0xB571 02 05 00", again.Frames)
	}
	if owner.ActiveCOS.Name != petNameTestName {
		t.Fatalf("a rename changed the name to %q", owner.ActiveCOS.Name)
	}
}

/*
================
TestPetNameRefusals

Each native refusal answers 0xB571 [2][u16 code] and names nothing.
================
*/
func TestPetNameRefusals(t *testing.T) {
	for _, tc := range []struct {
		label string
		gid   uint32
		name  string
		setup func(owner, other *enterworld.Character)
		code  uint16
	}{
		{"empty name", petNameTestGID, "", nil, petNameErrRefused},
		{"thirteen bytes", petNameTestGID, "Abcdefghijklm", nil, petNameErrRefused},
		{"one byte", petNameTestGID, "A", nil, petNameErrNotAllowed},
		{"space", petNameTestGID, "Fl uffy", nil, petNameErrNotAllowed},
		{"punctuation", petNameTestGID, "Fluffy!", nil, petNameErrNotAllowed},
		{"another player's pet", petNameOtherGID, petNameTestName, nil, petNameErrNotSummoned},
		{"no such pet", 0x77, petNameTestName, nil, petNameErrNotSummoned},
		{"unsummoned pet", petNameTestGID, petNameTestName,
			func(owner, _ *enterworld.Character) { owner.ActiveCOS.Summoned = false }, petNameErrNotSummoned},
		{"name in use, other case", petNameTestGID, petNameTestName,
			func(_, other *enterworld.Character) { other.ActiveCOS.Name = "fLUFFY" }, petNameErrTaken},
	} {
		rt, owner, other := petNamePair(t)
		if tc.setup != nil {
			tc.setup(owner, other)
		}
		out := rt.HandlePetName(testDivision, owner, petNameRequest(tc.gid, tc.name))
		want := []byte{2, byte(tc.code), byte(tc.code >> 8)}
		if len(out.Frames) != 1 || out.Frames[0].Opcode != 0xB571 || !bytes.Equal(out.Frames[0].Payload, want) || len(out.Around) != 0 {
			t.Fatalf("%s: answered %+v around %+v, want 0xB571 % X", tc.label, out.Frames, out.Around, want)
		}
		if owner.ActiveCOS.Name != "" {
			t.Fatalf("%s: named the pet %q", tc.label, owner.ActiveCOS.Name)
		}
	}
}

/*
================
TestPetNameMalformedRequestIsSilent

A request that does not decode answers nothing, as the hub's other
strict decoders do.
================
*/
func TestPetNameMalformedRequestIsSilent(t *testing.T) {
	rt, owner, _ := petNamePair(t)
	for _, payload := range [][]byte{nil, {1, 2, 3}, append(petNameRequest(petNameTestGID, "Fluffy"), 0)} {
		if out := rt.HandlePetName(testDivision, owner, payload); len(out.Frames) != 0 || owner.ActiveCOS.Name != "" {
			t.Fatalf("payload % X answered %+v", payload, out.Frames)
		}
	}
}

/*
================
TestPetNameNamesTheSummonerItemCopy

A summoned pet whose record is the bag item's copy is named there.
================
*/
func TestPetNameNamesTheSummonerItemCopy(t *testing.T) {
	rt, owner, _ := petNamePair(t)
	pet := owner.ActiveCOS
	owner.ActiveCOS = nil
	owner.MissionInventory = append(owner.MissionInventory, enterworld.InventoryRow{Slot: 40, RefObjID: 1, Codename: "ITEM_COS_P", Summon: pet})
	rt.HandlePetName(testDivision, owner, petNameRequest(petNameTestGID, petNameTestName))
	if pet.Name != petNameTestName {
		t.Fatalf("summoner item pet name %q", pet.Name)
	}
}

/*
================
TestPetNamePassesTheNameFilter

The native name filter: a pet name holding a filtered word answers 0x440E
and names nothing; a clean one still names the pet.
================
*/
func TestPetNamePassesTheNameFilter(t *testing.T) {
	rt, owner, _ := petNamePair(t)
	rt.NameFilter = testNameFilter(t)
	out := rt.HandlePetName(testDivision, owner, petNameRequest(petNameTestGID, "Rudeboy"))
	if len(out.Frames) != 1 || out.Frames[0].Opcode != 0xB571 || !bytes.Equal(out.Frames[0].Payload, []byte{2, 0x0e, 0x44}) {
		t.Fatalf("filtered name answered %+v, want 0xB571 02 0E 44", out.Frames)
	}
	if owner.ActiveCOS.Name != "" {
		t.Fatalf("a filtered name was stored: %q", owner.ActiveCOS.Name)
	}
	rt.HandlePetName(testDivision, owner, petNameRequest(petNameTestGID, petNameTestName))
	if owner.ActiveCOS.Name != petNameTestName {
		t.Fatalf("a clean name was refused: %q", owner.ActiveCOS.Name)
	}
}
