/*
===========================================================================

staff_test.go - a staff change reaches the live character and every
client at once

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/staff"
)

/*
================
TestOperatorSetStaffAppliesLiveAndTellsEveryone

A grant flips the record's bit and capabilities through its door, sends
the character its 0x3FCC state and every player the 0x3FCD roster; a
revoke clears both and sends the emptied roster, so no relog is needed.
================
*/
func TestOperatorSetStaffAppliesLiveAndTellsEveryone(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	roster := []staff.RosterEntry{{Character: c.Name, Tag: "[GM]", NameColor: 0xFFFFD87A, ChatColor: 0xFFFFAEC3}}
	rt.StaffRoster = func(string) []staff.RosterEntry { return roster }
	var own, everyone []wire.Frame
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) {
		if name == c.Name {
			own = append(own, frames...)
		}
	}
	rt.PushShardPeerFrames = func(_, except string, frames []wire.Frame) {
		if except != "" {
			t.Fatalf("the roster skipped %q", except)
		}
		everyone = append(everyone, frames...)
	}

	if _, err := rt.OperatorStaffTarget(testDivision, "NoSuchName"); err == nil {
		t.Fatal("a missing character resolved")
	}
	name, err := rt.OperatorStaffTarget(testDivision, c.Name)
	if err != nil || name != c.Name {
		t.Fatalf("target = %q, %v", name, err)
	}
	if err := rt.OperatorSetStaff(testDivision, c.Name, true, staff.TeleportSelf|staff.FindUser); err != nil {
		t.Fatal(err)
	}
	if !c.GMPrivilege || staff.Capability(c.GMCapabilities) != staff.TeleportSelf|staff.FindUser {
		t.Fatalf("record after grant: gm %v bits %b", c.GMPrivilege, c.GMCapabilities)
	}
	wantState := []byte{1, byte(staff.FindUser | staff.TeleportSelf), 0, 0, 0}
	if len(own) != 1 || own[0].Opcode != OpStaffState || !bytes.Equal(own[0].Payload, wantState) {
		t.Fatalf("own frames after grant = %+v", own)
	}
	wantRoster := wire.NewWriter(0).U16(1).Str(c.Name).Str("[GM]").U32(0xFFFFD87A).U32(0xFFFFAEC3).Payload()
	if len(everyone) != 1 || everyone[0].Opcode != OpStaffRoster || !bytes.Equal(everyone[0].Payload, wantRoster) {
		t.Fatalf("roster frames after grant = %+v", everyone)
	}
	if frames := rt.StaffEntryFrames(testDivision, c); len(frames) != 2 || frames[0].Opcode != OpStaffRoster || frames[1].Opcode != OpStaffState {
		t.Fatalf("entry frames for staff = %+v", frames)
	}

	roster = nil
	if err := rt.OperatorSetStaff(testDivision, c.Name, false, staff.All); err != nil {
		t.Fatal(err)
	}
	if c.GMPrivilege || c.GMCapabilities != 0 {
		t.Fatalf("record after revoke: gm %v bits %b", c.GMPrivilege, c.GMCapabilities)
	}
	if len(own) != 2 || !bytes.Equal(own[1].Payload, []byte{0, 0, 0, 0, 0}) {
		t.Fatalf("own frames after revoke = %+v", own)
	}
	if len(everyone) != 2 || !bytes.Equal(everyone[1].Payload, []byte{0, 0}) {
		t.Fatalf("roster after the last revoke = %+v", everyone)
	}
	// A shard without staff owes an entering player nothing.
	if frames := rt.StaffEntryFrames(testDivision, c); len(frames) != 0 {
		t.Fatalf("entry frames without staff = %+v", frames)
	}
	if err := rt.OperatorSetStaff(testDivision, "NoSuchName", true, staff.All); err == nil {
		t.Fatal("a missing character was granted")
	}
}
