/*
===========================================================================

premiumupgrade_test.go - the port-only premium upgrade (SRO_PREMIUM_UPGRADE)

With the flag on, a strictly higher-grade premium of the same family
replaces the running lower one; the old ticket's remaining time moves
the new ticket's end later (#1624, owner decision), its board row
ends, the new clock restarts today's grant. Off (default), equal and
lower grades, and the other family keep the native 0x1894 refusal.

===========================================================================
*/

package action

import (
	"encoding/json"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
premiumUpgradeTicket

One synthetic premium ticket (daily allotment and duration are the
grade; every shipped rate is 100).
================
*/
func premiumUpgradeTicket(refID uint32, codename string, skillOnly bool, seconds, dailyMs float64) *enterworld.ItemRef {
	tid := int64(4)
	expPct, skillPct := float64(100), float64(100)
	if skillOnly {
		tid, expPct = 5, 0
	}
	return &enterworld.ItemRef{RefObjID: refID, Codename: codename, Country: 3,
		TypeIDs: [4]int64{3, 3, 13, tid}, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "maxStack": 10,
			"itemParam1_29c": seconds, "itemParam3_2a4": dailyMs, "itemParam4_2a8": expPct, "itemParam5_2ac": skillPct})}
}

func holdTicket(c *enterworld.Character, slot uint8, ref *enterworld.ItemRef) {
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(slot), RefObjID: ref.RefObjID,
		Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 1})
}

func useTicket(t *testing.T, rt *Runtime, c *enterworld.Character, slot uint8, ref *enterworld.ItemRef) OpResult {
	t.Helper()
	return rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(slot).U16(ref.TypeFlags()).Payload())
}

func premiumUsePayload(slot uint8, ref *enterworld.ItemRef) []byte {
	return wire.NewWriter(3).U8(slot).U16(ref.TypeFlags()).Payload()
}

// The shipped grade order, as synthetic tickets.
var (
	gold1D    = premiumUpgradeTicket(3701, "ITEM_MALL_GOLD_TIME_SERVICE_TICKET_1D", false, 86400, 10800000)
	gold4W    = premiumUpgradeTicket(3704, "ITEM_MALL_GOLD_TIME_SERVICE_TICKET_4W", false, 2419200, 10800000)
	gold8W    = premiumUpgradeTicket(3708, "ITEM_MALL_GOLD_TIME_SERVICE_TICKET_8W", false, 4838400, 10800000)
	gold16W   = premiumUpgradeTicket(3716, "ITEM_MALL_GOLD_TIME_SERVICE_TICKET_16W", false, 9676800, 10800000)
	goldPlus  = premiumUpgradeTicket(3744, "ITEM_PRE_GOLTAM_4W_PLUS", false, 2419200, 18000000)
	skill1D   = premiumUpgradeTicket(3721, "ITEM_MALL_SKILL_GOLD_TIME_SERVICE_TICKET_1D", true, 86400, 10800000)
	skill4W   = premiumUpgradeTicket(3724, "ITEM_MALL_SKILL_GOLD_TIME_SERVICE_TICKET_4W", true, 2419200, 10800000)
	skillPlus = premiumUpgradeTicket(3745, "ITEM_MALL_SKILL_GOLD_TIME_SERVICE_TICKET_PLUS", true, 86400, 18000000)
)

func premiumUpgradeRuntime(t *testing.T, on bool, refs ...*enterworld.ItemRef) (*Runtime, *enterworld.Character, *fakeClock) {
	t.Helper()
	c := testCharacter()
	items := testItems()
	for _, ref := range refs {
		items[ref.Codename] = ref
	}
	rt, clock := newTestRuntime(c, items)
	rt.PremiumUpgrade = on
	return rt, c, clock
}

func hasFrame(frames []wire.Frame, opcode uint16) bool {
	for _, frame := range frames {
		if frame.Opcode == opcode {
			return true
		}
	}
	return false
}

/*
================
TestPremiumUpgradeFromEnv
================
*/
func TestPremiumUpgradeFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "0": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvPremiumUpgrade, value)
		if got := PremiumUpgradeFromEnv(); got != want {
			t.Fatalf("%s=%q: %v, want %v", EnvPremiumUpgrade, value, got, want)
		}
	}
}

