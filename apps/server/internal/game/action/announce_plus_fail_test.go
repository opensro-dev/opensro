/*
===========================================================================

announce_plus_fail_test.go - the high-plus failure notice: flag,
opt-in, threshold, wording, caps (#2852)

Port-only, not native: with the shard flag off, or the character not
opted in, a failed reinforce is never announced. Failures keep
#1738's privacy rule: the job alias or "a player", never the
character name, on the break channel linking the item (#3260) and
never the player.

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

// failTarget is the reinforced item as the door read it before the
// commit: the failure notice names it, and links it when the attempt
// destroyed it (#3260).
func failTarget(outcome alchemy.Outcome) inventory.Item {
	return inventory.Item{Slot: 13, Codename: "ITEM_CH_SWORD_01_A_RARE", Plus: outcome.From}
}

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
		rt.announcePlusFail(testDivision, character, failTarget(tc.outcome), tc.outcome, 0)
		if got := drainedNotices(t, rt); len(got) != tc.want {
			t.Fatalf("%s: %d notices %q, want %d", tc.name, len(got), got, tc.want)
		}
	}
}

/*
================
TestPlusFailWordingNamesTheOutcomeAndNeverTheCharacter

Destroyed, saved by Immortal, and dropped each read their own line in
the success line's wording (#3260), named by job alias (or "a
player"), on the break channel linking the item - the survivor as it
stands, the destroyed one as it stood - and no player, so the
character's own name never appears.
================
*/
func TestPlusFailWordingNamesTheOutcomeAndNeverTheCharacter(t *testing.T) {
	for _, tc := range []struct {
		name    string
		alias   string
		outcome alchemy.Outcome
		want    string
		// linkedPlus is the plus byte of the item the line links: the
		// final plus of a survivor, the pre-attempt plus of a destroy.
		linkedPlus byte
	}{
		{"destroyed", "leChiffre", failedAt(8, true, false, 0),
			"[leChiffre] has failed in augmenting <<ITEM_CH_SWORD_01_A_RARE>> to [+9]: destroyed.", 8},
		{"saved by immortal", "leChiffre", failedAt(9, false, true, 4),
			"[leChiffre] has failed in augmenting <<ITEM_CH_SWORD_01_A_RARE>> to [+10]: saved by Immortal.", 4},
		{"dropped to zero", "leChiffre", failedAt(8, false, false, 0),
			"[leChiffre] has failed in augmenting <<ITEM_CH_SWORD_01_A_RARE>> to [+9]: dropped to +0.", 0},
		{"dropped to the astral floor", "", failedAt(10, false, false, 4),
			"[a player] has failed in augmenting <<ITEM_CH_SWORD_01_A_RARE>> to [+11]: dropped to +4.", 4},
	} {
		rt, _ := newTestRuntime(testCharacter(), testItems())
		rt.Announce.PlusFail = true
		rt.Announce.PlusFailMin = announcePlusFailMinDefault
		character := breakCharacter(tc.alias)
		rt.PublicChat = chatfeed.New()
		if !rt.announcePlusFail(testDivision, character, failTarget(tc.outcome), tc.outcome, 0) {
			t.Fatalf("%s: the notice did not post", tc.name)
		}
		routed := rt.drainAnnouncements(0)
		if len(routed) != 1 || len(routed[0].Frames) != 1 {
			t.Fatalf("%s: frames %+v, want the one break-channel frame", tc.name, routed)
		}
		frame := routed[0].Frames[0]
		p := frame.Payload
		if frame.Opcode != 0x3667 || len(p) < 3 || p[0] != wire.BreakNoticeChannel {
			t.Fatalf("%s: not a break-channel notification: %#x % x", tc.name, frame.Opcode, p)
		}
		n := int(p[1]) | int(p[2])<<8
		links := p[3+2*n:]
		if len(links) < 4 || links[0] != 0 || links[1] != 0 {
			t.Fatalf("%s: payload % x names a player to link: a failure notice links only its item", tc.name, p)
		}
		body := links[4:]
		if int(links[2])|int(links[3])<<8 != len(body) || len(body) < 5 {
			t.Fatalf("%s: payload % x carries a malformed item link", tc.name, p)
		}
		if body[4] != tc.linkedPlus {
			t.Fatalf("%s: the linked item stands at +%d, want +%d", tc.name, body[4], tc.linkedPlus)
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
		rt.announcePlusFail(testDivision, character, failTarget(failedAt(8, false, false, 0)), failedAt(8, false, false, 0), 1000)
	}
	if got := drainedNotices(t, rt); len(got) != announcementsPerMinute {
		t.Fatalf("%d notices, want the shard cap %d", len(got), announcementsPerMinute)
	}
	rt.announcePlusFail(testDivision, breakCharacter("alias"), failTarget(failedAt(8, false, false, 0)), failedAt(8, false, false, 0), 1000+announceWindowMs)
	if got := drainedNotices(t, rt); len(got) != 1 {
		t.Fatalf("the window did not reopen: %v", got)
	}
}

/*
================
TestPlusFailReportsABucketDroppedLine

The kind's shard bucket full is not a post: the caller folds the
destroyed item's break line into the failure line only when the
failure line actually went out, so a bucket-dropped failure leaves
the break line to carry the destroy (#2852 follow-up).
================
*/
func TestPlusFailReportsABucketDroppedLine(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	rt.Announce.PlusFail = true
	rt.Announce.PlusFailMin = announcePlusFailMinDefault
	rt.Announce.Break = true
	for i := 0; i < announcementsPerMinute; i++ {
		rt.announce(testDivision, announcePlusFail, "filler", wire.NoticeLink{}, 0, 0)
	}
	if got := drainedNotices(t, rt); len(got) != announcementsPerMinute {
		t.Fatalf("bucket fill drained %d notices, want %d", len(got), announcementsPerMinute)
	}
	character := breakCharacter("leChiffre")
	if rt.announcePlusFail(testDivision, character, failTarget(failedAt(8, true, false, 0)), failedAt(8, true, false, 0), 0) {
		t.Fatal("a bucket-dropped failure line reported itself posted")
	}
	rt.announceBreak(testDivision, character, "ITEM_CH_SWORD_01_A_RARE", failedAt(8, true, false, 0), 0)
	if got := drainedNotices(t, rt); len(got) != 1 {
		t.Fatalf("the destroyed item got %v, want its one break line", got)
	}
}

/*
================
TestPlusFailQuietCapPerCharacter

The failure line keeps #1738's per-character window, shared with the
break line: a second failure inside it is silent (and a destroy in it
gets no second line either), another character is unaffected, and
the window reopening lets the first character post again.
================
*/
func TestPlusFailQuietCapPerCharacter(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	rt.Announce.PlusFail = true
	rt.Announce.PlusFailMin = announcePlusFailMinDefault
	rt.Announce.Break = true
	first := breakCharacter("leChiffre")
	second := breakCharacter("someoneElse")
	second.Name = "someoneElse"
	if !rt.announcePlusFail(testDivision, first, failTarget(failedAt(8, false, false, 0)), failedAt(8, false, false, 0), 0) {
		t.Fatal("the first failure line did not post")
	}
	if rt.announcePlusFail(testDivision, first, failTarget(failedAt(8, false, false, 0)), failedAt(8, false, false, 0), 1000) {
		t.Fatal("a second failure inside the window posted")
	}
	rt.announceBreak(testDivision, first, "ITEM_CH_SWORD_01_A_RARE", failedAt(8, true, false, 0), 2000)
	if got := drainedNotices(t, rt); len(got) != 1 {
		t.Fatalf("%v, want the one failure line inside the window", got)
	}
	if !rt.announcePlusFail(testDivision, second, failTarget(failedAt(8, false, false, 0)), failedAt(8, false, false, 0), 2000) {
		t.Fatal("another character's failure line was quieted")
	}
	if !rt.announcePlusFail(testDivision, first, failTarget(failedAt(8, false, false, 0)), failedAt(8, false, false, 0), breakNoticePerCharacterMs) {
		t.Fatal("the first character stayed quiet past the window")
	}
	if got := drainedNotices(t, rt); len(got) != 2 {
		t.Fatalf("%v, want the two lines after the window", got)
	}
}
