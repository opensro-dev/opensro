/*
===========================================================================

guildname_test.go - the guild name on roster rows and member updates

The party window's GUILD NAME column reads mask bit 0x40 of the masked
member row (CPartyMemberInfo_ReadMasked), a sized string between the position block and the
mastery pair.

===========================================================================
*/
package party

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestMemberRowEncoderCarriesGuildName
================
*/
func TestMemberRowEncoderCarriesGuildName(t *testing.T) {
	row := MemberRow{
		MemberID: 100001, Name: "alfa", ModelRefID: 1907, Level: 5,
		StatusNibbles: 0xAA, Region: 0x62A8, PosX: 960, PosY: 20, PosZ: 458,
		Guilded: true, Guild: "Nine",
		Masteries: true, PrimaryMastery: 258, SecondaryMastery: 273,
	}
	got := EncodeMaskedMemberRow(row)
	want := memberRowBytes(row)
	want[0] = MemberMaskFull | MemberMaskGuild | MemberMaskMastery
	want = append(want, concat(u16le(4), []byte("Nine"), u32le(258), u32le(273))...)
	if !bytes.Equal(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
}

/*
================
TestMemberUpdateCarriesGuildName

An empty guild still rides the bit so a member who left a guild clears.
================
*/
func TestMemberUpdateCarriesGuildName(t *testing.T) {
	row := MemberRow{MemberID: 7, Level: 3, Guilded: true, Guild: "Nine", Masteries: true, PrimaryMastery: 513, SecondaryMastery: 514}
	body := encodeMemberUpdate(row)
	if len(body) != 34 || body[5] != MemberMaskLevel|MemberMaskStatus|MemberMaskPosition|MemberMaskGuild|MemberMaskMastery {
		t.Fatalf("delta = % X", body)
	}
	if binary.LittleEndian.Uint16(body[20:22]) != 4 || string(body[22:26]) != "Nine" {
		t.Fatalf("guild = % X", body[20:26])
	}
	if binary.LittleEndian.Uint32(body[26:30]) != 513 || binary.LittleEndian.Uint32(body[30:34]) != 514 {
		t.Fatalf("pair = % X", body[26:])
	}
	row.Guild = ""
	if body = encodeMemberUpdate(row); len(body) != 30 || body[5]&MemberMaskGuild == 0 {
		t.Fatalf("guildless delta = % X", body)
	}
}

/*
================
TestMemberRowsCarryResolvedGuild

With the guild read installed every live row carries the bit; without it
(or for a vanished record) the row stays bare.
================
*/
func TestMemberRowsCarryResolvedGuild(t *testing.T) {
	guildID := int64(12)
	member := &domain.Character{ID: 1, Name: "Guilded", GuildID: &guildID}
	loner := &domain.Character{ID: 2, Name: "Loner"}
	runtime := NewRuntime(updateDeps{characters: []*domain.Character{member, loner}}, nil)
	if row := runtime.memberRowFor(testDivision, member); row.Guilded {
		t.Fatalf("row without a guild read = %+v", row)
	}
	runtime.UseMemberGuild(func(_ string, character *enterworld.Character) string {
		if character.GuildID != nil && *character.GuildID == guildID {
			return "Nine"
		}
		return ""
	})
	if row := runtime.memberRowFor(testDivision, member); !row.Guilded || row.Guild != "Nine" {
		t.Fatalf("member row = %+v", row)
	}
	if row := runtime.memberRowFor(testDivision, loner); !row.Guilded || row.Guild != "" {
		t.Fatalf("loner row = %+v", row)
	}
	if row := runtime.memberRowFor(testDivision, nil); row.Guilded {
		t.Fatalf("degraded row = %+v", row)
	}
}
