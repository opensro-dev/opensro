/*
===========================================================================

announce_test.go - the shard notices: flags, cap, clamp and each hook

Port-only, not native: off (the zero value) every hook is silent.

===========================================================================
*/
package action

import (
	"strings"
	"testing"
	"unicode/utf16"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
drainedNotices

The texts of the queued notices, decoded from their 0x3667 frames: a job
kill's on wire.JobKillNoticeChannel, every other on channel 7.
================
*/
func drainedNotices(t *testing.T, rt *Runtime) []string {
	t.Helper()
	var texts []string
	for _, line := range drainedNoticeLines(t, rt) {
		texts = append(texts, line.text)
	}
	return texts
}

/*
================
noticeLine
================
*/
type noticeLine struct {
	channel uint8
	text    string
}

/*
================
drainedNoticeLines

drainedNotices with each notice's channel.
================
*/
func drainedNoticeLines(t *testing.T, rt *Runtime) []noticeLine {
	t.Helper()
	var lines []noticeLine
	for _, routed := range rt.drainAnnouncements(0) {
		if routed.DivisionID != testDivision || routed.SourceGID != 0 || routed.OnlyCharacterID != 0 {
			t.Fatalf("a notice is not a division broadcast: %+v", routed)
		}
		for _, frame := range routed.Frames {
			p := frame.Payload
			if frame.Opcode != 0x3667 || len(p) < 3 || (p[0] != 7 && p[0] != wire.JobKillNoticeChannel) {
				t.Fatalf("not a notification frame: %#x % x", frame.Opcode, p)
			}
			n := int(p[1]) | int(p[2])<<8
			units := make([]uint16, n)
			for i := range units {
				units[i] = uint16(p[3+2*i]) | uint16(p[4+2*i])<<8
			}
			lines = append(lines, noticeLine{p[0], string(utf16.Decode(units))})
		}
	}
	return lines
}

/*
================
TestAnnouncementsFromEnv
================
*/
func TestAnnouncementsFromEnv(t *testing.T) {
	t.Setenv(EnvAnnounceSoxDrop, "on")
	t.Setenv(EnvAnnouncePlus, "")
	t.Setenv(EnvAnnounceJobKill, "TRUE")
	if got := AnnouncementsFromEnv(); got != (Announcements{SoxDrop: true, JobKill: true}) {
		t.Fatalf("flags %+v", got)
	}
}

/*
================
TestAnnouncementsAreCappedAndClamped

At most announcementsPerMinute of a kind per division a minute, the window
reopens after it, and a notice longer than the packet's budget is cut
rather than panicking the encoder.
================
*/
func TestAnnouncementsAreCappedAndClamped(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	for i := 0; i < announcementsPerMinute+2; i++ {
		rt.announce(testDivision, announcePlus, "line", 1000)
	}
	rt.announce(testDivision, announceJobKill, strings.Repeat("x", 300), 1000)
	got := drainedNotices(t, rt)
	if len(got) != announcementsPerMinute+1 || len(got[len(got)-1]) != noticeMaxUnits {
		t.Fatalf("%d notices, last %d long", len(got), len(got[len(got)-1]))
	}
	rt.announce(testDivision, announcePlus, "line", 1000+announceWindowMs)
	if got := drainedNotices(t, rt); len(got) != 1 {
		t.Fatalf("the window did not reopen: %v", got)
	}
}

/*
================
TestSealDropNoticeFollowsTheFlag
================
*/
func TestSealDropNoticeFollowsTheFlag(t *testing.T) {
	drops := []grounditem.Item{
		{Codename: "ITEM_CH_BLADE_09_C_RARE", DroppedBy: "Looter"},
		{Codename: "ITEM_CH_BLADE_09_C", DroppedBy: "Looter"},
		{Codename: "ITEM_CH_SWORD_09_C_RARE"},
	}
	for _, on := range []bool{false, true} {
		rt, _ := newTestRuntime(testCharacter(), testItems())
		rt.Announce.SoxDrop = on
		rt.announceSealDrops(testDivision, "Tiger Girl", drops, 0)
		got := drainedNotices(t, rt)
		if !on && len(got) != 0 || on && (len(got) != 1 || !strings.HasPrefix(got[0], "Looter dropped ") ||
			!strings.HasSuffix(got[0], " from Tiger Girl")) {
			t.Fatalf("on=%v: %q", on, got)
		}
	}
}

/*
================
TestSealDropNoticeNamesTheSeal

With the shipped items, a drop notice names the item's seal with the
client's own words (PARAM_RARE_*): A Star, B Moon, C Sun.
================
*/
func TestSealDropNoticeNamesTheSeal(t *testing.T) {
	licensed.RequireGameData(t)
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	for codename, seal := range map[string]string{
		"ITEM_CH_BLADE_08_A_RARE": "Seal Of Star",
		"ITEM_CH_BLADE_08_B_RARE": "Seal Of Moon",
		"ITEM_CH_BLADE_08_C_RARE": "Seal Of Sun",
	} {
		ref, ok := items.ItemRefByCodename(codename)
		if !ok || ref.Name == "" {
			t.Fatalf("missing shipped item %s", codename)
		}
		rt, _ := newTestRuntime(testCharacter(), items)
		rt.Announce.SoxDrop = true
		rt.announceSealDrops(testDivision, "Tiger Girl", []grounditem.Item{{Codename: codename, DroppedBy: "Looter"}}, 0)
		want := "Looter dropped " + ref.Name + " (" + seal + ") from Tiger Girl"
		if got := drainedNotices(t, rt); len(got) != 1 || got[0] != want {
			t.Fatalf("%s: %q, want %q", codename, got, want)
		}
	}
}

/*
================
TestPlusNoticeFollowsTheFlag

Only a success into +8..+11 is announced, and only with the flag.
================
*/
func TestPlusNoticeFollowsTheFlag(t *testing.T) {
	item := func(plus uint8) alchemy.Outcome {
		return alchemy.Outcome{Success: true, Target: 13,
			Items: []inventory.Item{{Slot: 13, Codename: "ITEM_CH_SWORD_01_A_DEF", Plus: plus}}}
	}
	failed := item(9)
	failed.Success = false
	for _, tc := range []struct {
		on      bool
		outcome alchemy.Outcome
		want    string
	}{
		{false, item(9), ""},
		{true, item(7), ""},
		{true, failed, ""},
		{true, item(8), "+8"},
		{true, item(11), "+11"},
		{true, item(12), ""},
	} {
		rt, _ := newTestRuntime(testCharacter(), testItems())
		rt.Announce.Plus = tc.on
		rt.announcePlus(testDivision, "Smith", tc.outcome, 0)
		got := drainedNotices(t, rt)
		if tc.want == "" && len(got) != 0 || tc.want != "" && (len(got) != 1 || !strings.HasPrefix(got[0], "Smith advanced ") ||
			!strings.HasSuffix(got[0], " to "+tc.want)) {
			t.Fatalf("on=%v plus=%d: %q", tc.on, tc.outcome.Items[0].Plus, got)
		}
	}
}

/*
================
TestJobKillNoticeFollowsTheFlag

A dressed thief killing a dressed trader reads "[leChiffre] killed
[ES_Tip]" (their job aliases, never their names), but only once the kill's
door has committed: payPlayerKillInDoor carries the
notice and payJobKillShares, run after the commit, posts it. The same pair
stays quiet for jobKillRepeatMs; an undressed victim is never announced.
================
*/
func TestJobKillNoticeFollowsTheFlag(t *testing.T) {
	rt, _, thief, trader := newPvpPair(t)
	dressForJob(rt, thief, 9302, "ITEM_CH_M_TRADE_THIEF_04", 2)
	dressForJob(rt, trader, 9303, "ITEM_CH_M_TRADE_TRADER_04", 1)
	thief.Job.Alias, trader.Job.Alias = "leChiffre", "ES_Tip"
	kill := func(victim *enterworld.Character, nowMs int64) jobKillPayout {
		k := playerKill{kind: rt.playerKillKind(testDivision, victim, thief)}.withVictim(thief, victim)
		_, _, after := rt.payPlayerKillInDoor(testDivision, thief, victim, k, nowMs)
		return after
	}
	after := kill(trader, 0)
	if got := drainedNotices(t, rt); len(got) != 0 {
		t.Fatalf("the kill's door posted before its commit: %q", got)
	}
	rt.payJobKillShares(after)
	if got := drainedNotices(t, rt); len(got) != 0 {
		t.Fatalf("flag off announced %q", got)
	}
	rt.Announce.JobKill = true
	rt.payJobKillShares(kill(trader, 0))
	rt.payJobKillShares(kill(trader, jobKillRepeatMs-1))
	if got := drainedNoticeLines(t, rt); len(got) != 1 || got[0].text != "[leChiffre] killed [ES_Tip]" ||
		got[0].channel != wire.JobKillNoticeChannel {
		t.Fatalf("notices %+v, want one on the job-kill channel", got)
	}
	for _, got := range []string{thief.Name, trader.Name} {
		if strings.Contains("[leChiffre] killed [ES_Tip]", got) {
			t.Fatalf("the notice names the character %q", got)
		}
	}
	rt.payJobKillShares(kill(trader, jobKillRepeatMs))
	if got := drainedNotices(t, rt); len(got) != 1 {
		t.Fatalf("the pair did not reopen: %q", got)
	}
	// A wearer without a job alias shows the job's name, still never the
	// character's.
	trader.Job.Alias = ""
	rt.payJobKillShares(kill(trader, 3*jobKillRepeatMs))
	if got := drainedNotices(t, rt); len(got) != 1 || got[0] != "[leChiffre] killed [Trader]" {
		t.Fatalf("no-alias notice %q", got)
	}
	plain := *trader
	plain.MissionInventory = nil
	if after := kill(&plain, 2*jobKillRepeatMs); len(after.notices) != 0 {
		t.Fatalf("an undressed victim carried a notice: %+v", after.notices)
	}
}

/*
================
TestReinforceIntoPlusEightAnnounces

Through HandleAlchemyReinforce: a success from +7 into +8 posts one notice
with the flag on, and none with it off.
================
*/
func TestReinforceIntoPlusEightAnnounces(t *testing.T) {
	for _, on := range []bool{false, true} {
		rt, c := alchemyRuntime()
		c.MissionInventory[0].Plus = 7
		rt.Announce.Plus = on
		frames := rt.HandleAlchemyReinforce(testDivision, c, []byte{2, 20, 21})
		if last := frames[len(frames)-1]; last.Payload[0] != 1 || last.Payload[1] != 1 {
			t.Fatalf("on=%v: the reinforcement did not succeed: %+v", on, frames)
		}
		got := drainedNotices(t, rt)
		if !on && len(got) != 0 || on && (len(got) != 1 || !strings.HasSuffix(got[0], " to +8")) {
			t.Fatalf("on=%v: %q", on, got)
		}
	}
}

/*
================
TestTickSendsQueuedNotices

The runtime tick drains the queue: a notice posted from a door reaches the
division on the next tick, once.
================
*/
func TestTickSendsQueuedNotices(t *testing.T) {
	rt, clock := newTestRuntime(testCharacter(), testItems())
	rt.announce(testDivision, announcePlus, "Looter advanced Blade to +8", clock.NowMs())
	count := func(out []simulation.DivisionFrames) int {
		n := 0
		for _, routed := range out {
			for _, frame := range routed.Frames {
				if routed.DivisionID == testDivision && frame.Opcode == 0x3667 && len(frame.Payload) > 0 && frame.Payload[0] == 7 {
					n++
				}
			}
		}
		return n
	}
	if n := count(rt.TickHook()(clock.NowMs())); n != 1 {
		t.Fatalf("first tick sent %d notices", n)
	}
	if n := count(rt.TickHook()(clock.NowMs())); n != 0 {
		t.Fatalf("second tick resent %d notices", n)
	}
}
