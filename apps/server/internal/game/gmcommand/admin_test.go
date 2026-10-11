/*
===========================================================================

admin_test.go - /WORLDSTATUS, /BAN, /RECALLGUILD, /LIENAME and /REALNAME

The answers 751EC0 reads, the kick going to the presence owner, and every
miss refusing without side effects.

===========================================================================
*/
package gmcommand

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
adminPresence

Online characters, and the sessions a kick ended.
================
*/
type adminPresence struct {
	online map[string]bool
	ended  []string
}

func (p *adminPresence) OnlineByName(_, name string) bool { return p.online[strings.ToLower(name)] }

func (p *adminPresence) EndSessionByName(_, name string) bool {
	if !p.online[strings.ToLower(name)] {
		return false
	}
	p.ended = append(p.ended, name)
	return true
}

/*
================
adminOwner

The action-owned ports, recording what they were asked.
================
*/
type adminOwner struct {
	recalled []string
	guilds   map[string]bool
}

func (*adminOwner) ToggleGMBodyStatus(string, string, uint8) bool { return false }

func (*adminOwner) WorldStatusGM(string) (uint32, uint32, uint32) { return 3, 250, 17 }

func (o *adminOwner) RecallGuildGM(_, name, guild string) bool {
	o.recalled = append(o.recalled, name+">"+guild)
	return o.guilds[guild]
}

func adminCharacters() (*enterworld.Character, *enterworld.Character, *enterworld.Character) {
	gm := gmChar("Gm", true, 0x1000)
	player := gmChar("Remo", false, 0x1000)
	player.AccountID = "remo_account"
	nameless := gmChar("Plain", false, 0x1000)
	return gm, player, nameless
}

/*
================
TestWorldStatusAnswersThreeCounts

751EC0 reads [1, 4, i32 players, i32 NPC_Mob, i32 DroppedItem].
================
*/
func TestWorldStatusAnswersThreeCounts(t *testing.T) {
	gm, _, _ := adminCharacters()
	out := HandleGmCommand(testDeps(gm), &adminPresence{}, testDivision, gm, []byte{SubWorldStatus}, &adminOwner{})
	want := []byte{1, 4}
	for _, n := range []uint32{3, 250, 17} {
		want = binary.LittleEndian.AppendUint32(want, n)
	}
	if !bytes.Equal(out.Ack, want) {
		t.Fatalf("world status % X, want % X", out.Ack, want)
	}
}

/*
================
TestBanLogsOutAPlayerInTheWorld
================
*/
func TestBanLogsOutAPlayerInTheWorld(t *testing.T) {
	gm, player, _ := adminCharacters()
	presence := &adminPresence{online: map[string]bool{"gm": true, "remo": true}}
	out := HandleGmCommand(testDeps(gm, player), presence, testDivision, gm, nameFrame(SubBan, "remo"), &adminOwner{})
	if !bytes.Equal(out.Ack, []byte{1, SubBan}) || len(presence.ended) != 1 || presence.ended[0] != "Remo" {
		t.Fatalf("ban ack % X, ended %v", out.Ack, presence.ended)
	}
	presence.online["remo"] = false
	presence.ended = nil
	out = HandleGmCommand(testDeps(gm, player), presence, testDivision, gm, nameFrame(SubBan, "Remo"), &adminOwner{})
	if !bytes.Equal(out.Ack, EncodeAckFail(SubBan)) || len(presence.ended) != 0 {
		t.Fatalf("offline ban ack % X, ended %v", out.Ack, presence.ended)
	}
	out = HandleGmCommand(testDeps(gm, player), presence, testDivision, gm, nameFrame(SubBan, "Ghost"), &adminOwner{})
	if !bytes.Equal(out.Ack, EncodeAckFail(SubBan)) {
		t.Fatalf("unknown ban ack % X", out.Ack)
	}
}

/*
================
TestBanNeedsPrivilege

A non-GM's /BAN is silent and logs nobody out.
================
*/
func TestBanNeedsPrivilege(t *testing.T) {
	_, player, nameless := adminCharacters()
	presence := &adminPresence{online: map[string]bool{"remo": true, "plain": true}}
	out := HandleGmCommand(testDeps(player, nameless), presence, testDivision, nameless, nameFrame(SubBan, "Remo"), &adminOwner{})
	if out.Ack != nil || !out.PrivilegeDenied || len(presence.ended) != 0 {
		t.Fatalf("non-GM ban %+v, ended %v", out, presence.ended)
	}
}

/*
================
TestRecallGuildAsksTheOwner
================
*/
func TestRecallGuildAsksTheOwner(t *testing.T) {
	gm, _, _ := adminCharacters()
	owner := &adminOwner{guilds: map[string]bool{"OakWard": true}}
	out := HandleGmCommand(testDeps(gm), &adminPresence{}, testDivision, gm, nameFrame(SubRecallGuild, "OakWard"), owner)
	if !bytes.Equal(out.Ack, []byte{1, SubRecallGuild}) || len(owner.recalled) != 1 || owner.recalled[0] != "Gm>OakWard" {
		t.Fatalf("recall guild ack % X, asked %v", out.Ack, owner.recalled)
	}
	out = HandleGmCommand(testDeps(gm), &adminPresence{}, testDivision, gm, nameFrame(SubRecallGuild, "NoSuchGuild"), owner)
	if !bytes.Equal(out.Ack, EncodeAckFail(SubRecallGuild)) {
		t.Fatalf("missing guild ack % X", out.Ack)
	}
}

/*
================
TestLieNameAndRealNameLookUpPlayersInTheWorld

/LIENAME takes an account and answers its character; /REALNAME takes a
character and answers its account, or the native placeholder.
================
*/
func TestLieNameAndRealNameLookUpPlayersInTheWorld(t *testing.T) {
	gm, player, nameless := adminCharacters()
	deps := testDeps(gm, player, nameless)
	presence := &adminPresence{online: map[string]bool{"gm": true, "remo": true, "plain": true}}
	cases := []struct {
		frame []byte
		want  []byte
	}{
		{nameFrame(SubLieName, "remo_account"), EncodeAckGuide(SubLieName, "Remo")},
		{nameFrame(SubRealName, "remo"), EncodeAckGuide(SubRealName, "remo_account")},
		{nameFrame(SubRealName, "Plain"), EncodeAckGuide(SubRealName, noNickname)},
		// The account lookup is exact (521050).
		{nameFrame(SubLieName, "REMO_ACCOUNT"), EncodeRequestFailure(Request{Subcmd: SubLieName, Name: "REMO_ACCOUNT"})},
		{nameFrame(SubRealName, "Ghost"), EncodeRequestFailure(Request{Subcmd: SubRealName, Name: "Ghost"})},
	}
	for _, tc := range cases {
		out := HandleGmCommand(deps, presence, testDivision, gm, tc.frame, &adminOwner{})
		if !bytes.Equal(out.Ack, tc.want) {
			t.Fatalf("frame % X: ack % X, want % X", tc.frame, out.Ack, tc.want)
		}
	}
	presence.online["remo"] = false
	out := HandleGmCommand(deps, presence, testDivision, gm, nameFrame(SubLieName, "remo_account"), &adminOwner{})
	if !bytes.Equal(out.Ack, EncodeRequestFailure(Request{Subcmd: SubLieName, Name: "remo_account"})) {
		t.Fatalf("offline lie-name ack % X", out.Ack)
	}
}
