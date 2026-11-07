/*
===========================================================================

forge_test.go - the port's /FORGE on the GM lane

Port-only, not native (action/forge.go): subcmd 0xF2 with a u16-sized
ANSI command text. The lane decodes it strictly, forwards it to the
action owner, answers the owner's guide lines one ack each (the first
on Ack, the rest on ExtraAcks), and keeps a non-GM sender silent.
===========================================================================
*/
package gmcommand

import (
	"bytes"
	"strings"
	"testing"
)

// forgeOwner records /FORGE calls and answers with lines/ok.
type forgeOwner struct {
	lines []string
	ok    bool
	calls int
	name  string
	text  string
}

func (o *forgeOwner) ToggleGMBodyStatus(string, string, uint8) bool { return false }

func (o *forgeOwner) ForgeGMItem(_, name, text string) ([]string, bool) {
	o.calls++
	o.name, o.text = name, text
	return o.lines, o.ok
}

// forgeFrame builds one 0xF2 request payload for text.
func forgeFrame(text string) []byte {
	payload := []byte{SubForge, 0, 0}
	payload[1] = byte(len(text))
	payload[2] = byte(len(text) >> 8)
	return append(payload, text...)
}

/*
================
TestForgeDecodesItsTextExactly
================
*/
func TestForgeDecodesItsTextExactly(t *testing.T) {
	request, err := DecodeGmCommand(forgeFrame("ITEM_X plus=1"))
	if err != nil || request.Forge != "ITEM_X plus=1" {
		t.Fatalf("decode = %+v, %v", request, err)
	}
	bad := map[string][]byte{
		"subcommand only": {SubForge},
		"short length":    {SubForge, 5},
		"empty text":      {SubForge, 0, 0},
		"trailing byte":   append(forgeFrame("ITEM_X"), 0x20),
		"short text":      {SubForge, 9, 0, 'I', 'T'},
		"control byte":    forgeFrame("ITEM_X\tplus=1"),
		"overlong text":   forgeFrame(strings.Repeat("A", ForgeTextMaxBytes+1)),
	}
	for name, frame := range bad {
		if _, err := DecodeGmCommand(frame); err == nil {
			t.Fatalf("%s decoded: % X", name, frame)
		}
	}
	if _, err := DecodeGmCommand(forgeFrame(strings.Repeat("A", ForgeTextMaxBytes))); err != nil {
		t.Fatalf("the maximum text refused: %v", err)
	}
}

/*
================
TestForgeForwardsAndAnswersGuideLines
================
*/
func TestForgeForwardsAndAnswersGuideLines(t *testing.T) {
	gm := gmChar("Gm", true, 0x1000)
	owner := &forgeOwner{ok: true, lines: []string{"forge ITEM_X: keys", "plus 0..255", "bound (always on; bound=0 refuses)"}}
	out := HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, forgeFrame("help ITEM_X"), owner)
	if owner.calls != 1 || owner.name != "Gm" || owner.text != "help ITEM_X" {
		t.Fatalf("owner saw %d call(s) %q %q", owner.calls, owner.name, owner.text)
	}
	if want := EncodeAckGuide(SubForge, "forge ITEM_X: keys"); !bytes.Equal(out.Ack, want) {
		t.Fatalf("ack % X, want % X", out.Ack, want)
	}
	if len(out.ExtraAcks) != 2 ||
		!bytes.Equal(out.ExtraAcks[0], EncodeAckGuide(SubForge, "plus 0..255")) ||
		!bytes.Equal(out.ExtraAcks[1], EncodeAckGuide(SubForge, "bound (always on; bound=0 refuses)")) {
		t.Fatalf("extra acks = % X", out.ExtraAcks)
	}
	owner.ok = false
	out = HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, forgeFrame("ITEM_X plus=1"), owner)
	if want := []byte{AckResultFail, SubForge}; !bytes.Equal(out.Ack, want) || out.Refusal == "" || len(out.ExtraAcks) != 0 {
		t.Fatalf("refused outcome = %+v, want ack % X with a reason", out, want)
	}
}

/*
================
TestForgeNeedsPrivilege
================
*/
func TestForgeNeedsPrivilege(t *testing.T) {
	player := gmChar("Player", false, 0x1000)
	owner := &forgeOwner{ok: true, lines: []string{"forged ITEM_X (bound)"}}
	out := HandleGmCommand(testDeps(player), &stubPresence{}, testDivision, player, forgeFrame("ITEM_X plus=1"), owner)
	if out.Ack != nil || len(out.ExtraAcks) != 0 || owner.calls != 0 {
		t.Fatalf("non-GM outcome = %+v after %d owner call(s), want silence and none", out, owner.calls)
	}
}