/*
================
TestPremiumUpgradeOffKeepsTheNativeRefusal

Flag off: a higher grade still answers 0x1894 and nothing changes.
================
*/
func TestPremiumUpgradeOffKeepsTheNativeRefusal(t *testing.T) {
	rt, c, clock := premiumUpgradeRuntime(t, false, gold4W, gold8W)
	holdTicket(c, 25, gold4W)
	holdTicket(c, 26, gold8W)
	if out := useTicket(t, rt, c, 25, gold4W); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("first ticket = %+v", out.Frames)
	}
	assertItemUseRefusedUnchanged(t, rt, c, premiumUsePayload(26, gold8W), errCodePremiumActive)
	if c.PremiumClock == nil || c.PremiumClock.ItemRefObjID != gold4W.RefObjID || !premiumRunning(c, clock.NowMs()) {
		t.Fatalf("the refusal moved the running premium: %+v", c.PremiumClock)
	}
	for _, job := range c.ParamJobs {
		if job.ItemRefObjID != gold4W.RefObjID {
			t.Fatalf("job after refusal = %+v, want the 4-week ticket's", job)
		}
	}
}

/*
================
TestPremiumUpgradeReplacesWithAHigherGrade

Flag on: the 8-week use succeeds, the 4-week row ends, the new clock
restarts today in full, and the bonus runs under the new ticket.
================
*/
func TestPremiumUpgradeReplacesWithAHigherGrade(t *testing.T) {
	rt, c, clock := premiumUpgradeRuntime(t, true, gold4W, gold8W)
	holdTicket(c, 25, gold4W)
	holdTicket(c, 26, gold8W)
	if out := useTicket(t, rt, c, 25, gold4W); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("first ticket = %+v", out.Frames)
	}
	out := useTicket(t, rt, c, 26, gold8W)
	if out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("upgrade = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	if !hasFrame(out.Frames, wire.OpParamJobEnd) || !hasFrame(out.Frames, wire.OpParamJobStart) {
		t.Fatalf("upgrade frames lack the old row's end or the new row's start: %+v", out.Frames)
	}
	now := clock.NowMs()
	for _, job := range c.ParamJobs {
		if job.ItemRefObjID != gold8W.RefObjID {
			t.Fatalf("job after upgrade = %+v, want only the 8-week ticket's", job)
		}
		if job.EndUnixMs != now+(4838400+2419200)*1000 {
			t.Fatalf("job end = %d, want %d (the new course plus the replaced ticket's full remaining course, credited at the same instant)", job.EndUnixMs, now+(4838400+2419200)*1000)
		}
	}
	if c.PremiumClock == nil || c.PremiumClock.ItemRefObjID != gold8W.RefObjID ||
		c.PremiumClock.StartUnixMs != now || c.PremiumClock.TodayMs != c.PremiumClock.DailyAllotmentMs ||
		c.PremiumClock.CarriedMs != 0 {
		t.Fatalf("clock after upgrade = %+v", c.PremiumClock)
	}
	if exp, _ := paramJobRewardBonus(c, 1000, 0, now); exp != 2000 {
		t.Fatalf("bonus after upgrade = %d, want doubled", exp)
	}
	if got := len(rt.paramJobOwners.keys()); got == 0 {
		t.Fatal("the owner fell out of the param-job pass")
	}
}

/*
================
TestPremiumUpgradeRefusesEqualAndLowerGrades
================
*/
func TestPremiumUpgradeRefusesEqualAndLowerGrades(t *testing.T) {
	rt, c, _ := premiumUpgradeRuntime(t, true, gold1D, gold4W)
	holdTicket(c, 25, gold4W)
	holdTicket(c, 26, gold4W)
	holdTicket(c, 27, gold1D)
	if out := useTicket(t, rt, c, 25, gold4W); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("first ticket = %+v", out.Frames)
	}
	assertItemUseRefusedUnchanged(t, rt, c, premiumUsePayload(26, gold4W), errCodePremiumActive)
	assertItemUseRefusedUnchanged(t, rt, c, premiumUsePayload(27, gold1D), errCodePremiumActive)
}

/*
================
TestPremiumUpgradeDailyAllotmentOutranksDuration

Five hours a day for four weeks outranks three hours a day for
sixteen; the reverse refuses.
================
*/
func TestPremiumUpgradeDailyAllotmentOutranksDuration(t *testing.T) {
	rt, c, _ := premiumUpgradeRuntime(t, true, gold16W, goldPlus)
	holdTicket(c, 25, gold16W)
	holdTicket(c, 26, goldPlus)
	if out := useTicket(t, rt, c, 25, gold16W); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("first ticket = %+v", out.Frames)
	}
	if out := useTicket(t, rt, c, 26, goldPlus); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("plus over 16-week = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	rt, c, _ = premiumUpgradeRuntime(t, true, gold16W, goldPlus)
	holdTicket(c, 25, goldPlus)
	holdTicket(c, 26, gold16W)
	if out := useTicket(t, rt, c, 25, goldPlus); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("first ticket = %+v", out.Frames)
	}
	assertItemUseRefusedUnchanged(t, rt, c, premiumUsePayload(26, gold16W), errCodePremiumActive)
}

