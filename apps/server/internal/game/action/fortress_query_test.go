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
	"opensro.online/server/internal/game/enterworld"
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
scheduleGuilds

A guild store that knows the named guilds (the one-guild stub cannot
name two applicants).
================
*/
type scheduleGuilds struct {
	enterworld.GuildStore
	records map[int64]domain.GuildRecord
}

/*
================
scheduleGuilds.Guild
================
*/
func (g scheduleGuilds) Guild(_ string, id int64) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
	record, ok := g.records[id]
	return record, nil, ok
}

/*
================
TestFortressScheduleNamesAttackAndAllyKinds

The v1.150 schedule reader (754A40) takes the two war times, a count
and, per applicant, its name, level and kind - there is no separate
"an applicant exists" byte on this wire, so the kind byte (0 an
attack, 1 an ally) is what the jobs window reads. Both kinds reach it.
================
*/
func TestFortressScheduleNamesAttackAndAllyKinds(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.Selected.Set(testDivision, c.Name, 17)
	rt.NpcRoster[0].Services = simulation.NpcServices(0).With(simulation.NpcServiceFortressManager)
	rt.Fortresses = fortress.New([]fortress.Catalog{{ID: 1}})
	rt.Fortresses.SetApplication(testDivision, 1, 41, fortress.RequestAlly, true)
	rt.Fortresses.SetApplication(testDivision, 1, 42, fortress.RequestAttack, true)
	rt.Guilds = scheduleGuilds{records: map[int64]domain.GuildRecord{
		41: {ID: 41, Name: "Defenders", Level: 3},
		42: {ID: 42, Name: "Attackers", Level: 5},
	}}
	previous := time.Date(2026, 9, 30, 20, 0, 0, 0, time.UTC)
	next := previous.AddDate(0, 0, 7)
	rt.FortressWarDates = func(int64) (time.Time, time.Time) { return previous, next }
	request := wire.NewWriter(9).U32(17).U8(siege.ActionSchedule).U32(1).Payload()
	out := rt.HandleFortressInteraction(testDivision, c, request)
	want := wire.NewWriter(48).U8(siege.ActionSchedule).U8(1)
	writeSystemTime(want, previous)
	writeSystemTime(want, next)
	want.U8(2).U16(9).Bytes([]byte("Defenders")).U8(3).U8(uint8(fortress.RequestAlly))
	want.U16(9).Bytes([]byte("Attackers")).U8(5).U8(uint8(fortress.RequestAttack))
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, want.Payload()) {
		t.Fatalf("schedule answer %+v, want % x", out.Frames, want.Payload())
	}
}

/*
================
TestFortressTaxRateBelongsToTheHolder

During a war the captured fortress is held temporarily: the temporary
holder's master sets the tax rate, the old owner's master hears the
owner refusal - the levy door already judged it so.
================
*/
func TestFortressTaxRateBelongsToTheHolder(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.Selected.Set(testDivision, c.Name, 17)
	rt.NpcRoster[0].Services = simulation.NpcServices(0).With(simulation.NpcServiceFortressManager)
	rt.Fortresses = fortress.New([]fortress.Catalog{{ID: 1}})
	owner := int64(41)
	rt.Fortresses.Occupy(testDivision, 1, owner)
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodTax, true)
	if !rt.Fortresses.Capture(testDivision, 1, 42, 0) {
		t.Fatal("the capture did not stand")
	}
	rate := func(value uint16) []byte {
		return wire.NewWriter(11).U32(17).U8(siege.ActionTaxRate).U32(1).U16(value).Payload()
	}
	holder := int64(42)
	c.GuildID = &holder
	rt.Guilds = fortressGuilds{guild: domain.GuildRecord{ID: 42}, members: []domain.GuildMemberRecord{{CharID: c.ID, Grade: 0}}}
	out := rt.HandleFortressInteraction(testDivision, c, rate(20))
	if !bytes.Equal(out.Frames[0].Payload, []byte{siege.ActionTaxRate, 1, 20, 0}) {
		t.Fatalf("the temporary holder's master answered %+v", out.Frames)
	}
	if record, _ := rt.Fortresses.Get(testDivision, 1); record.TaxRate != 20 {
		t.Fatalf("the rate stands at %d", record.TaxRate)
	}
	c.GuildID = &owner
	rt.Guilds = fortressGuilds{guild: domain.GuildRecord{ID: 41}, members: []domain.GuildMemberRecord{{CharID: c.ID, Grade: 0}}}
	out = rt.HandleFortressInteraction(testDivision, c, rate(10))
	if !bytes.Equal(out.Frames[0].Payload, []byte{siege.ActionTaxRate, 2, 0x06}) {
		t.Fatalf("the old owner's master answered %+v", out.Frames)
	}
	if record, _ := rt.Fortresses.Get(testDivision, 1); record.TaxRate != 20 {
		t.Fatalf("the refused rate moved to %d", record.TaxRate)
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
