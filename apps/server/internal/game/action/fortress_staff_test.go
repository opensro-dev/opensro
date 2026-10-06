/*
===========================================================================

fortress_staff_test.go - holder-only flags across world entry and war modes

===========================================================================
*/
package action

import (
	"bytes"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
)

/*
================
TestFortressHolderFlagsOutsideWar
================
*/
func TestFortressHolderFlagsOutsideWar(t *testing.T) {
	rt, _, owner, other, id := fortressBattlePair(t)
	guild := int64(41)
	owner.GuildID = &guild
	rt.Guilds = fortressGuilds{guild: domain.GuildRecord{ID: guild}}
	rt.Fortresses.Occupy(testDivision, id, guild)
	for _, war := range []bool{false, true} {
		rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, war)
		frames := rt.FortressHolderFrames(testDivision, owner)
		want := fortressHolderFlags(id, 0)
		if len(frames) != 1 || frames[0].Opcode != want.Opcode || !bytes.Equal(frames[0].Payload, want.Payload) {
			t.Fatalf("war=%v flags=%+v", war, frames)
		}
		if frames := rt.FortressHolderFrames(testDivision, other); len(frames) != 0 {
			t.Fatalf("guildless received %+v", frames)
		}
	}
	enemy := int64(42)
	other.GuildID = &enemy
	rt.Fortresses.Capture(testDivision, id, enemy, 0)
	if frames := rt.FortressHolderFrames(testDivision, owner); len(frames) != 0 {
		t.Fatalf("previous holder received %+v", frames)
	}
	rt.Guilds = fortressGuilds{guild: domain.GuildRecord{ID: enemy}}
	if frames := rt.FortressHolderFrames(testDivision, other); len(frames) != 1 {
		t.Fatalf("temporary holder missing flags %+v", frames)
	}
}

/*
================
TestFortressStaffManagerWireAndPayment
================
*/
func TestFortressStaffManagerWireAndPayment(t *testing.T) {
	merchant, seed := merchantFixture(t)
	seed.Gold = testInt64(60000)
	d := openDoorRuntime(t, t.TempDir(), seed)
	rt, c := d.rt, d.character
	rt.NpcRoster = merchant.NpcRoster
	rt.NpcSpawn = merchant.NpcSpawn
	rt.NpcRoster[0].Services = simulation.NpcServices(0).With(simulation.NpcServiceFortressManager)
	rt.Selected.Set(testDivision, c.Name, 17)
	rt.Guilds = d.authority.Guilds()
	id, err := rt.Guilds.CreateGuild(testDivision, domain.GuildRecord{Name: "StaffGuild", Level: 1, GP: 6000}, domain.GuildMemberRecord{CharID: c.ID, JID: uint32(c.ID), Name: c.Name, Grade: 0, Level: 1, RefObjID: 1907}, c)
	if err != nil {
		t.Fatal(err)
	}
	rt.Fortresses = fortress.New([]fortress.Catalog{{ID: 1}})
	if err := rt.Fortresses.Restore(testDivision, d.authority.Fortresses()); err != nil {
		t.Fatal(err)
	}
	rt.Fortresses.Occupy(testDivision, 1, id)
	request := wire.NewWriter(10).U32(17).U8(4).U32(1).U8(2).Payload()
	out := rt.HandleFortressInteraction(testDivision, c, request)
	if len(out.Frames) != 4 || out.Frames[0].Opcode != guild.OpGuildUpdatePush || !bytes.Equal(out.Frames[2].Payload, []byte{4, 1, 2}) || !bytes.Equal(out.Frames[3].Payload, fortressHolderFlags(1, 2).Payload) {
		t.Fatalf("hire response %+v", out.Frames)
	}
	if *c.Gold != 30000 {
		t.Fatalf("gold %d", *c.Gold)
	}
	row, _, _ := rt.Guilds.Guild(testDivision, id)
	if row.GP != 3000 {
		t.Fatalf("GP %d", row.GP)
	}
	out = rt.HandleFortressInteraction(testDivision, c, request)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{4, 2, 9}) || *c.Gold != 30000 {
		t.Fatalf("duplicate %+v gold=%d", out.Frames, *c.Gold)
	}
	query := wire.NewWriter(9).U32(17).U8(3).U32(1).Payload()
	out = rt.HandleFortressInteraction(testDivision, c, query)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{3, 1, 2}) {
		t.Fatalf("query %+v", out.Frames)
	}
	rt.Selected.Clear(testDivision, c.Name)
	out = rt.HandleFortressInteraction(testDivision, c, query)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{3, 2, 3}) {
		t.Fatalf("unselected query %+v", out.Frames)
	}
}
