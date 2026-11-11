/*
===========================================================================

fivestar_test.go - the 5-star caravan stakes in the sale distribution
and the hunter bounty (#2958)

Port-only, not native: the stakes ride SRO_TRADE_TIERS through the
FiveStar input, so a 4-star caravan and the flag-off native sale pay
exactly the native distribution.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestFiveStarThiefCutRaisesTheGoldShare
================
*/
func TestFiveStarThiefCutRaisesTheGoldShare(t *testing.T) {
	percent := tunables.Current().Int(tunables.TradeFiveStarThiefPercent)
	for _, tc := range []struct {
		name string
		in   commerce.TradeRewardInput
	}{
		{"party of two", commerce.TradeRewardInput{Credit: 100000, Profit: 40000, Job: domain.JobThief, Party: true, Thieves: 2}},
		{"party of three", commerce.TradeRewardInput{Credit: 100000, Profit: 40000, Job: domain.JobThief, Party: true, Thieves: 3}},
		{"solo", commerce.TradeRewardInput{Credit: 100000, Profit: 40000, Job: domain.JobThief}},
	} {
		four := tc.in
		native := commerce.DistributeTradeReward(four)
		five := tc.in
		five.FiveStar = true
		got := commerce.DistributeTradeReward(five)
		applyFiveStarTradeBonus(&got, five, tunables.Current())
		if want := native.Thief.Gold + native.Thief.Gold*percent/100; got.Thief.Gold != want {
			t.Fatalf("%s: thief share %d, want %d (4-star %d)", tc.name, got.Thief.Gold, want, native.Thief.Gold)
		}
		if want := native.Actor.Gold + native.Actor.Gold*percent/100; got.Actor.Gold != want {
			t.Fatalf("%s: thief credit %d, want %d (4-star %d)", tc.name, got.Actor.Gold, want, native.Actor.Gold)
		}
		if got.Thief.Experience != native.Thief.Experience || got.Actor.Experience != native.Actor.Experience {
			t.Fatalf("%s: the cut moved job EXP: %+v vs %+v", tc.name, got, native)
		}
		// The 4-star sale pays the native distribution, bonus or not.
		unchanged := commerce.DistributeTradeReward(four)
		applyFiveStarTradeBonus(&unchanged, four, tunables.Current())
		if unchanged != native {
			t.Fatalf("%s: 4-star distribution changed: %+v vs %+v", tc.name, unchanged, native)
		}
	}
}

/*
================
TestFiveStarHunterEscortRaisesTheGoldShare
================
*/
func TestFiveStarHunterEscortRaisesTheGoldShare(t *testing.T) {
	percent := tunables.Current().Int(tunables.TradeFiveStarHunterPercent)
	in := commerce.TradeRewardInput{Credit: 100000, Profit: 40000, Job: domain.JobTrader, Party: true, Traders: 1, Hunters: 2}
	native := commerce.DistributeTradeReward(in)
	five := in
	five.FiveStar = true
	got := commerce.DistributeTradeReward(five)
	applyFiveStarTradeBonus(&got, five, tunables.Current())
	if want := native.Hunter.Gold + native.Hunter.Gold*percent/100; got.Hunter.Gold != want {
		t.Fatalf("hunter share %d, want %d (4-star %d)", got.Hunter.Gold, want, native.Hunter.Gold)
	}
	if got.Actor != native.Actor || got.Hunter.Experience != native.Hunter.Experience || got.Thief != native.Thief {
		t.Fatalf("the escort bonus moved the trader, EXP or a thief share: %+v vs %+v", got, native)
	}
	unchanged := commerce.DistributeTradeReward(in)
	applyFiveStarTradeBonus(&unchanged, in, tunables.Current())
	if unchanged != native {
		t.Fatalf("4-star distribution changed: %+v vs %+v", unchanged, native)
	}
}

