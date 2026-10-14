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

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
drainedNotices

The texts of the queued notices, decoded from their 0x3667 type-7 frames.
================
*/
func drainedNotices(t *testing.T, rt *Runtime) []string {
	t.Helper()
	var texts []string
	for _, routed := range rt.drainAnnouncements(0) {
		if routed.DivisionID != testDivision || routed.SourceGID != 0 || routed.OnlyCharacterID != 0 {
			t.Fatalf("a notice is not a division broadcast: %+v", routed)
		}
		for _, frame := range routed.Frames {
			p := frame.Payload
			if frame.Opcode != 0x3667 || len(p) < 3 || p[0] != 7 {
				t.Fatalf("not a notification frame: %#x % x", frame.Opcode, p)
			}
			n := int(p[1]) | int(p[2])<<8
			units := make([]uint16, n)
			for i := range units {
				units[i] = uint16(p[3+2*i]) | uint16(p[4+2*i])<<8
			}
			texts = append(texts, string(utf16.Decode(units)))
		}
	}
	return texts
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

A dressed thief killing a dressed trader reads "Thief killed Trader"; the
same pair stays quiet for jobKillRepeatMs; an undressed player is never
announced.
================
*/
func TestJobKillNoticeFollowsTheFlag(t *testing.T) {
	thief, trader, plain := testCharacter(), testCharacter(), testCharacter()
	thief.Name, trader.Name, plain.Name = "Robber", "Merchant", "Walker"
	thief.MissionInventory = append(thief.MissionInventory, enterworld.InventoryRow{Slot: enterworld.JobSuitSlot,
		RefObjID: 9026, Codename: "ITEM_CH_M_TRADE_THIEF_04", TypeFlags: wire.PackTypeFlags(3, 1, 7, domain.JobThief), StackCount: 1})
	dressTrader(trader)
	rt, _ := newTestRuntime(testCharacter(), testItems())
	rt.announceJobKill(testDivision, thief, trader, 0)
	if got := drainedNotices(t, rt); len(got) != 0 {
		t.Fatalf("flag off announced %q", got)
	}
	rt.Announce.JobKill = true
	rt.announceJobKill(testDivision, thief, trader, 0)
	rt.announceJobKill(testDivision, thief, trader, jobKillRepeatMs-1)
	rt.announceJobKill(testDivision, thief, plain, 0)
	if got := drainedNotices(t, rt); len(got) != 1 || got[0] != "Thief killed Trader" {
		t.Fatalf("notices %q", got)
	}
	rt.announceJobKill(testDivision, thief, trader, jobKillRepeatMs)
	if got := drainedNotices(t, rt); len(got) != 1 {
		t.Fatalf("the pair did not reopen: %q", got)
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
	rt.announce(testDivision, announceJobKill, "Thief killed Trader", clock.NowMs())
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
