/*
===========================================================================

setspeed_test.go - the port's /SETSPEED on the GM lane

Port-only, not native (action/gmspeed.go): subcmd 0xF1 with a u16 percent.
The lane decodes it exactly, forwards it to the action owner, and echoes
the percent on success; the owner's refusal and a non-GM sender both stop
it.

===========================================================================
*/
package gmcommand

import (
	"bytes"
	"testing"
)

// speedOwner records /SETSPEED calls and answers with ok.
type speedOwner struct {
	ok      bool
	calls   int
	name    string
	percent uint16
}

func (o *speedOwner) ToggleGMBodyStatus(string, string, uint8) bool { return false }

func (o *speedOwner) SetGMSpeed(_, name string, percent uint16) bool {
	o.calls++
	o.name, o.percent = name, percent
	return o.ok
}

/*
================
TestSetSpeedDecodesExactlyThreeBytes
================
*/
func TestSetSpeedDecodesExactlyThreeBytes(t *testing.T) {
	request, err := DecodeGmCommand([]byte{SubSetSpeed, 0x2C, 0x01})
	if err != nil || request.SpeedPercent != 300 {
		t.Fatalf("decode = %+v, %v; want percent 300", request, err)
	}
	for _, frame := range [][]byte{{SubSetSpeed}, {SubSetSpeed, 1}, {SubSetSpeed, 1, 2, 3}} {
		if _, err := DecodeGmCommand(frame); err == nil {
			t.Fatalf("decode % X accepted a wrong length", frame)
		}
	}
}

/*
================
TestSetSpeedForwardsAndEchoesThePercent
================
*/
func TestSetSpeedForwardsAndEchoesThePercent(t *testing.T) {
	gm := gmChar("Gm", true, 0x1000)
	owner := &speedOwner{ok: true}
	out := HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, []byte{SubSetSpeed, 0x2C, 0x01}, owner)
	if owner.calls != 1 || owner.name != "Gm" || owner.percent != 300 {
		t.Fatalf("owner saw %d call(s) %q %d, want one for Gm at 300", owner.calls, owner.name, owner.percent)
	}
	if want := []byte{AckResultOK, SubSetSpeed, 0x2C, 0x01}; !bytes.Equal(out.Ack, want) {
		t.Fatalf("ack % X, want % X", out.Ack, want)
	}
	owner.ok = false
	out = HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, []byte{SubSetSpeed, 5, 0}, owner)
	if want := []byte{AckResultFail, SubSetSpeed}; !bytes.Equal(out.Ack, want) || out.Refusal == "" {
		t.Fatalf("refused ack % X (%q), want % X with a reason", out.Ack, out.Refusal, want)
	}
}

/*
================
TestSetSpeedNeedsPrivilege
================
*/
func TestSetSpeedNeedsPrivilege(t *testing.T) {
	player := gmChar("Player", false, 0x1000)
	owner := &speedOwner{ok: true}
	out := HandleGmCommand(testDeps(player), &stubPresence{}, testDivision, player, []byte{SubSetSpeed, 0x2C, 0x01}, owner)
	if out.Ack != nil || owner.calls != 0 {
		t.Fatalf("non-GM got ack % X and %d owner call(s), want silence and none", out.Ack, owner.calls)
	}
}
