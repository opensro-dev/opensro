/*
===========================================================================

job_rewards_test.go - what a job player earns from a kill and a sale

#bug-reports (2026-10-10) said killing the opposite job and a trade run
pay nothing. These pin both rewards end to end against the shipped level
table: a job kill pays job EXP and three times the lower level's monster
basis, and a trader's profitable sale pays the hunters
in its party their share.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
dressForJob

Wears a job suit of the given TID4 (2 thief, 3 hunter, 1 trader).
================
*/
func dressForJob(rt *Runtime, c *enterworld.Character, id uint32, codename string, tid4 int64) {
	items := rt.deps.ItemReferences().(staticItemSource)
	suit := &enterworld.ItemRef{RefObjID: id, Codename: codename, Country: 3, TypeIDs: [4]int64{3, 1, 7, tid4},
		ReqQuadTypes: [4]int64{-1, -1, -1, -1}, Combat: &enterworld.ItemCombatRef{}}
	items[suit.Codename] = suit
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(jobSuitSlot),
		RefObjID: suit.RefObjID, Codename: suit.Codename, TypeFlags: suit.TypeFlags(), StackCount: 1})
}

/*
================
TestHunterKillingAThiefEarnsJobAndCharacterExp

Level 30 against level 30 on the shipped leveldata: job EXP from the
withdrawal-gold basis, and 3 x 842 character EXP from the monster basis.
================
*/
func TestHunterKillingAThiefEarnsJobAndCharacterExp(t *testing.T) {
	rt, _, hunter, thief := newPvpPair(t)
	level := int64(30)
	hunter.Level, thief.Level = &level, &level
	dressForJob(rt, hunter, 9301, "ITEM_CH_M_TRADE_HUNTER_04", 3)
	dressForJob(rt, thief, 9302, "ITEM_CH_M_TRADE_THIEF_04", 2)
	levels := enterworld.NewTextdataLevels(gamedatatest.TextdataDir(t))
	rt.deps.(*enterworld.Deps).Levels = levels
	basis, ok := levels.MonsterExpBasis(30)
	if !ok || basis <= 0 {
		t.Fatalf("no monster EXP basis for level 30")
	}
	var job, exp int64
	rt.UpdateJobExperience = func(c *enterworld.Character, delta int64) ([]wire.Frame, bool) {
		if c == hunter {
			job += delta
		}
		return nil, true
	}
	rt.UpdateExperience = func(c *enterworld.Character, delta, _ int64, _ uint32) ([]wire.Frame, bool) {
		if c == hunter {
			exp += delta
		}
		return nil, true
	}
	kill := playerKill{kind: rt.playerKillKind(testDivision, thief, hunter)}.withVictim(hunter, thief)
	rt.payPlayerKillInDoor(testDivision, hunter, thief, kill, 0)
	// The shipped table's level-30 withdrawal-gold basis gives 162.
	if want := int64(162); job != want || jobKillExperience(levels, hunter, 30) != want {
		t.Fatalf("job EXP %d, want %d", job, want)
	}
	if exp != basis*jobKillExpMultiplier {
		t.Fatalf("character EXP %d, want %d", exp, basis*jobKillExpMultiplier)
	}
}

/*
================
TestTraderSaleSharesWithPartyHunters

A dressed trader's profitable sale pays a dressed hunter in its party,
and nobody outside the job.
================
*/
func TestTraderSaleSharesWithPartyHunters(t *testing.T) {
	rt, _, trader, hunter := newPvpPair(t)
	dressForJob(rt, trader, 9303, "ITEM_CH_M_TRADE_TRADER_04", 1)
	dressForJob(rt, hunter, 9304, "ITEM_CH_M_TRADE_HUNTER_04", 3)
	plain := testCharacter()
	plain.ID, plain.Name = 99, "plain"
	roster := tradeRoster{members: []*enterworld.Character{trader, hunter, plain}, party: true}
	payouts := rt.tradeRewards(testDivision, trader, roster, commerce.TradeRewardInput{Credit: 100000, Profit: 40000}, tunables.Current())
	paid := map[string]commerce.TradeReward{}
	for _, payout := range payouts {
		paid[payout.character.Name] = payout.TradeReward
	}
	if _, ok := paid[trader.Name]; !ok {
		t.Fatalf("the trader was not paid: %v", paid)
	}
	if share, ok := paid[hunter.Name]; !ok || share.Gold <= 0 || share.Experience <= 0 {
		t.Fatalf("the party hunter's share %+v (paid %v)", share, ok)
	}
	if _, ok := paid[plain.Name]; ok {
		t.Fatal("a member without a job suit was paid")
	}
	if enterworld.DressedJob(hunter) != domain.JobHunter {
		t.Fatal("the hunter is not dressed")
	}
}
