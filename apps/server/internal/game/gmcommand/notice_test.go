/*
===========================================================================

notice_test.go - /NOTICE decoding, its refusals and the notice line

Frames are hand-rolled to the layout the port's client composes
(wire.go SubNotice). The notice line is compared with the chat lane's
own 0x3667 type 7 encoder, the one the client already presents.

===========================================================================
*/
package gmcommand

import (
	"bytes"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/staff"
)

/*
================
noticeFrame
================
*/
func noticeFrame(text string) []byte {
	return append([]byte{SubNotice}, sized(text)...)
}

/*
================
TestNoticeDecodesItsSizedText
================
*/
func TestNoticeDecodesItsSizedText(t *testing.T) {
	request, err := DecodeGmCommand(noticeFrame("Server restart in 5 minutes"))
	if err != nil || request.Subcmd != SubNotice || request.Text != "Server restart in 5 minutes" {
		t.Fatalf("decode: %+v %v", request, err)
	}
	bad := map[string][]byte{
		"trailing byte": append(noticeFrame("hi"), 0),
		"truncated":     noticeFrame("hello")[:4],
		"no length":     {SubNotice},
		"not UTF-8":     append([]byte{SubNotice, 2, 0}, 0xff, 0xfe),
		"over bound":    noticeFrame(strings.Repeat("a", NoticeTextMaxBytes+1)),
	}
	for label, frame := range bad {
		if _, err := DecodeGmCommand(frame); err == nil {
			t.Errorf("%s: accepted", label)
		}
	}
}

/*
================
TestNoticeSendsTheBannerLine

The outcome carries the chat lane's type 7 line and the GM's OK reply;
the text is trimmed.
================
*/
func TestNoticeSendsTheBannerLine(t *testing.T) {
	gm := gmChar("Gm", true, 0x1000)
	out := HandleGmCommand(testDeps(gm), nil, testDivision, gm, noticeFrame("  Event at Jangan  "))
	if out.Refusal != "" {
		t.Fatalf("refused: %s", out.Refusal)
	}
	if want := chat.EncodeChatNotice("Event at Jangan"); !bytes.Equal(out.Notice, want) {
		t.Fatalf("notice line % X, want % X", out.Notice, want)
	}
	if want := EncodeModerationReply(AckResultOK, SubNotice, "notice sent"); !bytes.Equal(out.Ack, want) {
		t.Fatalf("ack % X, want % X", out.Ack, want)
	}
}

/*
================
TestNoticeRefusesWithoutSendingAnything

Empty, too long, a control character, a grant without the box, a
non-GM: none of them produces a notice line. The GM's refusals carry a
reason; the non-GM's is silent.
================
*/
func TestNoticeRefusesWithoutSendingAnything(t *testing.T) {
	gm := gmChar("Gm", true, 0x1000)
	limited := gmChar("Helper", true, 0x1000)
	limited.GMCapabilities = uint32(staff.All &^ staff.Notice)
	player := gmChar("Player", false, 0x1000)
	deps := testDeps(gm, limited, player)
	cases := []struct {
		label  string
		sender *enterworld.Character
		frame  []byte
		reason string
	}{
		{"empty", gm, noticeFrame(""), "type the notice text"},
		{"blank", gm, noticeFrame("   "), "type the notice text"},
		{"control", gm, noticeFrame("line\nbreak"), "control character"},
		{"too long", gm, noticeFrame(strings.Repeat("a", chat.MaxChatNoticeUnits+1)), "101 characters"},
		{"no grant", limited, noticeFrame("hello"), ""},
		{"non-GM", player, noticeFrame("hello"), ""},
	}
	for _, c := range cases {
		out := HandleGmCommand(deps, nil, testDivision, c.sender, c.frame)
		if out.Notice != nil {
			t.Errorf("%s: sent a notice", c.label)
		}
		if out.Refusal == "" {
			t.Errorf("%s: not refused", c.label)
		}
		if c.reason != "" && !strings.Contains(string(out.Ack), c.reason) {
			t.Errorf("%s: reply % q does not say %q", c.label, out.Ack, c.reason)
		}
	}
	// A banner of exactly the limit is accepted; astral characters count
	// two units each, as the client counts them.
	full := HandleGmCommand(deps, nil, testDivision, gm, noticeFrame(strings.Repeat("a", chat.MaxChatNoticeUnits)))
	if full.Notice == nil {
		t.Fatalf("a %d-unit notice was refused: %s", chat.MaxChatNoticeUnits, full.Refusal)
	}
	astral := HandleGmCommand(deps, nil, testDivision, gm, noticeFrame(strings.Repeat("\U0001F600", chat.MaxChatNoticeUnits/2+1)))
	if astral.Notice != nil {
		t.Fatal("a notice past the limit in UTF-16 units was sent")
	}
}
