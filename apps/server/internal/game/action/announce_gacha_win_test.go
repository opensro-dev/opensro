/*
===========================================================================

announce_gacha_win_test.go - the Magic Pop win's Event-feed line:
flag and wording (#2852)

Port-only, not native: with the flag off a committed win queues no
notice (the native 0x30C0 banner, asserted by the gacha tests, is
untouched). On, one drop/plus-channel line names the winner and the
prize, and the website's event feed keeps it.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/chatfeed"
)

/*
================
TestGachaWinNoticeFollowsTheFlag

Off: silent. On: one line, "asd2 won ITEM_CH_SWORD_01_A_RARE x3 from
Magic Pang" (the fixture's reference names nothing, so the codename
stands), on the drop/plus channel, and one event-feed line.
================
*/
func TestGachaWinNoticeFollowsTheFlag(t *testing.T) {
	for _, on := range []bool{false, true} {
		rt, _ := newTestRuntime(testCharacter(), testItems())
		rt.Announce.GachaWin = on
		rt.PublicChat = chatfeed.New()
		rt.announceGachaWin(testDivision, "asd2", 11459, 3, 0)
		lines := drainedNoticeLines(t, rt)
		if !on {
			if len(lines) != 0 {
				t.Fatalf("off: %v, want no notice", lines)
			}
			continue
		}
		if len(lines) != 1 || lines[0].channel != wire.DropPlusNoticeChannel ||
			lines[0].text != "asd2 won ITEM_CH_SWORD_01_A_RARE x3 from Magic Pang" {
			t.Fatalf("on: %+v, want the one win line on the drop/plus channel", lines)
		}
		if events := rt.PublicChat.Events(testDivision); len(events) != 1 || events[0].Kind != chatfeed.KindEvent ||
			events[0].Text != lines[0].text {
			t.Fatalf("events %+v, want the win line in the event feed", events)
		}
	}
}
