/*
===========================================================================

notice.go - the GM's /NOTICE banner

Port-only, not native. /NOTICE text shows the text in the notice banner
of every player in the GM's division, the same 0x3667 chat type 7 line
the server already sends for its own notices (chat.EncodeChatNotice).

The v1.150 client has no notice command: its SR_IMPLE_COMMAND_* table
(client strings @0x007cc488..@0x007cd228) names no notice word, and its
chat input composes no type 7 line (social/chat routeChat refuses one).
The original's notices came from the operator's server tools, so the
port picks subcommand 0xFB (wire.go) and its own reply text.

Off is native: the command answers only while SRO_GM_NOTICE is on, and
only for a GM whose staff grant carries staff.Notice (capability.go).
Every notice leaves an operator log line naming the GM and the text.

===========================================================================
*/
package gmcommand

import (
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf16"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/chat"
)

// EnvGMNotice turns /NOTICE on ("on", "1", "true"). Default off.
const EnvGMNotice = "SRO_GM_NOTICE"

/*
================
NoticeDesk

The GM lane's owner says whether /NOTICE is on (the GameWorld wiring
reads SRO_GM_NOTICE once, at start).
================
*/
type NoticeDesk interface {
	GMNoticeEnabled() bool
}

/*
================
GMNoticeFromEnv
================
*/
func GMNoticeFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvGMNotice))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
handleNotice

Checks the switch and the text, then hands the 0x3667 line to the hub
glue (Outcome.Notice), which sends it to the division. The text is
refused, never cut: the client would show only the first
MaxChatNoticeUnits units, so the GM is told the length instead.
================
*/
func handleNotice(divisionID string, sender *enterworld.Character, request Request, status []BodyStatusCommands) Outcome {
	fail := func(text string) Outcome {
		return Outcome{Ack: EncodeModerationReply(AckResultFail, SubNotice, text), Refusal: "notice: " + text}
	}
	enabled := false
	if len(status) > 0 {
		if owner, ok := status[0].(NoticeDesk); ok {
			enabled = owner.GMNoticeEnabled()
		}
	}
	if !enabled {
		return fail("this command is off on this server (" + EnvGMNotice + ")")
	}
	text := strings.TrimSpace(request.Text)
	if text == "" {
		return fail("type the notice text")
	}
	if strings.IndexFunc(text, unicode.IsControl) >= 0 {
		return fail("the notice carries a control character")
	}
	if units := len(utf16.Encode([]rune(text))); units > chat.MaxChatNoticeUnits {
		return fail(fmt.Sprintf("the notice is %d characters; the banner holds %d", units, chat.MaxChatNoticeUnits))
	}
	log.WithFields(log.Fields{"gm": sender.Name, "division": divisionID, "text": text}).Info("gmcommand: notice sent")
	return Outcome{Ack: EncodeModerationReply(AckResultOK, SubNotice, "notice sent"), Notice: chat.EncodeChatNotice(text)}
}