/*
================
TestCaravanThiefMarkWindow
================
*/
func TestCaravanThiefMarkWindow(t *testing.T) {
	rt := &Runtime{}
	thief := &enterworld.Character{ID: 77, Name: "MarkedThief"}
	if rt.caravanThiefMarked(thief, 1000) {
		t.Fatal("an unmarked thief reads marked")
	}
	rt.caravanThiefMarks.Store(thief.ID, caravanThiefMark{at: 1000})
	if !rt.caravanThiefMarked(thief, 1000) {
		t.Fatal("a fresh mark does not read")
	}
	if !rt.caravanThiefMarked(thief, 1000+caravanRobWindowMs) {
		t.Fatal("a mark at the window edge does not read")
	}
	if rt.caravanThiefMarked(thief, 1000+caravanRobWindowMs+1) {
		t.Fatal("an expired mark still reads")
	}
	if _, ok := rt.caravanThiefMarks.Load(thief.ID); ok {
		t.Fatal("an expired mark was not dropped on read")
	}
	rt.caravanThiefMarks.Store(thief.ID, caravanThiefMark{at: 5000})
	if rt.caravanThiefMarked(thief, 4000) {
		t.Fatal("a mark from the future reads")
	}
}

/*
================
TestFiveStarBountyPaysOnce
================
*/
func TestFiveStarBountyPaysOnce(t *testing.T) {
	rt, _, hunter, thief := newPvpPair(t)
	rt.TradeTiers = true
	dressForJob(rt, hunter, 9403, "ITEM_CH_M_TRADE_HUNTER_04", 3)
	dressForJob(rt, thief, 9404, "ITEM_CH_M_TRADE_THIEF_04", 2)
	now := rt.Now().UnixMilli()
	if got := rt.fiveStarBountyPercent(hunter, thief, now); got != 0 {
		t.Fatalf("unmarked bounty = %d, want 0", got)
	}
	rt.caravanThiefMarks.Store(thief.ID, caravanThiefMark{at: now})
	want := tunables.Current().Int(tunables.TradeFiveStarBountyPercent)
	if got := rt.fiveStarBountyPercent(hunter, thief, now); got != want {
		t.Fatalf("marked bounty = %d, want %d", got, want)
	}
	if got := rt.fiveStarBountyPercent(hunter, thief, now); got != 0 {
		t.Fatalf("bounty paid twice: %d", got)
	}
	// The flag off pays nothing (the mark survives; it is never paid).
	rt.caravanThiefMarks.Store(thief.ID, caravanThiefMark{at: now})
	rt.TradeTiers = false
	if got := rt.fiveStarBountyPercent(hunter, thief, now); got != 0 {
		t.Fatalf("flag-off bounty = %d, want 0", got)
	}
	// A trader who kills the marked thief earns no hunter bounty.
	rt2, _, trader, thief2 := newPvpPair(t)
	rt2.TradeTiers = true
	dressForJob(rt2, trader, 9405, "ITEM_CH_M_TRADE_TRADER_04", 1)
	dressForJob(rt2, thief2, 9406, "ITEM_CH_M_TRADE_THIEF_04", 2)
	rt2.caravanThiefMarks.Store(thief2.ID, caravanThiefMark{at: now})
	if got := rt2.fiveStarBountyPercent(trader, thief2, now); got != 0 {
		t.Fatalf("trader bounty = %d, want 0", got)
	}
}