/*
================
TestPremiumUpgradeStaysInFamily

The skill family upgrades inside itself; across families the refusal
stands even for a higher grade.
================
*/
func TestPremiumUpgradeStaysInFamily(t *testing.T) {
	rt, c, _ := premiumUpgradeRuntime(t, true, skill1D, skill4W, gold4W)
	holdTicket(c, 25, skill1D)
	holdTicket(c, 26, skill4W)
	holdTicket(c, 27, gold4W)
	if out := useTicket(t, rt, c, 25, skill1D); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("skill ticket = %+v", out.Frames)
	}
	assertItemUseRefusedUnchanged(t, rt, c, premiumUsePayload(27, gold4W), errCodePremiumActive)
	if out := useTicket(t, rt, c, 26, skill4W); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("skill upgrade = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	rt, c, _ = premiumUpgradeRuntime(t, true, gold4W, skillPlus)
	holdTicket(c, 25, gold4W)
	holdTicket(c, 26, skillPlus)
	if out := useTicket(t, rt, c, 25, gold4W); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("gold ticket = %+v", out.Frames)
	}
	assertItemUseRefusedUnchanged(t, rt, c, premiumUsePayload(26, skillPlus), errCodePremiumActive)
}

/*
================
TestPremiumUpgradePackageReplacesOnShippedData

The shipped chain: a running 1-day Gold Time ticket, then the Gold
Time package (its embedded 4-week ticket) — a replacement with the
flag on, the native refusal with it off.
================
*/
func TestPremiumUpgradePackageReplacesOnShippedData(t *testing.T) {
	for _, on := range []bool{true, false} {
		rt, c, clock, pack := goldTimeRuntime(t)
		rt.PremiumUpgrade = on
		oneDay, found := rt.deps.ItemReferences().ItemRefByCodename("ITEM_MALL_GOLD_TIME_SERVICE_TICKET_1D")
		if !found {
			t.Fatal("shipped 1-day Gold Time ticket missing")
		}
		holdTicket(c, 42, oneDay)
		if out := useTicket(t, rt, c, 42, oneDay); out.Frames[0].Payload[0] != wire.ResultSuccess {
			t.Fatalf("1-day ticket = %+v / %q", out.Frames, out.DiagnosticRefusal)
		}
		if on {
			out := useTicket(t, rt, c, 40, pack)
			if out.Frames[0].Payload[0] != wire.ResultSuccess {
				t.Fatalf("package upgrade = %+v / %q", out.Frames, out.DiagnosticRefusal)
			}
			if !hasFrame(out.Frames, wire.OpParamJobEnd) {
				t.Fatal("the replaced ticket's board row never ended")
			}
			inner, _ := rt.deps.ItemReferences().ItemRefByCodename("ITEM_PRE_GOLTAM_4W")
			if c.PremiumClock == nil || c.PremiumClock.ItemRefObjID != inner.RefObjID {
				t.Fatalf("clock after package upgrade = %+v, want the embedded 4-week ticket", c.PremiumClock)
			}
			if len(c.CompositeJobs) == 0 {
				t.Fatal("the package's other entries did not install")
			}
		} else {
			assertItemUseRefusedUnchanged(t, rt, c, premiumUsePayload(40, pack), errCodePremiumActive)
			if c.PremiumClock == nil || c.PremiumClock.ItemRefObjID != oneDay.RefObjID || !premiumRunning(c, clock.NowMs()) {
				t.Fatal("the flag-off refusal moved the running premium")
			}
		}
	}
}

/*
================
TestPremiumUpgradePersistsThroughRelog

The swap commits in the one character transaction: a save/reload at
any point sees the new premium whole (no half-swapped state).
================
*/
func TestPremiumUpgradePersistsThroughRelog(t *testing.T) {
	rt, c, clock := premiumUpgradeRuntime(t, true, gold4W, gold8W)
	holdTicket(c, 25, gold4W)
	holdTicket(c, 26, gold8W)
	useTicket(t, rt, c, 25, gold4W)
	useTicket(t, rt, c, 26, gold8W)
	saved, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	back := testCharacter()
	if err := json.Unmarshal(saved, back); err != nil {
		t.Fatal(err)
	}
	if len(back.ParamJobs) != len(c.ParamJobs) || back.PremiumClock == nil ||
		back.PremiumClock.ItemRefObjID != gold8W.RefObjID {
		t.Fatalf("reloaded premium = jobs %+v clock %+v", back.ParamJobs, back.PremiumClock)
	}
	for i := range c.ParamJobs {
		if back.ParamJobs[i] != c.ParamJobs[i] {
			t.Fatalf("reloaded job %d = %+v, want %+v", i, back.ParamJobs[i], c.ParamJobs[i])
		}
	}
	if !premiumRunning(back, clock.NowMs()) {
		t.Fatal("the reloaded premium no longer runs")
	}
}

