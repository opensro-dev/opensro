/*
===========================================================================

nameplate_test.go - the port's /NAMEPLATE ON and OFF on the GM lane

Port-only, not native (owner 2026-11-28): subcmd 0xFD and a u8 switch.
The lane decodes it exactly, forwards it to the action owner and answers
with the switch now in force; a refusal, a non-GM and a grant without the
nameplate power all stop it.

===========================================================================
*/
package gmcommand

import (
	"testing"

	"opensro.online/server/internal/game/staff"
)

// nameplateOwner records /NAMEPLATE calls and answers with ok.
type nameplateOwner struct {
	ok    bool
	calls int
	on    bool
}

func (o *nameplateOwner) ToggleGMBodyStatus(string, string, uint8) bool { return false }

func (o *nameplateOwner) SetGMNameplate(_, _ string, on bool) bool {
	o.calls++
	o.on = on
	return o.ok
}

func TestNameplateDecodesTheSwitchExactly(t *testing.T) {
	for _, tc := range []struct {
		frame []byte
		on    bool
	}{{[]byte{SubNameplate, 1}, true}, {[]byte{SubNameplate, 0}, false}} {
		request, err := DecodeGmCommand(tc.frame)
		if err != nil || request.NameplateOn != tc.on {
			t.Fatalf("decode % X = %+v, %v; want on %v", tc.frame, request, err, tc.on)
		}
	}
	for _, frame := range [][]byte{{SubNameplate}, {SubNameplate, 2}, {SubNameplate, 1, 0}} {
		if _, err := DecodeGmCommand(frame); err == nil {
			t.Fatalf("decode % X accepted a bad switch", frame)
		}
	}
}

func TestNameplateForwardsAndAnswers(t *testing.T) {
	gm := gmChar("Gm", true, 0x1000)
	owner := &nameplateOwner{ok: true}
	out := HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, []byte{SubNameplate, 0}, owner)
	if owner.calls != 1 || owner.on || len(out.Ack) < 2 || out.Ack[0] != AckResultOK || out.Ack[1] != SubNameplate {
		t.Fatalf("owner calls %d on %v ack % X", owner.calls, owner.on, out.Ack)
	}
	owner.ok = false
	out = HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, []byte{SubNameplate, 1}, owner)
	if len(out.Ack) < 1 || out.Ack[0] != AckResultFail || out.Refusal == "" {
		t.Fatalf("refused ack % X (%q)", out.Ack, out.Refusal)
	}
}

func TestNameplateNeedsStaffAndItsPower(t *testing.T) {
	player := gmChar("Player", false, 0x1000)
	owner := &nameplateOwner{ok: true}
	out := HandleGmCommand(testDeps(player), &stubPresence{}, testDivision, player, []byte{SubNameplate, 0}, owner)
	if owner.calls != 0 || !out.PrivilegeDenied {
		t.Fatalf("a non-GM reached the owner: calls=%d", owner.calls)
	}
	gm := gmChar("Gm", true, 0x1000)
	gm.GMCapabilities = uint32(staff.All &^ staff.Nameplate)
	HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, []byte{SubNameplate, 0}, owner)
	if owner.calls != 0 {
		t.Fatal("a grant without the nameplate power switched it")
	}
}
