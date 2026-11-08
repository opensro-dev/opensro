/*
===========================================================================

announce_plus_fail_test.go - the high-plus failure notice: flag,
opt-in, threshold, wording, caps (#2852)

Port-only, not native: with the shard flag off, or the character not
opted in, a failed reinforce is never announced. Failures keep
#1738's privacy rule: the job alias or "a player", never the
character name, on the break channel with nothing linked.

===========================================================================
*/
package action

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/chatfeed"
)

// failedAt is a failed reinforce of an item that stood at plus
// before, destroyed or not, its target kept at its final plus.
func failedAt(plus uint8, destroyed, saved bool, final uint8) alchemy.Outcome {
	out := alchemy.Outcome{Success: false, Destroyed: destroyed, ImmortalSaved: saved, From: plus, Target: 13}
	if !destroyed {
		out.Items = []inventory.Item{{Slot: 13, Codename: "ITEM_CH_SWORD_01_A_RARE", Plus: final}}
	}
	return out
}

/*
================
TestPlusFailFollowsTheFlagOptInAndThreshold

Both gates closed, or either one: silent. Both open: a failure below
the minimum is silent, at it one notice. A success never notices.
================
*/
func TestPlusFailFollowsTheFlagOptInAndThreshold(t *testing.T) {
	for _, tc := range []struct {
		name    string
		flag    bool
		optedIn bool
		outcome alchemy.Outcome
		want    int
	}{
		{"flag off", false, true, failedAt(8, false, false, 0), 0},
		{"not opted in", true, false, failedAt(8, false, false, 0), 0},
		{"both closed", false, false, failedAt(8, false, false, 0), 0},
		{"both open at +9", true, true, failedAt(8, false, false, 0), 1},
		{"both open at +12", true, true, failedAt(11, true, false, 0), 1},
		{"a +8 attempt", true, true, failedAt(7, false, false, 0), 0},
		{"a success", true, true, alchemy.Outcome{Success: true, From: 8, Target: 13}, 0},
	} {
		rt, _ := newTestRuntime(testCharacter(), nil)
		rt.Announce.PlusFail = tc.flag
		rt.Announce.PlusFailMin = announcePlusFailMinDefault
		character := testCharacter()
		character.BreakNotice = tc.optedIn
		rt.announcePlusFail(testDivision, character, "ITEM_CH_SWORD_01_A_RARE", tc.outcome, 0)
		if got := drainedNotices(t, rt); len(got) != tc.want {
			t.Fatalf("%s: %d notices %q, want %d", tc.name, len(got), got, tc.want)
		}
	}
}

/*
================
TestPlusFailWordingNamesTheOutcomeAndNeverTheCharacter

Destroyed, saved by Immortal, and dropped each read their own line,
named by job alias (or "a player"), on the break channel, linking
nothing, and the character's own name never appears.
================
*/
func TestPlusFailWordingNamesTheOutcomeAndNeverTheCharacter(t *testing.T) {
	for _, tc := range []struct {
		name    string
		alias   string
		outcome alchemy.Outcome
		want    string
	}{
		{"destroyed", "leChiffre", failedAt(8, true, false, 0),
			"leChiffre failed +9 on ITEM_CH_SWORD_01_A_RARE: destroyed"},
		{"saved by immortal", "leChiffre", failedAt(9, false, true, 4),
			"leChiffre failed +10 on ITEM_CH_SWORD_01_A_RARE: saved by Immortal"},
		{"dropped to zero", "leChiffre", failedAt(8, false, false, 0),
			"leChiffre failed +9 on ITEM_CH_SWORD_01_A_RARE: dropped to +0"},
		{"dropped to the astral floor", "", failedAt(10, false, false, 4),
			"a player failed +11 on ITEM_CH_SWORD_01_A_RARE: dropped to +4"},
	} {
		rt, _ := newTestRuntime(testCharacter(), testItems())
		rt.Announce.PlusFail = true
		rt.Announce.PlusFailMin = announcePlusFailMinDefault
		character := breakCharacter(tc.alias)
		rt.PublicChat = chatfeed.New()
		if !rt.announcePlusFail(testDivision, character, "ITEM_CH_SWORD_01_A_RARE", tc.outcome, 0) {
			t.Fatalf("%s: the notice did not post", tc.name)
		}
		routed := rt.drainAnnouncements(0)
		if len(routed) != 1 || len(routed[0].Frames) != 1 {
			t.Fatalf("%s: frames %+v, want one link-free frame", tc.name, routed)
		}
		frame := routed[0].Frames[0]
		p := frame.Payload
		if frame.Opcode != 0x3667 || len(p) < 3 || p[0] != wire.BreakNoticeChannel {
			t.Fatalf("%s: not a break-channel notification: %#x % x", tc.name, frame.Opcode, p)
		}
		n := int(p[1]) | int(p[2])<<8
		if len(p) != 3+2*n {
			t.Fatalf("%s: payload % x carries link bytes: a failure notice links nothing", tc.name, p)
		}
		if events := rt.PublicChat.Events(testDivision); len(events) != 1 || events[0].Kind != chatfeed.KindEvent ||
			events[0].Text != tc.want {
			t.Fatalf("%s: events %+v, want the one event line %q", tc.name, events, tc.want)
		}
		if strings.Contains(tc.want, character.Name) {
			t.Fatalf("%s: the line names the character: %q", tc.name, tc.want)
		}
	}
}

/*
================
TestPlusFailCaps

Seven opted-in failures in a minute show the kind's shard cap; the
next minute shows again.
================
*/
func TestPlusFailCaps(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), nil)
	rt.Announce.PlusFail = true
	rt.Announce.PlusFailMin = announcePlusFailMinDefault
	for i := range announcementsPerMinute + 1 {
		character := breakCharacter("alias")
		character.Name = string(rune('a' + i))
		rt.announcePlusFail(testDivision, character, "ITEM_CH_SWORD_01_A_RARE", failedAt(8, false, false, 0), 1000)
	}
	if got := drainedNotices(t, rt); len(got) != announcementsPerMinute {
		t.Fatalf("%d notices, want the shard cap %d", len(got), announcementsPerMinute)
	}
	rt.announcePlusFail(testDivision, breakCharacter("alias"), "ITEM_CH_SWORD_01_A_RARE", failedAt(8, false, false, 0), 1000+announceWindowMs)
	if got := drainedNotices(t, rt); len(got) != 1 {
		t.Fatalf("the window did not reopen: %v", got)
	}
}
