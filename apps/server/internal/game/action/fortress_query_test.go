/*
===========================================================================

fortress_query_test.go - manager dates and aide service admission

===========================================================================
*/
package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestFortressManagerQueriesExistingAuthority
================
*/
func TestFortressManagerQueriesExistingAuthority(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.Selected.Set(testDivision, c.Name, 17)
	rt.NpcRoster[0].Services = simulation.NpcServices(0).With(simulation.NpcServiceFortressManager)
	rt.Fortresses = fortress.New([]fortress.Catalog{{ID: 1}})
	rt.Fortresses.SetApplication(testDivision, 1, 41, fortress.RequestAlly, true)
	rt.Guilds = fortressGuilds{guild: domain.GuildRecord{ID: 41, Name: "Defenders", Level: 3}}
	previous := time.Date(2026, 9, 30, 20, 0, 0, 0, time.UTC)
	next := previous.AddDate(0, 0, 7)
	rt.FortressWarDates = func(int64) (time.Time, time.Time) { return previous, next }
	request := wire.NewWriter(9).U32(17).U8(siege.ActionSchedule).U32(1).Payload()
	out := rt.HandleFortressInteraction(testDivision, c, request)
	want := wire.NewWriter(48).U8(siege.ActionSchedule).U8(1)
	writeSystemTime(want, previous)
	writeSystemTime(want, next)
	want.U8(1).U16(9).Bytes([]byte("Defenders")).U8(3).U8(uint8(fortress.RequestAlly))
	if len(out.Frames) != 1 || out.Frames[0].Opcode != opFortressInteractionResult || !bytes.Equal(out.Frames[0].Payload, want.Payload()) {
		t.Fatalf("manager response: %+v", out.Frames)
	}
	rt.Selected.Clear(testDivision, c.Name)
	out = rt.HandleFortressInteraction(testDivision, c, request)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{siege.ActionSchedule, 2, fortressErrInvalid}) {
		t.Fatalf("unselected manager accepted: %+v", out.Frames)
	}
}

/*
================
TestFortressAideEntryIsAnAdmittedNoOp
================
*/
func TestFortressAideEntryIsAnAdmittedNoOp(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.Selected.Set(testDivision, c.Name, 17)
	request := wire.NewWriter(5).U32(17).U8(siege.ActionAide).Payload()
	out := rt.HandleFortressInteraction(testDivision, c, request)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{siege.ActionAide, 2, fortressErrInvalid}) {
		t.Fatalf("ordinary merchant admitted aide action: %+v", out.Frames)
	}
	rt.NpcRoster[0].Services = simulation.NpcServices(0).With(simulation.NpcServiceFortressAide)
	if out = rt.HandleFortressInteraction(testDivision, c, request); len(out.Frames) != 0 {
		t.Fatalf("aide fabricated an answer: %+v", out.Frames)
	}
	moveMerchantAway(rt, npcHitRange+10)
	if out = rt.HandleFortressInteraction(testDivision, c, request); len(out.Frames) != 1 {
		t.Fatalf("distant aide accepted: %+v", out.Frames)
	}
}

/*
================
TestFortressTaxAdmissionAndSignedWire
================
*/
func TestFortressTaxAdmissionAndSignedWire(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.Selected.Set(testDivision, c.Name, 17)
	rt.NpcRoster[0].Services = simulation.NpcServices(0).With(simulation.NpcServiceFortressManager)
	rt.Fortresses = fortress.New([]fortress.Catalog{{ID: 1}})
	guildID := int64(41)
	c.GuildID = &guildID
	rt.Fortresses.Occupy(testDivision, 1, guildID)
	rt.Guilds = fortressGuilds{guild: domain.GuildRecord{ID: guildID}, members: []domain.GuildMemberRecord{{CharID: c.ID, Grade: 0}}}
	request := wire.NewWriter(11).U32(17).U8(siege.ActionTaxRate).U32(1).U16(65516).Payload()
	out := rt.HandleFortressInteraction(testDivision, c, request)
	if !bytes.Equal(out.Frames[0].Payload, []byte{siege.ActionTaxRate, 2, 8}) {
		t.Fatalf("closed period response: %+v", out.Frames)
	}
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodTax, true)
	out = rt.HandleFortressInteraction(testDivision, c, request)
	if !bytes.Equal(out.Frames[0].Payload, []byte{siege.ActionTaxRate, 1, 236, 255}) {
		t.Fatalf("signed ratio response: %+v", out.Frames)
	}
	query := wire.NewWriter(9).U32(17).U8(siege.ActionTaxQuery).U32(1).Payload()
	out = rt.HandleFortressInteraction(testDivision, c, query)
	want := wire.NewWriter(16).U8(siege.ActionTaxQuery).U8(1).U32(1).U16(65516).U64(0).Payload()
	if !bytes.Equal(out.Frames[0].Payload, want) {
		t.Fatalf("tax query response: %+v", out.Frames)
	}
	for _, tc := range []struct {
		rate  uint16
		guild int64
		grade uint8
		error byte
	}{
		{21, 41, 0, 0x15}, {65515, 41, 0, 0x15}, {65516, 42, 0, 0x38},
		{20, 42, 0, 6}, {20, 41, 1, 7},
	} {
		guildID = tc.guild
		rt.Guilds = fortressGuilds{guild: domain.GuildRecord{ID: 41}, members: []domain.GuildMemberRecord{{CharID: c.ID, Grade: tc.grade}}}
		payload := wire.NewWriter(11).U32(17).U8(siege.ActionTaxRate).U32(1).U16(tc.rate).Payload()
		out = rt.HandleFortressInteraction(testDivision, c, payload)
		if !bytes.Equal(out.Frames[0].Payload, []byte{siege.ActionTaxRate, 2, tc.error}) {
			t.Fatalf("admission %+v: %+v", tc, out.Frames)
		}
	}
}
