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

	logtest "github.com/sirupsen/logrus/hooks/test"
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
	hours    []uint8
	recalled []string
	guilds   map[string]bool
	banned   []string
}

func (o *adminOwner) PlayerBannedGM(_, name string) {
	o.banned = append(o.banned, name)
}

func (*adminOwner) ToggleGMBodyStatus(string, string, uint8) bool { return false }

func (*adminOwner) WorldStatusGM(string) (uint32, uint32, uint32) { return 3, 250, 17 }

func (o *adminOwner) SetGameTimeGM(hour uint8) { o.hours = append(o.hours, hour) }

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
	owner := &adminOwner{}
	out := HandleGmCommand(testDeps(gm, player), presence, testDivision, gm, nameFrame(SubBan, "remo"), owner)
	if !bytes.Equal(out.Ack, []byte{1, SubBan}) || len(presence.ended) != 1 || presence.ended[0] != "Remo" {
		t.Fatalf("ban ack % X, ended %v", out.Ack, presence.ended)
	}
	// The website feed hears of the ban under the stored name (BanObserver).
	if len(owner.banned) != 1 || owner.banned[0] != "Remo" {
		t.Fatalf("ban observed %v", owner.banned)
	}
	presence.online["remo"] = false
	presence.ended = nil
	owner.banned = nil
	out = HandleGmCommand(testDeps(gm, player), presence, testDivision, gm, nameFrame(SubBan, "Remo"), owner)
	// Natively refused offline; the website feed still drops the player.
	if !bytes.Equal(out.Ack, EncodeAckFail(SubBan)) || len(presence.ended) != 0 || len(owner.banned) != 1 || owner.banned[0] != "Remo" {
		t.Fatalf("offline ban ack % X, ended %v, observed %v", out.Ack, presence.ended, owner.banned)
	}
	owner.banned = nil
	out = HandleGmCommand(testDeps(gm, player), presence, testDivision, gm, nameFrame(SubBan, "Ghost"), owner)
	if !bytes.Equal(out.Ack, EncodeAckFail(SubBan)) || len(owner.banned) != 0 {
		t.Fatalf("unknown ban ack % X, observed %v", out.Ack, owner.banned)
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
		// The account lookup is exact, as the server's who command does.
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

/*
================
TestBanLeavesAnOperatorTrail

#681: a ban names the GM, the player and the account in the operator log;
a refused one leaves nothing.
================
*/
func TestBanLeavesAnOperatorTrail(t *testing.T) {
	hook := logtest.NewGlobal()
	t.Cleanup(hook.Reset)
	gm, player, _ := adminCharacters()
	presence := &adminPresence{online: map[string]bool{"gm": true, "remo": true}}
	HandleGmCommand(testDeps(gm, player), presence, testDivision, gm, nameFrame(SubBan, "Ghost"), &adminOwner{})
	if len(hook.AllEntries()) != 0 {
		t.Fatalf("a refused ban logged %v", hook.AllEntries())
	}
	HandleGmCommand(testDeps(gm, player), presence, testDivision, gm, nameFrame(SubBan, "Remo"), &adminOwner{})
	var trail map[string]any
	for _, entry := range hook.AllEntries() {
		if entry.Data["gm"] != nil {
			trail = entry.Data
		}
	}
	if trail["gm"] != "Gm" || trail["character"] != "Remo" || trail["account"] != "remo_account" || trail["division"] != testDivision {
		t.Fatalf("ban trail %v", trail)
	}
}

/*
================
TestSetTimeMovesTheClockByTheGivenHour

509CF0 sends [0x0A, hour 0..23]; the owner moves the clock and the GM gets
[1, 0x0A]. A malformed frame is the silent decode refusal.
================
*/
func TestSetTimeMovesTheClockByTheGivenHour(t *testing.T) {
	gm, _, _ := adminCharacters()
	owner := &adminOwner{}
	out := HandleGmCommand(testDeps(gm), &adminPresence{}, testDivision, gm, []byte{SubSetTime, 13}, owner)
	if !bytes.Equal(out.Ack, []byte{1, SubSetTime}) || len(owner.hours) != 1 || owner.hours[0] != 13 {
		t.Fatalf("settime ack % X, hours %v", out.Ack, owner.hours)
	}
	for _, frame := range [][]byte{{SubSetTime}, {SubSetTime, 24}, {SubSetTime, 1, 2}} {
		if out := HandleGmCommand(testDeps(gm), &adminPresence{}, testDivision, gm, frame, owner); out.Ack != nil {
			t.Fatalf("malformed % X answered % X", frame, out.Ack)
		}
	}
	if len(owner.hours) != 1 {
		t.Fatalf("a malformed frame moved the clock: %v", owner.hours)
	}
}
