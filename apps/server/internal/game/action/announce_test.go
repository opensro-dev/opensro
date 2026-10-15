/*
===========================================================================

announce_test.go - the shard notices: flags, cap, clamp and each hook

Port-only, not native: off (the zero value) every hook is silent.

===========================================================================
*/
package action

import (
	"bytes"
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
kill's on wire.JobKillNoticeChannel, a seal drop's and a plus's on
wire.DropPlusNoticeChannel.
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
			if frame.Opcode != 0x3667 || len(p) < 3 || (p[0] != wire.DropPlusNoticeChannel && p[0] != wire.JobKillNoticeChannel) {
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
		if !on && len(got) != 0 || on && (len(got) != 1 || !strings.HasPrefix(got[0], "Looter has dropped ") ||
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
		want := "Looter has dropped " + ref.Name + " (" + seal + ") from Tiger Girl"
		if got := drainedNotices(t, rt); len(got) != 1 || got[0] != want {
			t.Fatalf("%s: %q, want %q", codename, got, want)
		}
	}
}

/*
================
TestPlusNoticeNamesTheSeal

With the shipped items, a +8..+11 notice names a sealed item's seal as the
drop notice does (A Star, B Moon, C Sun) and an ordinary item plainly. A
line too long for the notice keeps its plus and gives up the seal.
================
*/
func TestPlusNoticeNamesTheSeal(t *testing.T) {
	licensed.RequireGameData(t)
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	plus := func(codename string) alchemy.Outcome {
		return alchemy.Outcome{Success: true, Target: 13,
			Items: []inventory.Item{{Slot: 13, Codename: codename, Plus: 9}}}
	}
	for codename, seal := range map[string]string{
		"ITEM_CH_BLADE_08_A_RARE": " (Seal Of Star)",
		"ITEM_CH_BLADE_08_B_RARE": " (Seal Of Moon)",
		"ITEM_CH_BLADE_08_C_RARE": " (Seal Of Sun)",
		"ITEM_CH_BLADE_08_A":      "",
	} {
		ref, ok := items.ItemRefByCodename(codename)
		if !ok || ref.Name == "" {
			t.Fatalf("missing shipped item %s", codename)
		}
		rt, _ := newTestRuntime(testCharacter(), items)
		rt.Announce.Plus = true
		rt.announcePlus(testDivision, "Smith", plus(codename), 0)
		want := "Smith advanced " + ref.Name + seal + " to +9"
		if got := drainedNotices(t, rt); len(got) != 1 || got[0] != want {
			t.Fatalf("%s: %q, want %q", codename, got, want)
		}
	}
	rt, _ := newTestRuntime(testCharacter(), items)
	rt.Announce.Plus = true
	long := strings.Repeat("W", noticeMaxUnits-30)
	rt.announcePlus(testDivision, long, plus("ITEM_CH_BLADE_08_A_RARE"), 0)
	if got := drainedNotices(t, rt); len(got) != 1 || !strings.HasSuffix(got[0], " to +9") || strings.Contains(got[0], "Seal") {
		t.Fatalf("an over-long line %q, want the plus kept and the seal dropped", got)
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
TestEachNoticeKindTravelsOnItsChannel

A seal drop and a plus go out on the drop/plus channel, a job kill on its
own, so the client's Experimental rows can hide each group.
================
*/
func TestEachNoticeKindTravelsOnItsChannel(t *testing.T) {
	for kind, want := range map[announceKind]uint8{
		announceSoxDrop: wire.DropPlusNoticeChannel,
		announcePlus:    wire.DropPlusNoticeChannel,
		announceJobKill: wire.JobKillNoticeChannel,
	} {
		rt, clock := newTestRuntime(testCharacter(), testItems())
		rt.announce(testDivision, kind, "notice", clock.NowMs())
		if got := drainedNoticeLines(t, rt); len(got) != 1 || got[0].channel != want {
			t.Fatalf("kind %d: %+v, want one on channel %#x", kind, got, want)
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
				if routed.DivisionID == testDivision && frame.Opcode == 0x3667 && len(frame.Payload) > 0 &&
					frame.Payload[0] == wire.DropPlusNoticeChannel {
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

/*
================
TestJobKillNoticeRepeatLiftsThePairWindow

SRO_JOB_KILL_NOTICE_REPEAT: off, a pair killing eight times in a minute
is announced once; on, every kill is, up to the per-minute shard cap.
================
*/
func TestJobKillNoticeRepeatLiftsThePairWindow(t *testing.T) {
	for _, tc := range []struct {
		repeat bool
		want   int
	}{{false, 1}, {true, announcementsPerMinute}} {
		rt, _, thief, trader := newPvpPair(t)
		dressForJob(rt, thief, 9302, "ITEM_CH_M_TRADE_THIEF_04", 2)
		dressForJob(rt, trader, 9303, "ITEM_CH_M_TRADE_TRADER_04", 1)
		rt.Announce.JobKill, rt.Announce.JobKillRepeat = true, tc.repeat
		for i := int64(0); i < announcementsPerMinute+2; i++ {
			k := playerKill{kind: rt.playerKillKind(testDivision, trader, thief)}.withVictim(thief, trader)
			_, _, after := rt.payPlayerKillInDoor(testDivision, thief, trader, k, i*1000)
			rt.payJobKillShares(after)
		}
		if got := drainedNotices(t, rt); len(got) != tc.want {
			t.Fatalf("repeat=%v: %d notices %q, want %d", tc.repeat, len(got), got, tc.want)
		}
	}
	t.Setenv(EnvJobKillNoticeRepeat, "on")
	if !AnnouncementsFromEnv().JobKillRepeat {
		t.Fatal("the repeat flag does not read on")
	}
	t.Setenv(EnvJobKillNoticeRepeat, "")
	if AnnouncementsFromEnv().JobKillRepeat {
		t.Fatal("the repeat flag reads on when unset")
	}
}

/*
================
noticeLinkTail

The bytes a drop or plus notice carries after its text.
================
*/
func noticeLinkTail(t *testing.T, rt *Runtime) []byte {
	t.Helper()
	routed := rt.drainAnnouncements(0)
	if len(routed) != 1 || len(routed[0].Frames) != 1 {
		t.Fatalf("notices %+v, want one", routed)
	}
	p := routed[0].Frames[0].Payload
	return p[3+2*(int(p[1])|int(p[2])<<8):]
}

/*
================
TestDropAndPlusNoticesLinkThePlayerAndItem

A seal drop links its owner and the ground item in the form a pickup
grants; a +8..+11 links the player and the advanced item after its plus.
================
*/
func TestDropAndPlusNoticesLinkThePlayerAndItem(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	rt.Announce.SoxDrop, rt.Announce.Plus = true, true
	drop := grounditem.Item{Codename: "ITEM_CH_BLADE_09_C_RARE", DroppedBy: "Looter", RefObjID: 0x01020304,
		TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), Plus: 3, Durability: 40}
	rt.announceSealDrops(testDivision, "Tiger Girl", []grounditem.Item{drop}, 0)
	body := groundInventoryItem(drop, 1).Body().Encode()
	want := append(wire.NewWriter(0).Str("Looter").U16(uint16(len(body))).Payload(), body...)
	if got := noticeLinkTail(t, rt); !bytes.Equal(got, want) {
		t.Fatalf("drop link % X, want % X", got, want)
	}
	advanced := inventory.Item{Slot: 13, RefObjID: 0x01020304, Codename: "ITEM_CH_SWORD_01_A_DEF",
		TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), Plus: 9, Durability: 40}
	rt.announcePlus(testDivision, "Smith", alchemy.Outcome{Success: true, Target: 13, Items: []inventory.Item{advanced}}, 0)
	body = advanced.Body().Encode()
	want = append(wire.NewWriter(0).Str("Smith").U16(uint16(len(body))).Payload(), body...)
	if got := noticeLinkTail(t, rt); !bytes.Equal(got, want) || body[4] != 9 {
		t.Fatalf("plus link % X, want % X", got, want)
	}
}
