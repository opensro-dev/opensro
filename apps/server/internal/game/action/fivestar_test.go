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
	rt.caravanThiefMarks.Store(thief.ID, caravanThiefMark{at: now, cargoScale: 1000})
	want := tunables.Current().Int(tunables.TradeFiveStarBountyPercent)
	if got := rt.fiveStarBountyPercent(hunter, thief, now); got != want {
		t.Fatalf("marked bounty = %d, want %d", got, want)
	}
	if got := rt.fiveStarBountyPercent(hunter, thief, now); got != 0 {
		t.Fatalf("bounty paid twice: %d", got)
	}
	// The flag off pays nothing (the mark survives; it is never paid).
	rt.caravanThiefMarks.Store(thief.ID, caravanThiefMark{at: now, cargoScale: 1000})
	rt.TradeTiers = false
	if got := rt.fiveStarBountyPercent(hunter, thief, now); got != 0 {
		t.Fatalf("flag-off bounty = %d, want 0", got)
	}
	// A trader who kills the marked thief earns no hunter bounty.
	rt2, _, trader, thief2 := newPvpPair(t)
	rt2.TradeTiers = true
	dressForJob(rt2, trader, 9405, "ITEM_CH_M_TRADE_TRADER_04", 1)
	dressForJob(rt2, thief2, 9406, "ITEM_CH_M_TRADE_THIEF_04", 2)
	rt2.caravanThiefMarks.Store(thief2.ID, caravanThiefMark{at: now, cargoScale: 1000})
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
			rt.caravanThiefMarks.Store(thief.ID, caravanThiefMark{at: now, cargoScale: 1000})
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
TestScaledBountyPercent

The bounty bonus follows the robbed cargo (#3162): the full configured
percent at the largest ordinary full load, half at half that cargo,
the Trade group's floor under a small load - and nothing at all when
the bounty percent itself is configured to 0, floor or not.
================
*/
func TestScaledBountyPercent(t *testing.T) {
	settings := tradeSettings(t, nil)
	for scale, want := range map[int64]int64{1000: 50, 500: 25, 317: 15, 100: 10, 0: 10} {
		if got := scaledBountyPercent(scale, settings); got != want {
			t.Fatalf("scale %d: %d%%, want %d%%", scale, got, want)
		}
	}
	off := tradeSettings(t, map[string]string{"SRO_TRADE_FIVE_STAR_BOUNTY_PERCENT": "0"})
	if got := scaledBountyPercent(1000, off); got != 0 {
		t.Fatalf("disabled bounty paid %d%%", got)
	}
	noFloor := tradeSettings(t, map[string]string{"SRO_TRADE_FIVE_STAR_BOUNTY_FLOOR_PERCENT": "0"})
	if got := scaledBountyPercent(100, noFloor); got != 5 {
		t.Fatalf("floorless small bounty = %d%%, want 5%%", got)
	}
}

/*
================
TestFiveStarBountyPerGoldAtRisk

The #3162 collusion cycle, played through the mark: a thief attacks a
brim-full 32-slot caravan and a brim-full 101-slot one, a hunter kills
the thief after each. Both caravans are 5 stars, but the small one's
mark carries 32/101 of the largest load, so its bounty bonus per gold
at risk is no larger than the big caravan's - where the pre-#3162 flat
percent paid the small cycle 101/32 times the big one's rate.
================
*/
func TestFiveStarBountyPerGoldAtRisk(t *testing.T) {
	rt, _ := traderFixture(t, false)
	rt.TradeTiers = true
	good, found := rt.deps.ItemReferences().ItemRefByCodename("ITEM_ETC_TRADE_WC_01")
	if !found {
		t.Fatal("fixture good missing")
	}
	owner := func(id int64, slots int64) *enterworld.Character {
		o := testCharacter()
		o.ID, o.Name = id, "Owner"
		o.Job = domain.CharacterJob{Type: domain.JobTrader, Grade: 1, Alias: "Owner"}
		o.MissionInventory = []domain.InventoryRow{{Slot: enterworld.JobSuitSlot,
			TypeFlags: wire.PackTypeFlags(3, 1, 7, 1), StackCount: 1}}
		rows := make([]domain.InventoryRow, 0, slots)
		for slot := int64(0); slot < slots; slot++ {
			rows = append(rows, domain.InventoryRow{Slot: slot, RefObjID: good.RefObjID,
				Codename: good.Codename, TypeFlags: good.TypeFlags(), StackCount: 40})
		}
		o.ActiveCOS = &domain.CharacterCOS{GID: uint32(6000 + id), RefObjID: 3914, Codename: "COS_T_DHORSE3",
			Summoned: true, Container: &domain.COSContainer{Capacity: uint8(slots), Rows: rows}}
		return o
	}
	dressed := func(id int64, tid4 uint8) *enterworld.Character {
		c := testCharacter()
		c.ID, c.Name = id, "Dressed"
		c.MissionInventory = []domain.InventoryRow{{Slot: enterworld.JobSuitSlot,
			TypeFlags: wire.PackTypeFlags(3, 1, 7, tid4), StackCount: 1}}
		return c
	}
	thief, hunter := dressed(9001, 2), dressed(9002, 3)
	now := int64(100_000)
	bounty := func(o *enterworld.Character) (int64, int64) {
		rt.noteCaravanAttack(thief, o, now)
		mark, marked := rt.caravanThiefMarkOf(thief, now)
		if !marked {
			t.Fatalf("attack on a full %d-slot caravan left no mark", o.ActiveCOS.Container.Capacity)
		}
		return rt.fiveStarBountyPercent(hunter, thief, now), mark.cargoScale
	}
	smallPercent, smallScale := bounty(owner(101, 32))
	largePercent, largeScale := bounty(owner(102, 101))
	if smallScale != 32000/101 || largeScale != 1000 {
		t.Fatalf("scales %d and %d, want %d and 1000", smallScale, largeScale, 32000/101)
	}
	if smallPercent != 15 || largePercent != 50 {
		t.Fatalf("bounties %d%% and %d%%, want the scaled 15%% and the full 50%%", smallPercent, largePercent)
	}
	// Per gold at risk (32 and 101 identical stacks of the same good):
	// the small cycle must not pay the higher rate.
	smallGold := rt.cargoGoldValue(owner(103, 32))
	largeGold := rt.cargoGoldValue(owner(104, 101))
	if smallPercent*int64(largeGold) > largePercent*int64(smallGold) {
		t.Fatalf("bounty per gold at risk: small %d%% of %d gold beats large %d%% of %d",
			smallPercent, smallGold, largePercent, largeGold)
	}
	// A caravan short of five stars marks nothing, however full the
	// thief's pockets: a fifth of the big bag is two stars.
	light := owner(105, 101)
	light.ActiveCOS.Container.Rows = light.ActiveCOS.Container.Rows[:21]
	rt.noteCaravanAttack(thief, light, now)
	if _, marked := rt.caravanThiefMarkOf(thief, now); marked {
		t.Fatal("attack on a 2-star caravan left a mark")
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
	// The fixture's good at its reference price of 101, a brim-full bag.
	good := c.MissionInventory[1]
	c.ActiveCOS = &enterworld.CharacterCOS{GID: 4242, Codename: "COS_TRANSPORT", Container: &domain.COSContainer{
		Capacity: 4,
		Rows:     []domain.InventoryRow{{Codename: good.Codename, TypeFlags: good.TypeFlags, StackCount: 160}},
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
