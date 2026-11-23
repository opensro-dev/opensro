/*
===========================================================================

moderation_test.go - /MUTE, /SHADOWMUTE and /BLOCKLOGOUT and their undos

Frames are hand-rolled to the layout the port's client composes (wire.go);
the effects are read back from the mute book, the shadow-mute list and the
logout owner, never from this package's own reply text alone.

===========================================================================
*/
package gmcommand

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/moderation"
	"opensro.online/server/internal/game/social/shadowmute"
)

/*
================
restrictionHub

The transport hub's by-character restriction records.
================
*/
type restrictionHub struct{ set map[string]bool }

func (h *restrictionHub) SetCharacterRestriction(_, name string, _ uint8, _ [8]uint16) bool {
	h.set[name] = true
	return true
}

func (h *restrictionHub) ClearCharacterRestriction(_, name string, _ uint8) bool {
	delete(h.set, name)
	return true
}

/*
================
logoutHolds
================
*/
type logoutHolds struct{ held map[string]bool }

func (l *logoutHolds) SetLogoutBlockGM(_, name string, blocked bool) bool {
	if l.held[name] == blocked {
		return false
	}
	l.held[name] = blocked
	return true
}

/*
================
moderationOwner
================
*/
type moderationOwner struct{ desk *Moderation }

func (moderationOwner) ToggleGMBodyStatus(string, string, uint8) bool { return false }

func (o moderationOwner) GMModeration() *Moderation { return o.desk }

/*
================
writeFile

The tests' writer: a plain write stands in for the server's atomic one.
================
*/
func writeFile(path string, payload []byte) error {
	return os.WriteFile(path, payload, 0o600)
}

/*
================
sized

u16 length + bytes, the composer's string layout.
================
*/
func sized(text string) []byte {
	out := binary.LittleEndian.AppendUint16(nil, uint16(len(text)))
	return append(out, text...)
}

func muteFrame(name string, minutes uint16, clause, reason string) []byte {
	frame := nameFrame(SubChatMute, name)
	frame = binary.LittleEndian.AppendUint16(frame, minutes)
	frame = append(frame, sized(clause)...)
	return append(frame, sized(reason)...)
}

/*
================
moderationFixture
================
*/
type moderationFixture struct {
	deps   *enterworld.Deps
	gm     *enterworld.Character
	hub    *restrictionHub
	holds  *logoutHolds
	dir    string
	owner  moderationOwner
	gmPeer *enterworld.Character
}

func newModerationFixture(t *testing.T, enabled bool) moderationFixture {
	t.Helper()
	gm := gmChar("Gm", true, 0x1000)
	player := gmChar("Juke", false, 0x1000)
	player.AccountID = "Youssef59"
	peer := gmChar("OtherGm", true, 0x1000)
	peer.AccountID = "staff"
	at := time.Date(2026, 11, 23, 13, 23, 0, 0, time.UTC)
	hub := &restrictionHub{set: map[string]bool{}}
	dir := t.TempDir()
	book, err := moderation.OpenBook(filepath.Join(dir, moderation.MuteFileName), hub, func() time.Time { return at }, writeFile)
	if err != nil {
		t.Fatal(err)
	}
	holds := &logoutHolds{held: map[string]bool{}}
	desk := &Moderation{Enabled: enabled, Mutes: book, ShadowMutes: shadowmute.Open(dir, writeFile), Logout: holds,
		Now: func() time.Time { return at }}
	return moderationFixture{deps: testDeps(gm, player, peer), gm: gm, hub: hub, holds: holds, dir: dir,
		owner: moderationOwner{desk: desk}, gmPeer: peer}
}

func (f moderationFixture) run(payload []byte) Outcome {
	return HandleGmCommand(f.deps, nil, testDivision, f.gm, payload, f.owner)
}

/*
================
replyText

The text of a moderation reply, checking its frame.
================
*/
func replyText(t *testing.T, ack []byte, result, subcmd uint8) string {
	t.Helper()
	if len(ack) < 4 || ack[0] != result || ack[1] != subcmd {
		t.Fatalf("reply % X, want result %d subcmd 0x%02X", ack, result, subcmd)
	}
	size := int(binary.LittleEndian.Uint16(ack[2:]))
	if len(ack) != 4+size {
		t.Fatalf("reply % X carries %d text bytes, frame has %d", ack, size, len(ack)-4)
	}
	return string(ack[4:])
}

/*
================
TestMuteAndUnmuteReachTheBook
================
*/
func TestMuteAndUnmuteReachTheBook(t *testing.T) {
	f := newModerationFixture(t, true)
	text := replyText(t, f.run(muteFrame("juke", 30, "C1", "spamming trade chat")).Ack, AckResultOK, SubChatMute)
	if text != "Juke muted until 2026-11-23 13:53 UTC" || !f.hub.set["Juke"] {
		t.Fatalf("mute reply %q, hub %v", text, f.hub.set)
	}
	if mute, ok := f.owner.desk.Mutes.Active(testDivision, "Juke"); !ok || mute.Operator != "gm:Gm" || mute.Clause != "C1" {
		t.Fatalf("book holds %+v %v", mute, ok)
	}
	// The book's own rules reach the GM as the refusal text.
	if text := replyText(t, f.run(muteFrame("Juke", 30, "C1", "no")).Ack, AckResultFail, SubChatMute); !strings.Contains(text, "reason") {
		t.Fatalf("short reason refusal %q", text)
	}
	if text := replyText(t, f.run(nameFrame(SubChatUnmute, "Juke")).Ack, AckResultOK, SubChatUnmute); text != "Juke unmuted" || f.hub.set["Juke"] {
		t.Fatalf("unmute reply %q, hub %v", text, f.hub.set)
	}
	if text := replyText(t, f.run(nameFrame(SubChatUnmute, "Juke")).Ack, AckResultFail, SubChatUnmute); text != "Juke is not muted" {
		t.Fatalf("second unmute %q", text)
	}
}

