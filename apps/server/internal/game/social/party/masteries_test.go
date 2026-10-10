/*
===========================================================================

masteries_test.go - the mastery pair on roster rows and its operator switch

===========================================================================
*/
package party

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestMemberRowCarriesMasteryPairOnlyWhenAsked
================
*/
func TestMemberRowCarriesMasteryPairOnlyWhenAsked(t *testing.T) {
	row := MemberRow{
		MemberID: 100001, Name: "alfa", ModelRefID: 1907, Level: 5,
		StatusNibbles: 0xAA, Region: 0x62A8, PosX: 960, PosY: 20, PosZ: 458,
	}
	plain := EncodeMaskedMemberRow(row)
	if !bytes.Equal(plain, memberRowBytes(row)) {
		t.Fatalf("plain row changed: % X", plain)
	}
	row.Masteries, row.PrimaryMastery, row.SecondaryMastery = true, 258, 273
	got := EncodeMaskedMemberRow(row)
	want := append(memberRowBytes(row), concat(u32le(258), u32le(273))...)
	want[0] = MemberMaskFull | MemberMaskMastery
	if !bytes.Equal(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
}

/*
================
TestMemberUpdateCarriesMasteryPair
================
*/
func TestMemberUpdateCarriesMasteryPair(t *testing.T) {
	row := MemberRow{MemberID: 7, Level: 3, Masteries: true, PrimaryMastery: 513, SecondaryMastery: 514}
	body := encodeMemberUpdate(row)
	if len(body) != 28 || body[5] != MemberMaskLevel|MemberMaskStatus|MemberMaskPosition|MemberMaskMastery {
		t.Fatalf("delta = % X", body)
	}
	if binary.LittleEndian.Uint32(body[20:24]) != 513 || binary.LittleEndian.Uint32(body[24:28]) != 514 {
		t.Fatalf("pair = % X", body[20:])
	}
	row.Masteries = false
	if body = encodeMemberUpdate(row); len(body) != 20 || body[5]&MemberMaskMastery != 0 {
		t.Fatalf("plain delta = % X", body)
	}
}

/*
================
TestMemberUpdatesRepublishWhenAMasteryChanges
================
*/
func TestMemberUpdatesRepublishWhenAMasteryChanges(t *testing.T) {
	first := &domain.Character{ID: 1, Name: "MastFirst", Masteries: []domain.CharacterMastery{{ID: 257, Level: 2}}}
	second := &domain.Character{ID: 2, Name: "MastSecond"}
	runtime := NewRuntime(updateDeps{characters: []*domain.Character{first, second}}, nil)
	runtime.UseMasteries(true)
	_, refusal := runtime.Registry().Form(testDivision,
		Member{MemberID: simulation.PlayerObjectID(1), Name: first.Name},
		Member{MemberID: simulation.PlayerObjectID(2), Name: second.Name}, PartyOptionExpShare)
	if refusal != "" {
		t.Fatal(refusal)
	}
	world := simulation.DefaultWorldState(simulation.ChinaStartProfile())
	sessions := []simulation.SessionView{
		{DivisionID: testDivision, CharacterID: 1, World: world, WorldInstance: domain.DefaultWorldInstance},
		{DivisionID: testDivision, CharacterID: 2, World: world, WorldInstance: domain.DefaultWorldInstance},
	}
	if got := runtime.MemberUpdates(sessions, 1000); len(got) != 2 {
		t.Fatalf("initial deltas = %+v", got)
	}
	if got := runtime.MemberUpdates(sessions, 2000); len(got) != 0 {
		t.Fatal("unchanged masteries were republished")
	}
	first.Masteries = append(first.Masteries, domain.CharacterMastery{ID: 258, Level: 5})
	got := runtime.MemberUpdates(sessions, 3000)
	if len(got) != 2 || len(got[0].Frames) != 1 {
		t.Fatalf("mastery change deltas = %+v", got)
	}
	body := got[0].Frames[0].Payload
	if binary.LittleEndian.Uint32(body[20:24]) != 258 || binary.LittleEndian.Uint32(body[24:28]) != 257 {
		t.Fatalf("pair = % X", body[20:])
	}
}

/*
================
TestMasteriesSwitch
================
*/
func TestMasteriesSwitch(t *testing.T) {
	for raw, want := range map[string]bool{
		"": true, "on": true, "1": true, "true": true, "typo": true,
		"off": false, " OFF\n": false, "0": false, "false": false,
	} {
		if got := masteriesEnabled(raw); got != want {
			t.Errorf("%q: got %v want %v", raw, got, want)
		}
	}
}

/*
================
TestMemberRowsOmitMasteriesWhenSwitchedOff
================
*/
func TestMemberRowsOmitMasteriesWhenSwitchedOff(t *testing.T) {
	character := &domain.Character{ID: 1, Name: "Mast", Masteries: []domain.CharacterMastery{{ID: 257, Level: 2}}}
	runtime := NewRuntime(updateDeps{characters: []*domain.Character{character}}, nil)
	if row := runtime.memberRowFor(testDivision, character); row.Masteries {
		t.Fatal("pair rode a row with the switch off")
	}
	runtime.UseMasteries(true)
	row := runtime.memberRowFor(testDivision, character)
	if !row.Masteries || row.PrimaryMastery != 257 || row.SecondaryMastery != 0 {
		t.Fatalf("row = %+v", row)
	}
}
