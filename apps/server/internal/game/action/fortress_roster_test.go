/*
===========================================================================

fortress_roster_test.go - the fortress world's structure roster
(0x3887 0x0F)

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
rosterRow

One decoded roster row (client 76C870 case 0xF's read order).
================
*/
type rosterRow struct {
	zone, ref, hp uint32
	state         uint16
}

/*
================
decodeRoster

The fortress and rows of a 0x0F payload; ok is false for any other frame.
================
*/
func decodeRoster(t *testing.T, payload []byte) (uint32, []rosterRow, bool) {
	t.Helper()
	if len(payload) < 6 || payload[0] != siege.SubtypeStructureRoster {
		return 0, nil, false
	}
	r := wire.NewReader(payload[1:])
	fortressID, _ := r.U32()
	count, _ := r.U8()
	rows := make([]rosterRow, 0, count)
	for range int(count) {
		var row rosterRow
		row.zone, _ = r.U32()
		row.ref, _ = r.U32()
		row.hp, _ = r.U32()
		row.state, _ = r.U16()
		if guild, _ := r.U8(); guild != 0 {
			t.Fatalf("a roster row named a guild")
		}
		rows = append(rows, row)
	}
	if r.Remaining() != 0 {
		t.Fatalf("roster has %d trailing bytes", r.Remaining())
	}
	return fortressID, rows, true
}

/*
================
captureRosters

Collects every roster pushed to the fixture character.
================
*/
func captureRosters(t *testing.T, rt *Runtime, name string) *[][]rosterRow {
	t.Helper()
	var rosters [][]rosterRow
	rt.PushCharacterFrames = func(_, to string, frames []wire.Frame) {
		for _, frame := range frames {
			if frame.Opcode != opFortressWarState || to != name {
				continue
			}
			if _, rows, ok := decodeRoster(t, frame.Payload); ok {
				rosters = append(rosters, rows)
			}
		}
	}
	return &rosters
}

/*
================
TestEncodeStructureRoster3887

The roster's layout: subtype, fortress, count, then zone, reference, hit
points, state and the has-guild byte, with a headquarters' sized name.
================
*/
func TestEncodeStructureRoster3887(t *testing.T) {
	got := siege.EncodeStructureRoster3887(3, []siege.StructureRosterRow{
		{EventStructID: 84, RefObjID: 19553, HP: 900, State: 0},
		{EventStructID: 90, RefObjID: 19600, HP: 1, State: 4, GuildName: "Ab"},
	})
	want := wire.NewWriter(64).U8(0x0f).U32(3).U8(2).
		U32(84).U32(19553).U32(900).U16(0).U8(0).
		U32(90).U32(19600).U32(1).U16(4).U8(1).U16(2).Bytes([]byte("Ab")).Payload()
	if !bytes.Equal(got, want) {
		t.Fatalf("roster % x, want % x", got, want)
	}
}

/*
================
TestFortressRosterOnEntryAndEveryThirtySeconds

A player inside the fortress world gets the roster at entry and from the
siege tick every 30000 ms; the rows list each standing structure in zone
order with its reference and hit points. A player outside gets none.
================
*/
func TestFortressRosterOnEntryAndEveryThirtySeconds(t *testing.T) {
	rt, c, _, _ := constructionFixture(t)
	frames := rt.FortressRosterFrames(testDivision, c)
	if len(frames) != 1 {
		t.Fatalf("entry roster frames = %d, want 1", len(frames))
	}
	_, rows, ok := decodeRoster(t, frames[0].Payload)
	want := []rosterRow{{zone: 84, ref: 19553, hp: 900}, {zone: 85, ref: 19536, hp: 500}}
	if !ok || len(rows) != len(want) {
		t.Fatalf("entry roster %+v, want %+v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("entry roster row %d = %+v, want %+v", i, rows[i], want[i])
		}
	}
	rosters := captureRosters(t, rt, c.Name)
	now := rt.Now().UnixMilli()
	// The period is the reference server's thirty seconds, written out so
	// a change to fortressRosterPeriodMs fails here.
	rt.advanceFortressRosters(now)
	rt.advanceFortressRosters(now + 29999)
	if len(*rosters) != 1 {
		t.Fatalf("rosters within one period = %d, want 1", len(*rosters))
	}
	rt.advanceFortressRosters(now + 30000)
	if len(*rosters) != 2 {
		t.Fatalf("rosters after one period = %d, want 2", len(*rosters))
	}

	outside, field, _ := fortressFixtureWithPopulation(t, testFieldFortGate, monster.TemplateFromParts(nil, nil))
	if got := outside.FortressRosterFrames(testDivision, field); len(got) != 0 {
		t.Fatalf("a player outside a fortress got a roster: %+v", got)
	}
}

/*
================
TestStructureChangesResendTheRoster

The reference server resends the roster after the database result of a
construction and of a demolition: the built barricade joins it, the demolished one
leaves it.
================
*/
func TestStructureChangesResendTheRoster(t *testing.T) {
	rt, c, _, jangan := constructionFixture(t)
	rosters := captureRosters(t, rt, c.Name)
	if got := construct(rt, c, jangan, testBarricadeZone); len(got) < 2 || got[1] != 1 {
		t.Fatalf("construction answered % x", got)
	}
	if len(*rosters) != 1 {
		t.Fatalf("rosters after construction = %d, want 1", len(*rosters))
	}
	built := (*rosters)[0]
	if last := built[len(built)-1]; last.zone != testBarricadeZone || last.ref != testBarricadeRef || last.hp == 0 {
		t.Fatalf("roster after construction ends %+v", last)
	}
	barricade, ok := rt.structureOnZone(testDivision, rt.fortressWorlds()[jangan], testBarricadeZone)
	if !ok {
		t.Fatal("the barricade does not stand")
	}
	if got := dismantle(rt, c, siege.ActionDemolish, barricade.Gid, jangan); len(got) < 2 || got[1] != 1 {
		t.Fatalf("demolition answered % x", got)
	}
	if len(*rosters) != 2 {
		t.Fatalf("rosters after demolition = %d, want 2", len(*rosters))
	}
	for _, row := range (*rosters)[1] {
		if row.zone == testBarricadeZone {
			t.Fatalf("the demolished barricade stayed in the roster: %+v", (*rosters)[1])
		}
	}
}

/*
================
TestStructureUpgradeResendsTheRoster

An upgrade resends the roster to the fortress world, as the reference
server does after the upgrade's database result: the zone now names the
raised structure.
================
*/
func TestStructureUpgradeResendsTheRoster(t *testing.T) {
	rt, c, _, jangan, _, _ := upgradeFixture(t)
	rosters := captureRosters(t, rt, c.Name)
	if got := upgradeAnswer(t, upgradeRequest(rt, c, jangan, testPositionZone, 0)); len(got) < 2 || got[1] != 1 {
		t.Fatalf("upgrade answered % x", got)
	}
	if len(*rosters) != 1 {
		t.Fatalf("rosters after the upgrade = %d, want 1", len(*rosters))
	}
	for _, row := range (*rosters)[0] {
		if row.zone == testPositionZone {
			if row.ref != testPositionRef1 {
				t.Fatalf("roster names %d on zone %d, want the raised %d", row.ref, testPositionZone, testPositionRef1)
			}
			return
		}
	}
	t.Fatalf("the upgraded zone is missing from the roster: %+v", (*rosters)[0])
}