/*
================
TestShadowMuteEditsTheListAndKeepsHandLines
================
*/
func TestShadowMuteEditsTheListAndKeepsHandLines(t *testing.T) {
	// The flag does not gate the shadow-mute pair (owner, 2026-11-23).
	f := newModerationFixture(t, false)
	path := filepath.Join(f.dir, shadowmute.FileName)
	if err := os.WriteFile(path, []byte("# hand list\nahm8192\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	list := f.owner.desk.ShadowMutes
	frame := append(nameFrame(SubShadowMute, "Juke"), sized("rmt spam")...)
	if text := replyText(t, f.run(frame).Ack, AckResultOK, SubShadowMute); text != "Juke's account shadow-muted" {
		t.Fatalf("shadow-mute reply %q", text)
	}
	if !list.Muted("youssef59") || !list.Muted("ahm8192") {
		t.Fatal("the new account or the hand-listed one is not muted")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# shadowmute youssef59: Juke by GM Gm at 2026-11-23 13:23 UTC: rmt spam\nyoussef59\n") {
		t.Fatalf("file after add:\n%s", data)
	}
	if text := replyText(t, f.run(frame).Ack, AckResultFail, SubShadowMute); !strings.Contains(text, "already") {
		t.Fatalf("second shadow-mute %q", text)
	}
	if text := replyText(t, f.run(nameFrame(SubShadowUnmute, "juke")).Ack, AckResultOK, SubShadowUnmute); !strings.Contains(text, "no longer") {
		t.Fatalf("unshadow reply %q", text)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "# hand list\nahm8192\n" || list.Muted("youssef59") || !list.Muted("ahm8192") {
		t.Fatalf("file after remove:\n%q", data)
	}
}

/*
================
TestLogoutBlockPair
================
*/
func TestLogoutBlockPair(t *testing.T) {
	f := newModerationFixture(t, true)
	if text := replyText(t, f.run(nameFrame(SubBlockLogout, "Juke")).Ack, AckResultOK, SubBlockLogout); text != "Juke cannot log out" || !f.holds.held["Juke"] {
		t.Fatalf("block reply %q, holds %v", text, f.holds.held)
	}
	if text := replyText(t, f.run(nameFrame(SubBlockLogout, "Juke")).Ack, AckResultFail, SubBlockLogout); !strings.Contains(text, "already") {
		t.Fatalf("second block %q", text)
	}
	if text := replyText(t, f.run(nameFrame(SubAllowLogout, "Juke")).Ack, AckResultOK, SubAllowLogout); text != "Juke can log out again" || f.holds.held["Juke"] {
		t.Fatalf("allow reply %q, holds %v", text, f.holds.held)
	}
}

/*
================
TestModerationRefusals

The flag, a GM target, an unknown name, a non-GM sender and a malformed
frame each leave every store untouched.
================
*/
func TestModerationRefusals(t *testing.T) {
	off := newModerationFixture(t, false)
	for _, frame := range [][]byte{muteFrame("Juke", 30, "C1", "spamming trade chat"), nameFrame(SubBlockLogout, "Juke")} {
		if text := replyText(t, off.run(frame).Ack, AckResultFail, frame[0]); !strings.Contains(text, EnvGMModeration) {
			t.Fatalf("flag-off refusal %q", text)
		}
	}
	if len(off.hub.set) != 0 || len(off.holds.held) != 0 {
		t.Fatal("a flag-off command changed state")
	}

	f := newModerationFixture(t, true)
	if text := replyText(t, f.run(nameFrame(SubBlockLogout, "OtherGm")).Ack, AckResultFail, SubBlockLogout); text != "OtherGm is a GM" {
		t.Fatalf("GM target refusal %q", text)
	}
	if text := replyText(t, f.run(append(nameFrame(SubShadowMute, "OtherGm"), sized("")...)).Ack, AckResultFail, SubShadowMute); text != "OtherGm is a GM" || f.owner.desk.ShadowMutes.Muted("staff") {
		t.Fatalf("GM target shadow-mute %q", text)
	}
	if text := replyText(t, f.run(nameFrame(SubBlockLogout, "Nobody")).Ack, AckResultFail, SubBlockLogout); text != "no character named Nobody" {
		t.Fatalf("unknown target %q", text)
	}
	player := gmChar("Cheeky", false, 0x1000)
	if out := HandleGmCommand(f.deps, nil, testDivision, player, nameFrame(SubBlockLogout, "Juke"), f.owner); out.Ack != nil || !out.PrivilegeDenied {
		t.Fatalf("non-GM sender got %+v", out)
	}
	trailing := append(nameFrame(SubBlockLogout, "Juke"), 0)
	if out := f.run(trailing); out.Ack != nil {
		t.Fatalf("trailing byte answered %+v", out)
	}
	if len(f.hub.set) != 0 || len(f.holds.held) != 0 {
		t.Fatal("a refused command changed state")
	}
}
