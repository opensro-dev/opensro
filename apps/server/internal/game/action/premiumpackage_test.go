/*
===========================================================================

premiumpackage_test.go - the Gold Time packages on the shipped data

Expectations follow 49F590's entry kinds and CUsedItemLimit (654080,
653C30, 653A40) over the v1.150 itemdata and skilldata rows.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
goldTimeRuntime

A level-1 character holding ITEM_MALL_PREMIUM_GOLDTIME in bag slot 40,
with the shipped items and skills.
================
*/
func goldTimeRuntime(t *testing.T) (*Runtime, *enterworld.Character, *fakeClock, *enterworld.ItemRef) {
	t.Helper()
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	pack, ok := items.ItemRefByCodename("ITEM_MALL_PREMIUM_GOLDTIME")
	if !ok {
		t.Fatal("shipped Gold Time package missing")
	}
	c := testCharacter()
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 40, RefObjID: pack.RefObjID,
		Codename: pack.Codename, TypeFlags: pack.TypeFlags(), StackCount: 1})
	rt, clock := newTestRuntime(c, items)
	rt.deps.(*enterworld.Deps).Skills = enterworld.NewTextdataSkills(dir)
	return rt, c, clock, pack
}

/*
================
TestGoldTimeEntriesSpanThreeDescriptions
================
*/
func TestGoldTimeEntriesSpanThreeDescriptions(t *testing.T) {
	_, _, _, pack := goldTimeRuntime(t)
	entries, ok := compositeEntries(pack)
	if !ok {
		t.Fatal("Gold Time entries did not parse")
	}
	var tags []string
	for _, entry := range entries {
		tags = append(tags, entry.tag)
	}
	want := []string{"UIU1", "UIL1", "USU1", "UIL1", "UIL1", "BFI1", "UQL1"}
	if len(tags) != len(want) {
		t.Fatalf("tags = %v, want %v", tags, want)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Fatalf("tags = %v, want %v", tags, want)
		}
	}
	if args := entries[1].args; len(args) != 4 || args[0] != "ITEM_MALL_RESURRECTION_100P_SCROLL" || args[3] != "1" {
		t.Fatalf("resurrection limit args = %q", args)
	}
}

/*
================
TestGoldTimePackageInstallsEveryEntry

The ticket's keepers and clock, the APRU buff (its alchemy luck reaches
the Alchemy bonus), three limited uses on the board, the booth and the
quest limit, all from one use.
================
*/
func TestGoldTimePackageInstallsEveryEntry(t *testing.T) {
	rt, c, clock, pack := goldTimeRuntime(t)
	out := rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(40).U16(pack.TypeFlags()).Payload())
	if len(out.Frames) == 0 || out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("package use = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	if c.PremiumClock == nil || !premiumRunning(c, clock.NowMs()) {
		t.Fatal("the package's premium ticket did not start")
	}
	kinds := map[string]int{}
	for _, job := range c.CompositeJobs {
		kinds[job.Kind]++
	}
	if kinds[domain.CompositeUsedItemLimit] != 3 || kinds[domain.CompositeBuffItem] != 1 || kinds[domain.CompositeUsedQuestLimit] != 1 {
		t.Fatalf("composite works = %v", kinds)
	}
	starts := 0
	for _, frame := range out.Frames {
		if frame.Opcode == wire.OpCountJobStart {
			starts++
		}
	}
	if starts != 3 {
		t.Fatalf("board rows raised = %d, want 3", starts)
	}
	if reinforce, stone := rt.alchemyBonuses(testDivision, c); reinforce == 0 || stone == 0 {
		t.Fatalf("APRU alchemy bonuses = %d/%d", reinforce, stone)
	}
	// A second package while the first runs answers 0x1894.
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 41, RefObjID: pack.RefObjID,
		Codename: pack.Codename, TypeFlags: pack.TypeFlags(), StackCount: 1})
	assertItemUseRefusedUnchanged(t, rt, c, wire.NewWriter(3).U8(41).U16(pack.TypeFlags()).Payload(), errCodePremiumActive)
}

/*
================
TestGoldTimeLimitedResurrectionRefillsDaily

/Resurrection revives once a day without an item: a second use the same
day is over the limit, and the next day's refill allows it again.
================
*/
func TestGoldTimeLimitedResurrectionRefillsDaily(t *testing.T) {
	rt, c, clock, pack := goldTimeRuntime(t)
	rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(40).U16(pack.TypeFlags()).Payload())
	var job domain.CompositeJob
	for _, candidate := range c.CompositeJobs {
		if candidate.TargetCodename == "ITEM_MALL_RESURRECTION_100P_SCROLL" {
			job = candidate
		}
	}
	if job.ID == 0 || job.Uses != 1 {
		t.Fatalf("resurrection work = %+v", job)
	}
	use := wire.NewWriter(8).U32(job.ID).U32(job.Target).Payload()
	if out := rt.HandleCountJobUse(testDivision, c, use); out.Frames[0].Payload[0] != wire.ResultError ||
		out.Frames[0].Payload[1] != errCodeOnlyDeadResurrect {
		t.Fatalf("alive use = % X, want the scroll's 0x87", out.Frames[0].Payload)
	}
	kill := func() {
		zero := int64(0)
		rt.deps.Update(c, "test-death", func() bool { c.CurrentHP = &zero; return true })
	}
	kill()
	if out := rt.HandleCountJobUse(testDivision, c, use); out.Frames[0].Opcode != wire.OpCountJobAnswer ||
		out.Frames[0].Payload[0] != wire.ResultSuccess || !enterworld.CharacterAlive(c) {
		t.Fatalf("dead use = %+v", out.Frames)
	}
	kill()
	if out := rt.HandleCountJobUse(testDivision, c, use); out.Frames[0].Payload[1] != errCodePremiumUseOver {
		t.Fatalf("second use the same day = % X, want 0xC6", out.Frames[0].Payload)
	}
	clock.Advance(24 * time.Hour)
	rt.advanceParamJobs(clock.NowMs())
	if out := rt.HandleCountJobUse(testDivision, c, use); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("next day use = % X", out.Frames[0].Payload)
	}
}
