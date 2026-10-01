package match

import (
	"opensro.online/server/internal/domain"
	"reflect"
	"testing"
)

func TestPartyMasteriesSelectTrainedLevelsAndNativeIDTieOrder(t *testing.T) {
	for _, tc := range []struct {
		rows []domain.CharacterMastery
		want [2]uint32
	}{
		{nil, [2]uint32{}},
		{[]domain.CharacterMastery{{ID: 257, Level: 0}}, [2]uint32{}},
		{[]domain.CharacterMastery{{ID: 273, Level: 30}}, [2]uint32{273, 0}},
		{[]domain.CharacterMastery{{ID: 276, Level: 40}, {ID: 257, Level: 40}, {ID: 273, Level: 40}}, [2]uint32{257, 273}},
		{[]domain.CharacterMastery{{ID: 257, Level: 1}, {ID: 273, Level: 50}, {ID: 258, Level: 30}}, [2]uint32{273, 258}},
		{[]domain.CharacterMastery{{ID: 518, Level: 90}, {ID: 513, Level: 90}, {ID: 514, Level: 0}}, [2]uint32{513, 518}},
	} {
		character := &domain.Character{Masteries: tc.rows}
		before := append([]domain.CharacterMastery(nil), tc.rows...)
		got := partyApplicant(character)
		if [2]uint32{got.Primary, got.Secondary} != tc.want {
			t.Fatalf("%+v => %+v, want %v", tc.rows, got, tc.want)
		}
		if !reflect.DeepEqual(before, tc.rows) {
			t.Fatal("selection reordered authority")
		}
	}
}

func TestPartyPurposeEveryJobAndPurposeByte(t *testing.T) {
	for job := 0; job < 256; job++ {
		for purpose := 0; purpose < 256; purpose++ {
			want := job == 4 && (purpose == 0 || purpose == 1) || (job == 1 || job == 3) && purpose == 2 || job == 2 && purpose == 3
			if got := partyPurposeAllowed(uint8(job), uint8(purpose)); got != want {
				t.Fatalf("job %d purpose %d = %v", job, purpose, got)
			}
		}
	}
}

func TestPartyJobEveryTypeWordAndEquippedSlot(t *testing.T) {
	for flags := 0; flags <= 65535; flags++ {
		want := uint8(4)
		for job := 1; job <= 3; job++ {
			if flags == 0x3ac+(job<<11) || flags == 0x3ad+(job<<11) {
				want = uint8(job)
			}
		}
		c := &domain.Character{MissionInventory: []domain.InventoryRow{{Slot: 8, TypeFlags: uint16(flags)}}}
		if got := activePartyJob(c); got != want {
			t.Fatalf("word %04x => %d, want %d", flags, got, want)
		}
		c.MissionInventory[0].Slot = 13
		if activePartyJob(c) != 4 {
			t.Fatal("bag suit changed active job")
		}
	}
}

func TestPartyRegistrationRevalidatesChangedSuitForEveryPurpose(t *testing.T) {
	r := &Runtime{}
	level := int64(10)
	c := &domain.Character{Name: "Applicant", Level: &level}
	for job := uint8(1); job <= 4; job++ {
		c.MissionInventory = nil
		if job != 4 {
			c.MissionInventory = []domain.InventoryRow{{Slot: 8, TypeFlags: 0x3ac | uint16(job)<<11}}
		}
		for purpose := uint8(0); purpose < 4; purpose++ {
			request := PartyMatchRequest{Purpose: purpose, MinLevel: 1, MaxLevel: 90, Title: "Party"}
			_, refusal := r.preparePartyRegistration("fixture", c, request)
			if ok := refusal == 0; ok != partyPurposeAllowed(job, purpose) {
				t.Fatalf("job %d purpose %d admitted %v", job, purpose, ok)
			}
		}
	}
}

/*
================
TestPartyRegistrationRefusalCodesAreNative

sub_514170: a party member who is not the master gets 0x1D, a partyless
registrant below level 5 gets 0x0A; a level 4 member of a party may not
list it either, and the master lists at any level with the party's options.
================
*/
func TestPartyRegistrationRefusalCodesAreNative(t *testing.T) {
	type standing struct {
		partied, leader bool
		level           int64
		want            uint8
	}
	for _, c := range []standing{
		{partied: true, leader: false, level: 40, want: partyMatchErrNotPartyLeader},
		{partied: true, leader: false, level: 1, want: partyMatchErrNotPartyLeader},
		{partied: false, level: 4, want: partyMatchErrCreatorLevel},
		{partied: false, level: 5, want: 0},
		{partied: true, leader: true, level: 1, want: 0},
	} {
		r := &Runtime{PartyListingAuthority: func(string, string) (uint8, bool, bool) {
			return 3, c.partied, c.leader
		}}
		level := c.level
		character := &domain.Character{Name: "Applicant", Level: &level}
		request := PartyMatchRequest{TypeBits: 0, Purpose: 0, MinLevel: 1, MaxLevel: 90, Title: "Party"}
		got, refusal := r.preparePartyRegistration("fixture", character, request)
		if refusal != c.want {
			t.Fatalf("%+v: refusal 0x%02X, want 0x%02X", c, refusal, c.want)
		}
		if refusal == 0 && c.partied && got.TypeBits != 3 {
			t.Fatalf("%+v: listing kept type bits %d, want the party's 3", c, got.TypeBits)
		}
	}
}
