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
the character its 0x3FCC state and every player the 0x3FCD roster with
its 0x3FD3 nameplate switches; a revoke clears both and sends the emptied
roster, so no relog is needed.
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
	if len(everyone) != 2 || everyone[0].Opcode != OpStaffRoster || !bytes.Equal(everyone[0].Payload, wantRoster) ||
		everyone[1].Opcode != OpStaffNameplates || !bytes.Equal(everyone[1].Payload, []byte{0, 0}) {
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
	if len(everyone) != 4 || everyone[2].Opcode != OpStaffRoster || !bytes.Equal(everyone[2].Payload, []byte{0, 0}) {
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

/*
================
TestGMNameplateSwitchTellsEveryone

/NAMEPLATE OFF from a GM keeps the switch through the book's hook and
sends every player the roster with the 0x3FD3 frame naming the GM; an
entering player gets the frame too while anyone is off, and nothing once
everyone is back on. A player who is not staff is refused before the hook.
================
*/
func TestGMNameplateSwitchTellsEveryone(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	off := false
	rt.StaffRoster = func(string) []staff.RosterEntry {
		return []staff.RosterEntry{{Character: c.Name, Tag: "[GM]", NameplateOff: off}}
	}
	saved := 0
	rt.StaffSetNameplate = func(_, name string, on bool) (bool, error) {
		if name != c.Name {
			t.Fatalf("switched %q, want %q", name, c.Name)
		}
		saved++
		changed := off == on
		off = !on
		return changed, nil
	}
	var everyone []wire.Frame
	rt.PushShardPeerFrames = func(_, _ string, frames []wire.Frame) { everyone = append(everyone, frames...) }

	if rt.SetGMNameplate(testDivision, c.Name, false) || saved != 0 {
		t.Fatal("a player who is not staff switched a nameplate")
	}
	c.GMPrivilege = true
	if !rt.SetGMNameplate(testDivision, c.Name, false) || saved != 1 {
		t.Fatal("the GM's /NAMEPLATE OFF was refused")
	}
	wantOff := wire.NewWriter(0).U16(1).Str(c.Name).Payload()
	if len(everyone) != 2 || everyone[1].Opcode != OpStaffNameplates || !bytes.Equal(everyone[1].Payload, wantOff) {
		t.Fatalf("frames after OFF = %+v", everyone)
	}
	entry := rt.StaffEntryFrames(testDivision, c)
	if len(entry) != 3 || entry[1].Opcode != OpStaffNameplates || !bytes.Equal(entry[1].Payload, wantOff) {
		t.Fatalf("entry frames while off = %+v", entry)
	}
	// Off again changes nothing and tells nobody.
	if !rt.SetGMNameplate(testDivision, c.Name, false) || len(everyone) != 2 {
		t.Fatalf("a repeated OFF pushed %+v", everyone)
	}
	if !rt.SetGMNameplate(testDivision, c.Name, true) || len(everyone) != 4 || !bytes.Equal(everyone[3].Payload, []byte{0, 0}) {
		t.Fatalf("frames after ON = %+v", everyone)
	}
	if entry := rt.StaffEntryFrames(testDivision, c); len(entry) != 2 || entry[1].Opcode != OpStaffState {
		t.Fatalf("entry frames with every glow on = %+v", entry)
	}
}
