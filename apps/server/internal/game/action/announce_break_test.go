/*
===========================================================================

announce_break_test.go - the item-break notice: flag, opt-in, wording,
caps (#1738)

Port-only, not native: with the shard flag off, or the character not
opted in, a destroyed reinforcement is never announced. The notice is
all the feature does; the reinforcement's outcome is computed before it
and stands either way.

===========================================================================
*/
package action

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
breakCharacter

A character who opted in, with the given job alias ("" for none). Its
name is "asd2"; no notice may ever carry it.
================
*/
func breakCharacter(alias string) *enterworld.Character {
	c := testCharacter()
	c.BreakNotice = true
	c.Job.Alias = alias
	return c
}

// destroyed is a reinforcement outcome that broke its item.
func destroyed() alchemy.Outcome {
	return alchemy.Outcome{Success: false, Destroyed: true, Target: 13}
}

/*
================
TestBreakNoticeFollowsTheFlagAndOptIn

Both gates closed: silent. Either one open: still silent. Both open on
a destroyed item: one notice. A success, or a failure that keeps the
item, never notices.
================
*/
func TestBreakNoticeFollowsTheFlagAndOptIn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		flag    bool
		optedIn bool
		outcome alchemy.Outcome
		want    int
	}{
		{"flag off", false, true, destroyed(), 0},
		{"not opted in", true, false, destroyed(), 0},
		{"both closed", false, false, destroyed(), 0},
		{"both open", true, true, destroyed(), 1},
		{"a success", true, true, alchemy.Outcome{Success: true, Target: 13}, 0},
		{"a kept failure", true, true, alchemy.Outcome{Success: false, Target: 13}, 0},
	} {
		rt, _ := newTestRuntime(testCharacter(), nil)
		rt.Announce.Break = tc.flag
		character := testCharacter()
		character.BreakNotice = tc.optedIn
		rt.announceBreak(testDivision, character, "ITEM_CH_SWORD_01_A_DEF", tc.outcome, 0)
		if got := drainedNotices(t, rt); len(got) != tc.want {
			t.Fatalf("%s: %d notices %q, want %d", tc.name, len(got), got, tc.want)
		}
	}
}

/*
================
TestBreakNoticeNamesAliasOrAPlayer

The notice names the job alias, or "a player" when none is set - never
the character name - and names the destroyed item by its reference name
and seal, as the plus notice does. It rides its own channel and links
nothing: one frame, no item references ahead of it.
================
*/
func TestBreakNoticeNamesAliasOrAPlayer(t *testing.T) {
	clientdata.RequireGameData(t)
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	ref, ok := items.ItemRefByCodename("ITEM_CH_BLADE_08_C_RARE")
	if !ok || ref.Name == "" {
		t.Fatal("missing shipped item ITEM_CH_BLADE_08_C_RARE")
	}
	for _, tc := range []struct {
		alias string
		want  string
	}{
		{"leChiffre", "leChiffre broke " + ref.Name + " (Seal Of Sun)"},
		{"", "a player broke " + ref.Name + " (Seal Of Sun)"},
	} {
		rt, _ := newTestRuntime(testCharacter(), items)
		rt.Announce.Break = true
		character := breakCharacter(tc.alias)
		rt.announceBreak(testDivision, character, "ITEM_CH_BLADE_08_C_RARE", destroyed(), 0)
		routed := rt.drainAnnouncements(0)
		if len(routed) != 1 || len(routed[0].Frames) != 1 {
			t.Fatalf("alias %q: frames %+v, want one link-free frame (no item references ahead of it)", tc.alias, routed)
		}
		frame := routed[0].Frames[0]
		p := frame.Payload
		if frame.Opcode != 0x3667 || len(p) < 3 || p[0] != wire.BreakNoticeChannel {
			t.Fatalf("not a break-channel notification: %#x % x", frame.Opcode, p)
		}
		n := int(p[1]) | int(p[2])<<8
		if len(p) != 3+2*n {
			t.Fatalf("payload % x carries %d bytes past the text: a break notice links nothing", p, len(p)-3-2*n)
		}
		units := make([]uint16, n)
		for i := range units {
			units[i] = uint16(p[3+2*i]) | uint16(p[4+2*i])<<8
		}
		if text := string(utf16.Decode(units)); text != tc.want {
			t.Fatalf("alias %q: %q, want %q", tc.alias, text, tc.want)
		} else if strings.Contains(text, character.Name) {
			t.Fatalf("the notice names the character: %q", text)
		}
	}
}

/*
================
TestBreakNoticeCaps

One character shows one break notice per breakNoticePerCharacterMs
however often they break in it; another character is capped only by
their own window. The kind's per-minute shard cap still binds everyone
together: seven opted-in characters breaking in one minute show six.
================
*/
func TestBreakNoticeCaps(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), nil)
	rt.Announce.Break = true
	one := breakCharacter("one")
	two := breakCharacter("two")
	two.Name = "asd3"
	rt.announceBreak(testDivision, one, "ITEM_CH_SWORD_01_A_DEF", destroyed(), 0)
	rt.announceBreak(testDivision, one, "ITEM_CH_SWORD_01_A_DEF", destroyed(), 60_000)
	rt.announceBreak(testDivision, two, "ITEM_CH_SWORD_01_A_DEF", destroyed(), 60_000)
	if got := drainedNotices(t, rt); len(got) != 2 {
		t.Fatalf("inside the window: %q, want one notice per character", got)
	}
	rt.announceBreak(testDivision, one, "ITEM_CH_SWORD_01_A_DEF", destroyed(), breakNoticePerCharacterMs)
	if got := drainedNotices(t, rt); len(got) != 1 {
		t.Fatalf("past the window: %q, want the character's next notice", got)
	}

	rt, _ = newTestRuntime(testCharacter(), nil)
	rt.Announce.Break = true
	for i := range 7 {
		c := breakCharacter(fmt.Sprintf("alias%d", i))
		c.Name = fmt.Sprintf("char%d", i)
		rt.announceBreak(testDivision, c, "ITEM_CH_SWORD_01_A_DEF", destroyed(), 0)
	}
	if got := drainedNotices(t, rt); len(got) != announcementsPerMinute {
		t.Fatalf("seven characters in a minute: %d notices, want the shard cap %d", len(got), announcementsPerMinute)
	}
}