/*
================
TestFiveStarBountyRaisesTheJobKillPayoff

The kill door itself: a hunter killing a marked thief banks the native
job-kill EXP raised by the bounty percent; an unmarked thief pays the
native amount, and with the flag off the mark pays nothing.
================
*/
func TestFiveStarBountyRaisesTheJobKillPayoff(t *testing.T) {
	var wantNative int64
	pay := func(marked, tiers bool) int64 {
		rt, clock, hunter, thief := newPvpPair(t)
		rt.TradeTiers = tiers
		dressForJob(rt, hunter, 9451, "ITEM_CH_M_TRADE_HUNTER_04", 3)
		dressForJob(rt, thief, 9452, "ITEM_CH_M_TRADE_THIEF_04", 2)
		rt.deps.(*enterworld.Deps).Levels = jobKillLevels{LevelDataSource: rt.deps.LevelData(), gold: map[int64]int64{1: 80}}
		wantNative = jobKillExperience(rt.deps.LevelData(), hunter, rewardLevel(thief))
		var paid int64
		rt.UpdateJobExperience = func(c *enterworld.Character, delta int64) ([]wire.Frame, bool) {
			if c == hunter {
				paid += delta
			}
			return nil, true
		}
		now := clock.NowMs()
		if marked {
			rt.caravanThiefMarks.Store(thief.ID, caravanThiefMark{at: now})
		}
		kill := playerKill{kind: pk.DeathJob}.withVictim(hunter, thief)
		_, _, after := rt.payPlayerKillInDoor(testDivision, hunter, thief, kill, now)
		rt.payJobKillShares(after)
		return paid
	}
	native := pay(false, true)
	if native <= 0 || native != wantNative {
		t.Fatalf("unmarked job kill paid %d, want the native %d", native, wantNative)
	}
	percent := tunables.Current().Int(tunables.TradeFiveStarBountyPercent)
	if got, want := pay(true, true), native+native*percent/100; got != want {
		t.Fatalf("marked kill paid %d, want %d (native %d raised %d%%)", got, want, native, percent)
	}
	if got := pay(true, false); got != native {
		t.Fatalf("flag-off marked kill paid %d, want native %d", got, native)
	}
}

/*
================
TestCaravanStarFramesListOnlyFiveStarTransports

The nameplate roster: a transport past the 5-star mark is listed by gid,
at the push pace and no faster; an emptied caravan clears in an empty
roster; with the flag off nothing is ever sent.
================
*/
func TestCaravanStarFramesListOnlyFiveStarTransports(t *testing.T) {
	rt, c := traderFixture(t, false)
	rt.TradeTiers = true
	// The fixture's good at its reference price of 101, stacked past the 5-star mark.
	good := c.MissionInventory[1]
	c.ActiveCOS = &enterworld.CharacterCOS{GID: 4242, Codename: "COS_TRANSPORT", Container: &domain.COSContainer{
		Rows: []domain.InventoryRow{{Codename: good.Codename, TypeFlags: good.TypeFlags, StackCount: 500_000}},
	}}
	views := []simulation.SessionView{{SessionID: "s1", DivisionID: testDivision, CharacterID: c.ID}}
	frames := rt.CaravanStarFrames(testDivision, views, 10_000)
	if len(frames) != 1 || frames[0].Opcode != wire.OpCaravanStars {
		t.Fatalf("frames %+v, want one caravan-star frame", frames)
	}
	payload := frames[0].Payload
	if len(payload) != 6 || binary.LittleEndian.Uint16(payload) != 1 ||
		binary.LittleEndian.Uint32(payload[2:]) != 4242 {
		t.Fatalf("payload % X, want one gid 4242", payload)
	}
	if frames := rt.CaravanStarFrames(testDivision, views, 10_000); frames != nil {
		t.Fatalf("off-pace frames %+v, want none", frames)
	}
	c.ActiveCOS.Container.Rows[0].StackCount = 1
	frames = rt.CaravanStarFrames(testDivision, views, 10_000+caravanStarPushIntervalMs)
	if len(frames) != 1 || !bytes.Equal(frames[0].Payload, []byte{0, 0}) {
		t.Fatalf("cleared frames %+v, want an empty roster", frames)
	}
	rt.TradeTiers = false
	rt.caravanStarDueMs = 0
	if frames := rt.CaravanStarFrames(testDivision, views, 99_000); frames != nil {
		t.Fatalf("flag-off frames %+v, want none", frames)
	}
}

/*
================
TestCaravanStarsOpcodeIsNotTheCrowVisual

The roster must never take the pet-crow visual's opcode (#2968
review): the client decodes 0x3FBB strictly as a crow frame and each
owner throws on the other's body, so a shared opcode breaks both.
================
*/
func TestCaravanStarsOpcodeIsNotTheCrowVisual(t *testing.T) {
	if wire.OpCaravanStars == crowVisualOpcode {
		t.Fatalf("caravan roster opcode %#04x collides with the pet-crow visual", wire.OpCaravanStars)
	}
}
