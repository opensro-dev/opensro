/*
===========================================================================

hwan_test.go - the Blue Berserk: absorption, scroll rules, charge and frames

The scroll flow runs over the shipped items when the build has the cap-110
Blue Berserk items (it skips otherwise); the rules that need no data are
tested on plain values.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

func TestHwanAbsorptionIsTenPlusFivePerLevelOnlyWhileBerserk(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), nil)
	level := uint8(0)
	rt.HwanLevel = func(*enterworld.Character) uint8 { return level }
	c := testCharacter()
	for want, lv := range map[float32]uint8{10: 1, 15: 2, 20: 3, 25: 4, 30: 5, 35: 6} {
		level = lv
		if writes := rt.hwanWrites(c); writes != nil {
			t.Fatalf("level %d absorbed outside Berserk: %v", lv, writes)
		}
		c.NativeBodyStatus = 1
		writes := rt.hwanWrites(c)
		if len(writes) != 4 {
			t.Fatalf("level %d wrote %d keepers, want the four incoming-damage ones", lv, len(writes))
		}
		for _, write := range writes {
			if write.Value != -want || write.Source != hwanModifierSource || write.Parameter < 0xae || write.Parameter > 0xb1 {
				t.Fatalf("level %d wrote %+v, want -%v on 0xAE..0xB1", lv, write, want)
			}
		}
		c.NativeBodyStatus = 0
	}
	level = 0
	c.NativeBodyStatus = 1
	if rt.hwanWrites(c) != nil {
		t.Fatal("level 0 absorbed damage")
	}
	rt.HwanLevel = nil
	level = 3
	if rt.hwanWrites(c) != nil {
		t.Fatal("a runtime without the quest owner absorbed damage")
	}
}

func TestHwanLevelFrameCarriesTheObjectAndTheLevel(t *testing.T) {
	frame := HwanLevelFrame(0x01020304, 4)
	if frame.Opcode != 0x3FBF || string(frame.Payload) != string([]byte{4, 3, 2, 1, 4}) {
		t.Fatalf("frame = %#x % X", frame.Opcode, frame.Payload)
	}
	c := testCharacter()
	rt, _ := newTestRuntime(c, nil)
	if len(rt.HwanEntryFrames(c)) != 0 || len(rt.HwanLevelFrames(c)) != 0 {
		t.Fatal("level 0 sent frames")
	}
	rt.HwanLevel = func(*enterworld.Character) uint8 { return 2 }
	if got := rt.HwanEntryFrames(c); len(got) != 1 || got[0].Opcode != 0x3FBF {
		t.Fatalf("entry frames %+v", got)
	}
	notice := HwanTitleNotice(c, 2)
	if notice.Opcode != 0x36bf || string(notice.Payload[2:]) != "UIO_SECOND_HWAN_CH_NAME_LEVEL_2" {
		t.Fatalf("title notice %q", notice.Payload)
	}
}

func TestBerserkLengthAddsTheThpSeconds(t *testing.T) {
	c := testCharacter()
	now := int64(1_000_000)
	if hwanBerserkExtraMs(c, now) != 0 {
		t.Fatal("extra time without a row")
	}
	c.ParamJobs = []domain.ParamJob{{ItemRefObjID: 7, Param: paramBerserkTime, Value: 8, EndUnixMs: now + 5000}}
	if got := hwanBerserkExtraMs(c, now); got != 8000 {
		t.Fatalf("extra time %d, want 8000", got)
	}
	if hwanBerserkExtraMs(c, now+6000) != 0 {
		t.Fatal("an expired row still extended the Berserk")
	}
}

// energyOfLifeRuntime is a Chinese character holding a tier-1 scroll in slot 40 on the shipped items.
func energyOfLifeRuntime(t *testing.T) (*Runtime, *enterworld.Character, *fakeClock, *enterworld.ItemRef, *uint8) {
	t.Helper()
	clientdata.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	scroll, ok := items.ItemRefByCodename("ITEM_ETC_SECOND_HWAN_SCROLL_01")
	if !ok {
		t.Skip("the built data has no Energy of Life scrolls (SRO_CAP110_ITEMS)")
	}
	c := testCharacter()
	*c.Level = 95
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 40, RefObjID: scroll.RefObjID,
		Codename: scroll.Codename, TypeFlags: scroll.TypeFlags(), StackCount: 3})
	rt, clock := newTestRuntime(c, items)
	rt.deps.(*enterworld.Deps).Skills = enterworld.NewTextdataSkills(dir)
	level := new(uint8)
	rt.HwanLevel = func(*enterworld.Character) uint8 { return *level }
	return rt, c, clock, scroll, level
}

func TestEnergyOfLifeScrollNeedsTheBlueLevelItNames(t *testing.T) {
	rt, c, _, scroll, level := energyOfLifeRuntime(t)
	body := wire.NewWriter(3).U8(40).U16(scroll.TypeFlags()).Payload()
	assertItemUseRefusedUnchanged(t, rt, c, body, errCodeHwanLevel)
	*level = 1
	if out := rt.HandleItemUse(testDivision, c, body); len(out.Frames) == 0 || out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("a level 1 Blue Berserk could not use tier 1: %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
}

func TestEnergyOfLifeRowsChargeAndCooldown(t *testing.T) {
	rt, c, clock, scroll, level := energyOfLifeRuntime(t)
	*level = 1
	body := wire.NewWriter(3).U8(40).U16(scroll.TypeFlags()).Payload()
	if out := rt.HandleItemUse(testDivision, c, body); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("scroll use: %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	// The buff and the thp row are on the bar; the scroll started no cooldown.
	var buff domain.CompositeJob
	for _, work := range c.CompositeJobs {
		if work.TargetCodename == hwanChargeItem {
			buff = work
		}
	}
	if buff.Target == 0 || buff.Uses != 1 {
		t.Fatalf("composite works %+v: no Energy of Life buff", c.CompositeJobs)
	}
	if got := paramJobPercent(c, paramBerserkTime, clock.NowMs()); got != 5 {
		t.Fatalf("thp extra seconds %d, want 5", got)
	}
	if len(c.ItemGroupCooldowns) != 0 {
		t.Fatalf("the scroll started a cooldown: %v", c.ItemGroupCooldowns)
	}
	// A second scroll is refused while the rows are on the bar (port-only).
	assertItemUseRefusedUnchanged(t, rt, c, body, wire.ErrCodeItemReuseDelay)

	// The charge is refused in Berserk (the potion's 0xCE) and keeps everything.
	use := wire.NewWriter(8).U32(buff.PackageRefObjID).U32(buff.Target).Payload()
	c.NativeBodyStatus = 1
	if out := rt.HandleCountJobUse(testDivision, c, use); out.Frames[0].Payload[0] != wire.ResultError || out.Frames[0].Payload[1] != hwanBerserkFailure {
		t.Fatalf("charge in Berserk = % X", out.Frames[0].Payload)
	}
	if len(c.CompositeJobs) == 0 || len(c.ParamJobs) == 0 || len(c.ItemGroupCooldowns) != 0 {
		t.Fatal("a refused charge changed the bar or started the cooldown")
	}
	c.NativeBodyStatus = 0

	// The charge fills the gauge, clears both rows and starts the 20 minute group.
	out := rt.HandleCountJobUse(testDivision, c, use)
	if out.Frames[0].Opcode != wire.OpCountJobAnswer || out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("charge = %+v", out.Frames)
	}
	if c.BerserkPoints != 5 {
		t.Fatalf("berserk points %d, want 5", c.BerserkPoints)
	}
	for _, work := range c.CompositeJobs {
		if work.TargetCodename == hwanChargeItem {
			t.Fatal("the buff stayed on the bar")
		}
	}
	if paramJobPercent(c, paramBerserkTime, clock.NowMs()) != 0 {
		t.Fatal("the extra Berserk time stayed after the charge")
	}
	ends := 0
	for _, frame := range out.Frames {
		if frame.Opcode == wire.OpCountJobEnd || frame.Opcode == wire.OpParamJobEnd {
			ends++
		}
	}
	if ends != 2 {
		t.Fatalf("%d end frames, want the count row and the param row", ends)
	}
	until := c.ItemGroupCooldowns[hwanChargeGroup]
	if until != clock.NowMs()+hwanChargeCooldownMs {
		t.Fatalf("cooldown until %d, want %d", until, clock.NowMs()+hwanChargeCooldownMs)
	}
	// Every tier shares it: refused for 20 minutes, then allowed again.
	assertItemUseRefusedUnchanged(t, rt, c, body, wire.ErrCodeItemReuseDelay)
	clock.Advance(time.Duration(hwanChargeCooldownMs+1) * time.Millisecond)
	if out := rt.HandleItemUse(testDivision, c, body); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("scroll after the cooldown: %+v", out.Frames)
	}
}

func TestGMSetBerserkChangesTheLevelAndTellsTheTarget(t *testing.T) {
	gm, target := testCharacter(), testCharacter()
	gm.ID, gm.Name, gm.GMPrivilege = 1, "Gm", true
	target.ID, target.Name = 2, "Ayla"
	rt, _ := newTestRuntime(gm, nil)
	rt.deps.(*enterworld.Deps).Characters = enterworld.StaticCharacterSource{testDivision: {gm, target}}
	levels := map[int64]uint8{}
	rt.HwanLevel = func(c *enterworld.Character) uint8 { return levels[c.ID] }
	rt.SetHwanLevel = func(c *enterworld.Character, level uint8) bool { levels[c.ID] = level; return true }
	pushed := map[string][]wire.Frame{}
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) { pushed[name] = append(pushed[name], frames...) }

	if !rt.SetGMBerserkLevel(testDivision, "Gm", "Ayla", 5) || levels[2] != 5 {
		t.Fatalf("level %d after /SETBERSERK Ayla 5", levels[2])
	}
	got := pushed["Ayla"]
	if len(got) != 2 || got[0].Opcode != OpHwanLevel || got[1].Opcode != 0x36bf {
		t.Fatalf("Ayla was told %+v, want the level frame and its title", got)
	}
	// Level 0 clears it and sends the level frame alone.
	pushed = map[string][]wire.Frame{}
	if !rt.SetGMBerserkLevel(testDivision, "Gm", "Ayla", 0) || levels[2] != 0 || len(pushed["Ayla"]) != 1 {
		t.Fatalf("level 0: levels %v pushed %v", levels, pushed)
	}
	// Too high, an unknown name and a non-GM change nothing.
	if rt.SetGMBerserkLevel(testDivision, "Gm", "Ayla", 7) || rt.SetGMBerserkLevel(testDivision, "Gm", "Nobody", 2) {
		t.Fatal("an invalid level or an unknown character was accepted")
	}
	gm.GMPrivilege = false
	if rt.SetGMBerserkLevel(testDivision, "Gm", "Ayla", 2) || levels[2] != 0 {
		t.Fatal("a non-GM set a level")
	}
}
