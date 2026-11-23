/*
===========================================================================

capability_test.go - every GM subcommand needs a staff capability

===========================================================================
*/
package gmcommand

import (
	"bytes"
	"strings"
	"testing"

	"opensro.online/server/internal/game/staff"
)

/*
================
TestEverySubcommandNeedsACapability

Every subcommand byte the server honours has a capability row: a byte is
honoured when its one-byte body is a decode error (it has a layout) or the
dispatcher answers it other than "not honored". A command added without a
row fails here, and every row names a real capability.
================
*/
func TestEverySubcommandNeedsACapability(t *testing.T) {
	gm := gmChar("Hundred", true, 25000)
	honoured := 0
	for value := 0; value <= 0xFF; value++ {
		subcmd := uint8(value)
		request, err := DecodeGmCommand([]byte{subcmd})
		known := err != nil
		if !known {
			out := dispatchGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, []byte{subcmd}, request, nil)
			known = !strings.Contains(out.Refusal, "not honored by this server")
		}
		capability, mapped := subcommandCapability[subcmd]
		if known {
			honoured++
		}
		if known != mapped {
			t.Errorf("subcommand 0x%02X: honoured %v, capability row %v", subcmd, known, mapped)
		}
		if mapped && (capability == 0 || capability&staff.All != capability) {
			t.Errorf("subcommand 0x%02X maps to no real capability", subcmd)
		}
	}
	if honoured < 30 {
		t.Fatalf("only %d subcommands honoured; the probe is broken", honoured)
	}
}

/*
================
TestAStaffGrantOpensOnlyItsBoxes

A GM whose grant lacks make item gets the command's failure ack and no
effect; FINDUSER, which the grant carries, still answers. Unticking the
box on the live record refuses the very next command.
================
*/
func TestAStaffGrantOpensOnlyItsBoxes(t *testing.T) {
	gm := gmChar("Hundred", true, 25000)
	gm.GMCapabilities = uint32(staff.FindUser)
	target := gmChar("Remo", false, 25000)
	presence := &stubPresence{online: map[string]bool{"remo": true}}

	makeItem := []byte{SubMakeItem, 0x86, 0x5e, 0, 0, 20}
	out := HandleGmCommand(testDeps(gm, target), presence, testDivision, gm, makeItem)
	request, _ := DecodeGmCommand(makeItem)
	if !bytes.Equal(out.Ack, EncodeRequestFailure(request)) || !strings.Contains(out.Refusal, "staff grant") || out.PrivilegeDenied {
		t.Fatalf("make item without the box = %+v", out)
	}

	out = HandleGmCommand(testDeps(gm, target), presence, testDivision, gm, nameFrame(SubFindUser, "Remo"))
	if len(out.Ack) == 0 || out.Ack[0] != AckResultOK {
		t.Fatalf("find user with the box = %+v", out)
	}

	gm.GMCapabilities = 0
	out = HandleGmCommand(testDeps(gm, target), presence, testDivision, gm, nameFrame(SubFindUser, "Remo"))
	if !strings.Contains(out.Refusal, "staff grant") {
		t.Fatalf("find user after the box was unticked = %+v", out)
	}
}