/*
================
TestPremiumUpgradeCreditsTheReplacedTicketsRemainingTime

Owner decision (#1624): the replaced ticket's unused time is not
lost - a 4-week ticket upgraded with 3 weeks left moves the new
ticket's end to now + its own course + 3 weeks. The daily clock
still restarts fresh (full grant, no carry).
================
*/
func TestPremiumUpgradeCreditsTheReplacedTicketsRemainingTime(t *testing.T) {
	rt, c, clock := premiumUpgradeRuntime(t, true, gold4W, gold8W)
	holdTicket(c, 25, gold4W)
	holdTicket(c, 26, gold8W)
	if out := useTicket(t, rt, c, 25, gold4W); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("first ticket = %+v", out.Frames)
	}
	// One week of the 4-week course spent: 3 weeks remain.
	clock.Advance(7 * 24 * time.Hour)
	if out := useTicket(t, rt, c, 26, gold8W); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("upgrade = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	now := clock.NowMs()
	wantEnd := now + (4838400+1814400)*1000
	for _, job := range c.ParamJobs {
		if job.ItemRefObjID != gold8W.RefObjID {
			t.Fatalf("job after upgrade = %+v, want only the 8-week ticket's", job)
		}
		if job.EndUnixMs != wantEnd {
			t.Fatalf("job end = %d, want %d (new course plus the 3 credited weeks)", job.EndUnixMs, wantEnd)
		}
	}
	if c.PremiumClock == nil || c.PremiumClock.ItemRefObjID != gold8W.RefObjID ||
		c.PremiumClock.StartUnixMs != now || c.PremiumClock.EndUnixMs != wantEnd ||
		c.PremiumClock.TodayMs != c.PremiumClock.DailyAllotmentMs || c.PremiumClock.CarriedMs != 0 {
		t.Fatalf("clock after upgrade = %+v, want a fresh clock ending at %d", c.PremiumClock, wantEnd)
	}
}

/*
================
TestPremiumUpgradePackageCreditsTheReplacedTicketsRemainingTime

The package path credits too: a 1-day Gold Time ticket with 12 hours
left, replaced by the Gold Time package's embedded 4-week ticket,
runs until now + 4 weeks + 12 hours.
================
*/
func TestPremiumUpgradePackageCreditsTheReplacedTicketsRemainingTime(t *testing.T) {
	rt, c, clock, pack := goldTimeRuntime(t)
	rt.PremiumUpgrade = true
	oneDay, found := rt.deps.ItemReferences().ItemRefByCodename("ITEM_MALL_GOLD_TIME_SERVICE_TICKET_1D")
	if !found {
		t.Fatal("shipped 1-day Gold Time ticket missing")
	}
	inner, found := rt.deps.ItemReferences().ItemRefByCodename("ITEM_PRE_GOLTAM_4W")
	if !found {
		t.Fatal("shipped embedded 4-week Gold Time ticket missing")
	}
	holdTicket(c, 42, oneDay)
	if out := useTicket(t, rt, c, 42, oneDay); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("1-day ticket = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	clock.Advance(12 * time.Hour)
	if out := useTicket(t, rt, c, 40, pack); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("package upgrade = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	now := clock.NowMs()
	wantEnd := now + (2419200+43200)*1000
	sawInner := false
	for _, job := range c.ParamJobs {
		if job.ItemRefObjID != inner.RefObjID {
			continue
		}
		sawInner = true
		if job.EndUnixMs != wantEnd {
			t.Fatalf("package job end = %d, want %d (4 weeks plus the credited 12 hours)", job.EndUnixMs, wantEnd)
		}
	}
	if !sawInner {
		t.Fatalf("no jobs under the embedded ticket after the package upgrade: %+v", c.ParamJobs)
	}
	if c.PremiumClock == nil || c.PremiumClock.ItemRefObjID != inner.RefObjID || c.PremiumClock.EndUnixMs != wantEnd {
		t.Fatalf("clock after package upgrade = %+v, want the embedded ticket ending at %d", c.PremiumClock, wantEnd)
	}
}
