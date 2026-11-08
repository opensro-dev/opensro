/*
===========================================================================

setberserk_test.go - the port's /SETBERSERK on the GM lane

Port-only, not native (action/hwan.go): subcmd 0xF3, a sized ANSI name and a u8
Blue Berserk level. The lane decodes it exactly, forwards it to the action
owner, and echoes the level on success; the owner's refusal and a non-GM
sender both stop it.

===========================================================================
*/
package gmcommand

import (
	"bytes"
	"testing"
)

// berserkOwner records /SETBERSERK calls and answers with ok.
type berserkOwner struct {
	ok    bool
	calls int
	name  string
	level uint8
}

func (o *berserkOwner) ToggleGMBodyStatus(string, string, uint8) bool { return false }

func (o *berserkOwner) SetGMBerserkLevel(_, _, name string, level uint8) bool {
	o.calls++
	o.name, o.level = name, level
	return o.ok
}

func berserkFrame(name string, level uint8) []byte {
	frame := []byte{SubSetBerserk, byte(len(name)), 0}
	frame = append(frame, name...)
	return append(frame, level)
}

func TestSetBerserkDecodesNameAndLevelExactly(t *testing.T) {
	request, err := DecodeGmCommand(berserkFrame("Ayla", 4))
	if err != nil || request.Name != "Ayla" || request.BerserkLevel != 4 {
		t.Fatalf("decode = %+v, %v; want Ayla at level 4", request, err)
	}
	for _, frame := range [][]byte{{SubSetBerserk}, {SubSetBerserk, 4, 0, 'A', 'y', 'l', 'a'}, append(berserkFrame("Ayla", 4), 9)} {
		if _, err := DecodeGmCommand(frame); err == nil {
			t.Fatalf("decode % X accepted a wrong length", frame)
		}
	}
}

func TestSetBerserkForwardsAndEchoesTheLevel(t *testing.T) {
	gm := gmChar("Gm", true, 0x1000)
	owner := &berserkOwner{ok: true}
	out := HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, berserkFrame("Ayla", 3), owner)
	if owner.calls != 1 || owner.name != "Ayla" || owner.level != 3 {
		t.Fatalf("owner saw %d call(s) %q %d, want one for Ayla at 3", owner.calls, owner.name, owner.level)
	}
	if want := []byte{AckResultOK, SubSetBerserk, 3}; !bytes.Equal(out.Ack, want) {
		t.Fatalf("ack % X, want % X", out.Ack, want)
	}
	owner.ok = false
	out = HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, berserkFrame("Ayla", 9), owner)
	if want := []byte{AckResultFail, SubSetBerserk}; !bytes.Equal(out.Ack, want) || out.Refusal == "" {
		t.Fatalf("refused ack % X (%q), want % X with a reason", out.Ack, out.Refusal, want)
	}
}

func TestSetBerserkNeedsPrivilege(t *testing.T) {
	player := gmChar("Player", false, 0x1000)
	owner := &berserkOwner{ok: true}
	out := HandleGmCommand(testDeps(player), &stubPresence{}, testDivision, player, berserkFrame("Player", 6), owner)
	if owner.calls != 0 || out.Ack != nil || !out.PrivilegeDenied {
		t.Fatalf("a non-GM reached the owner: calls=%d ack=% X", owner.calls, out.Ack)
	}
}
