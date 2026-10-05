package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/simulation"
)

const testOfficialGid uint32 = 919521

/*
================
fortressGuilds

A guild store that knows one guild.
================
*/
type fortressGuilds struct {
	enterworld.GuildStore
	guild   domain.GuildRecord
	members []domain.GuildMemberRecord
}

/*
================
fortressGuilds.Guild
================
*/
func (g fortressGuilds) Guild(_ string, id int64) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
	if id != g.guild.ID {
		return domain.GuildRecord{}, nil, false
	}
	return g.guild, g.members, true
}

/*
================
officialFixture

The shipped portal and fortress catalogs, the Jangan official selected, a
level-3 guild of seven whose master is the character, enough gold, and
the request period open.
================
*/
func officialFixture(t *testing.T) (*Runtime, *enterworld.Character) {
	t.Helper()
	rt, c := fortressPortalFixture(t, testFieldFortGate)
	rt.NpcRoster = append(rt.NpcRoster, simulation.NpcDef{ObjectID: testOfficialGid, RefObjID: 1, Codename: "NPC_CH_FORTRESS_OFFICIAL"})
	rt.Selected.Set(testDivision, c.Name, testOfficialGid)
	guildID := int64(41)
	c.GuildID = &guildID
	members := []domain.GuildMemberRecord{{CharID: c.ID, Grade: 0}}
	for i := 1; i < 7; i++ {
		members = append(members, domain.GuildMemberRecord{CharID: c.ID + int64(i), Grade: 1})
	}
	rt.Guilds = fortressGuilds{guild: domain.GuildRecord{ID: guildID, Level: 3}, members: members}
	gold := int64(6000000)
	c.Gold = &gold
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodRequest, true)
	return rt, c
}

/*
================
applyFrame
================
*/
func applyFrame(subtype, kind uint8) []byte {
	return wire.NewWriter(10).U32(testOfficialGid).U8(subtype).U32(1).U8(kind).Payload()
}

/*
================
TestGuildMasterAppliesForTheFortressWar

633910 -> the 0xA result: the fee leaves the master's gold, the guild is
registered, the master hears [7][1][fortress][kind] and everyone hears
0x3887 0x0C. Withdrawing (633C40) removes the registration.
================
*/
func TestGuildMasterAppliesForTheFortressWar(t *testing.T) {
	rt, c := officialFixture(t)
	out := rt.HandleFortressInteraction(testDivision, c, applyFrame(fortressApply, 0))
	want := wire.NewWriter(7).U8(fortressApply).U8(1).U32(1).U8(0).Payload()
	if len(out.Frames) < 2 || !bytes.Equal(out.Frames[0].Payload, want) || out.Frames[1].Payload[0] != fortressWarApplied {
		t.Fatalf("apply answer %+v", out.Frames)
	}
	if *c.Gold != 1000000 {
		t.Fatalf("fee not charged: %d", *c.Gold)
	}
	if record, _ := rt.Fortresses.Get(testDivision, 1); record.Applicants[41] != fortress.RequestAttack || len(record.Applicants) != 1 {
		t.Fatalf("guild not registered: %+v", record.Applicants)
	}
	// 633910 tests the fee before the existing registration.
	gold := int64(6000000)
	c.Gold = &gold
	again := rt.HandleFortressInteraction(testDivision, c, applyFrame(fortressApply, 0))
	if !bytes.Equal(again.Frames[0].Payload, []byte{fortressApply, 2, fortressErrApplied}) || *c.Gold != 6000000 {
		t.Fatalf("second application %+v", again.Frames)
	}
	out = rt.HandleFortressInteraction(testDivision, c, applyFrame(fortressWithdraw, 0))
	if out.Frames[0].Payload[1] != 1 || out.Frames[1].Payload[0] != fortressWarWithdrawn {
		t.Fatalf("withdraw answer %+v", out.Frames)
	}
	if record, _ := rt.Fortresses.Get(testDivision, 1); len(record.Applicants) != 0 {
		t.Fatal("withdrawal kept the registration")
	}
}

/*
================
TestFortressApplicationRefusals

633910's checks, each in its native order.
================
*/
func TestFortressApplicationRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(rt *Runtime, c *enterworld.Character)
		code  uint8
	}{
		{"outside the request period", func(rt *Runtime, _ *enterworld.Character) {
			rt.Fortresses.SetPeriod(testDivision, fortress.PeriodRequest, false)
		}, fortressErrPeriod},
		{"another NPC", func(rt *Runtime, c *enterworld.Character) {
			rt.Selected.Set(testDivision, c.Name, 1)
		}, fortressErrWrongOfficial},
		{"no guild", func(_ *Runtime, c *enterworld.Character) { c.GuildID = nil }, fortressErrNoGuild},
		{"not the master", func(rt *Runtime, _ *enterworld.Character) {
			guilds := rt.Guilds.(fortressGuilds)
			guilds.members[0].Grade = 2
		}, fortressErrNotMaster},
		{"as an ally without a union", nil, fortressErrAlliance},
		{"a level-2 guild", func(rt *Runtime, _ *enterworld.Character) {
			guilds := rt.Guilds.(fortressGuilds)
			guilds.guild.Level = 2
			rt.Guilds = guilds
		}, fortressErrGuildLevel},
		{"six members", func(rt *Runtime, _ *enterworld.Character) {
			guilds := rt.Guilds.(fortressGuilds)
			guilds.members = guilds.members[:6]
			rt.Guilds = guilds
		}, fortressErrGuildMembers},
		{"short of the fee", func(_ *Runtime, c *enterworld.Character) {
			gold := int64(4999999)
			c.Gold = &gold
		}, fortressErrGold},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, c := officialFixture(t)
			kind := uint8(0)
			if tc.setup == nil {
				kind = 1
			} else {
				tc.setup(rt, c)
			}
			out := rt.HandleFortressInteraction(testDivision, c, applyFrame(fortressApply, kind))
			if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{fortressApply, 2, tc.code}) {
				t.Fatalf("answer %+v, want code %#x", out.Frames, tc.code)
			}
		})
	}
}

/*
================
TestOfficialOpensTheApplicationWindow

[6][1], the war start as a SYSTEMTIME, then the guild's application.
================
*/
func TestOfficialOpensTheApplicationWindow(t *testing.T) {
	rt, c := officialFixture(t)
	loc := time.FixedZone("shard", 8*3600)
	rt.FortressWindows = func(int64) time.Time { return time.Date(2026, 10, 7, 20, 0, 0, 0, loc) }
	status := wire.NewWriter(2).U32(testOfficialGid).U8(fortressWarStatus).Payload()
	// SYSTEMTIME 2026-10-07 (Wednesday) 20:00:00.000.
	quads := []byte{0xea, 0x07, 10, 0, 3, 0, 7, 0, 20, 0, 0, 0, 0, 0, 0, 0}
	out := rt.HandleFortressInteraction(testDivision, c, status)
	if want := append(append([]byte{6, 1}, quads...), 0); len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, want) {
		t.Fatalf("status before applying %x", out.Frames[0].Payload)
	}
	rt.HandleFortressInteraction(testDivision, c, applyFrame(fortressApply, 0))
	out = rt.HandleFortressInteraction(testDivision, c, status)
	want := append(append([]byte{6, 1}, quads...), 1, 1, 0, 0, 0, 0)
	if !bytes.Equal(out.Frames[0].Payload, want) {
		t.Fatalf("status after applying %x", out.Frames[0].Payload)
	}
}
