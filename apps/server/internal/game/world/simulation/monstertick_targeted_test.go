/*
===========================================================================

monstertick_targeted_test.go - private consequences reach their characters

===========================================================================
*/

package simulation

import "testing"

/*
================
TestMonsterPrivateFramesReachEachRecipient

An area attack carries one private tail per struck character; each goes
to that character's session alone, whatever object was struck.
================
*/
func TestMonsterPrivateFramesReachEachRecipient(t *testing.T) {
	first, second, bystander := playerSessionAt(1, 0, 0), playerSessionAt(2, 0, 0), playerSessionAt(3, 0, 0)
	first.SessionID, second.SessionID, bystander.SessionID = "first", "second", "bystander"
	push := &fakePusher{}
	deliverMonsterTargetFrames(monsterTestDivision, []MonsterPrivateFrames{
		{CharacterID: 1, Frames: []Frame{{Opcode: 0x3057}}},
		{CharacterID: 2, Frames: []Frame{{Opcode: 0x30d2}}},
	}, []SessionSnapshot{first, second, bystander}, push)
	if got := sessionFrames(push, "first"); len(got) != 1 || got[0].Opcode != 0x3057 {
		t.Fatalf("first got %+v", got)
	}
	if got := sessionFrames(push, "second"); len(got) != 1 || got[0].Opcode != 0x30d2 {
		t.Fatalf("second got %+v", got)
	}
	if got := sessionFrames(push, "bystander"); len(got) != 0 {
		t.Fatalf("bystander got %+v", got)
	}
}
