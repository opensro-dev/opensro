/*
===========================================================================

honor_buffs_test.go - Training Camp Honor Rank buffs and Honor Shop purchase

===========================================================================
*/

package action

import (
	"reflect"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/platform/jobhonor"
)

/*
================
TestHonorBuffSkillIDsForRank
================
*/
func TestHonorBuffSkillIDsForRank(t *testing.T) {
	if got := HonorBuffSkillIDsForRank(1); !reflect.DeepEqual(got, []uint32{20160, 20161, 20162, 20163}) {
		t.Fatalf("rank 1 = %v, want [20160 20161 20162 20163]", got)
	}
	if got := HonorBuffSkillIDsForRank(2); !reflect.DeepEqual(got, []uint32{20160, 20161, 20162}) {
		t.Fatalf("rank 2 = %v, want [20160 20161 20162]", got)
	}
	if got := HonorBuffSkillIDsForRank(3); !reflect.DeepEqual(got, []uint32{20160, 20161}) {
		t.Fatalf("rank 3 = %v, want [20160 20161]", got)
	}
	if got := HonorBuffSkillIDsForRank(4); !reflect.DeepEqual(got, []uint32{20160}) {
		t.Fatalf("rank 4 = %v, want [20160]", got)
	}
	if got := HonorBuffSkillIDsForRank(0); got != nil {
		t.Fatalf("rank 0 = %v, want nil", got)
	}
	if got := HonorBuffSkillIDsForRank(5); got != nil {
		t.Fatalf("rank 5 = %v, want nil", got)
	}
}

/*
================
TestSyncTrainingCampHonorBuffsAndCrownBerserkGain
================
*/
func TestSyncTrainingCampHonorBuffsAndCrownBerserkGain(t *testing.T) {
	rt, _, killer, _ := newPvpPair(t)
	skills := rt.deps.SkillData().(staticSkillSource)
	for _, code := range []string{
		"SKILL_TC_COPPER_BUFF_01",
		"SKILL_TC_SILVER_BUFF_01",
		"SKILL_TC_GOLD_BUFF_01",
		"SKILL_TC_CROWN_BUFF_01",
	} {
		row := shippedOffense(t, code)
		skills[row.ID] = row
	}
	var charFrames, peerFrames []wire.Frame
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) {
		if name == killer.Name {
			charFrames = append(charFrames, frames...)
		}
	}
	rt.PushDivisionPeerFrames = func(_, name string, frames []wire.Frame) {
		if name == killer.Name {
			peerFrames = append(peerFrames, frames...)
		}
	}

	// Rank 1 (King) installs all four Honor buffs (20160..20163).
	rt.SyncTrainingCampHonorBuffs(testDivision, killer.ID, 1)
	snap := rt.effects.Snapshot(testDivision, killer.Name)
	have := make(map[uint32]bool)
	silverMove := uint32(0)
	for _, e := range snap {
		if isHonorBuffSkillID(e.SkillID) {
			have[e.SkillID] = true
			if !e.DeathProtected || e.ClientCancelable {
				t.Fatalf("honor buff %d DeathProtected=%v ClientCancelable=%v, want true/false", e.SkillID, e.DeathProtected, e.ClientCancelable)
			}
			if e.SkillID == HonorBuffSilverSkillID && e.Movement {
				silverMove = e.MovementPercent
			}
		}
	}
	for _, want := range []uint32{20160, 20161, 20162, 20163} {
		if !have[want] {
			t.Fatalf("rank 1 missing honor buff %d (have %v)", want, have)
		}
	}
	if silverMove != 20 {
		t.Fatalf("silver buff movement percent = %d, want 20", silverMove)
	}
	if !rt.hasCrownHonorBuff(testDivision, killer) {
		t.Fatal("hasCrownHonorBuff = false at rank 1, want true")
	}
	if len(charFrames) == 0 || len(peerFrames) == 0 {
		t.Fatalf("rank 1 sync published char=%d peer=%d frames, want non-empty", len(charFrames), len(peerFrames))
	}

	// Roll 850 fails at 100% gain rate (threshold 800) and succeeds at 120% (threshold 960).
	if got := berserkKillAwardWithGainRate(60, 60, 0, 100, 850); got != 0 {
		t.Fatalf("100%% gain at roll 850 = %d, want 0", got)
	}
	if got := berserkKillAwardWithGainRate(60, 60, 0, 120, 850); got != 1 {
		t.Fatalf("120%% gain at roll 850 = %d, want 1", got)
	}

	// Demote to Rank 4 (Bronze): retires 20161..20163 and retains 20160.
	rt.SyncTrainingCampHonorBuffs(testDivision, killer.ID, 4)
	snap = rt.effects.Snapshot(testDivision, killer.Name)
	have = make(map[uint32]bool)
	for _, e := range snap {
		if isHonorBuffSkillID(e.SkillID) {
			have[e.SkillID] = true
		}
	}
	if !have[20160] || len(have) != 1 {
		t.Fatalf("rank 4 honor buffs = %v, want only 20160", have)
	}
	if rt.hasCrownHonorBuff(testDivision, killer) {
		t.Fatal("hasCrownHonorBuff = true at rank 4, want false")
	}

	// Unranked (0): retires 20160.
	rt.SyncTrainingCampHonorBuffs(testDivision, killer.ID, 0)
	for _, e := range rt.effects.Snapshot(testDivision, killer.Name) {
		if isHonorBuffSkillID(e.SkillID) {
			t.Fatalf("unranked retained honor buff %d", e.SkillID)
		}
	}
}

/*
================
TestHonorStoreShopBuyRequiresGoldAndHonorPoints
================
*/
func TestHonorStoreShopBuyRequiresGoldAndHonorPoints(t *testing.T) {
	rt, c := merchantFixture(t)
	gold := int64(1000)
	c.Gold = &gold
	c.MissionInventory = nil
	offer := rt.Commerce.Tabs[1][0]
	offer.Price = 400
	offer.HonorPrice = 70
	rt.Commerce.Tabs[1][0] = offer

	honorBalance := uint32(30)
	rt.HonorPoints = func(division string, character *enterworld.Character) uint32 {
		return honorBalance
	}
	rt.SpendHonorPoints = func(division string, character *enterworld.Character, cost uint32) bool {
		if honorBalance < cost {
			return false
		}
		honorBalance -= cost
		return true
	}

	// Short on Honor points (30 < 70): refuses with ErrCodeNotEnoughHonor (0xD4).
	res := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopBuy, NpcGID: 17, ShopTab: 0, ShopSlot: 2, Quantity: 1})
	if len(res.Frames) != 1 || len(res.Frames[0].Payload) != 2 || res.Frames[0].Payload[0] != 2 || res.Frames[0].Payload[1] != wire.ErrCodeNotEnoughHonor {
		t.Fatalf("short honor response = %+v, want {2, 0xD4}", res.Frames)
	}
	if goldOf(c) != 1000 || honorBalance != 30 || len(c.MissionInventory) != 0 {
		t.Fatalf("short honor mutated state: gold=%d honor=%d inv=%d", goldOf(c), honorBalance, len(c.MissionInventory))
	}

	// Sufficient Gold (1000 >= 400) and Honor (100 >= 70): succeeds and deducts both.
	honorBalance = 100
	res = trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopBuy, NpcGID: 17, ShopTab: 0, ShopSlot: 2, Quantity: 1})
	if goldOf(c) != 600 || honorBalance != 30 || len(c.MissionInventory) != 1 {
		t.Fatalf("settled state: gold=%d honor=%d inv=%d, want 600/30/1", goldOf(c), honorBalance, len(c.MissionInventory))
	}
}

// tierSink is a job honor sink holding a fixed tier, for the tier
// buff fold: nothing ever scores into it.
type tierSink struct{ tier int }

/*
================
tierSink.AwardJobHonor
================
*/
func (s tierSink) AwardJobHonor(string, int64, int, int, time.Time) int { return 0 }

/*
================
tierSink.HeldTier
================
*/
func (s tierSink) HeldTier(int64, int) int { return s.tier }

/*
================
TestTheTierRankMapping
================
*/
func TestTheTierRankMapping(t *testing.T) {
	for tier, want := range map[int]uint8{
		jobhonor.TierKing: 1, jobhonor.TierGold: 2, jobhonor.TierSilver: 3,
		jobhonor.TierNone: 0, 99: 0,
	} {
		if got := jobHonorTierBuffRank(tier); got != want {
			t.Fatalf("tier %d grants rank %d, want %d", tier, got, want)
		}
	}
	for _, tc := range []struct{ a, b, want uint8 }{
		{0, 0, 0}, {0, 3, 3}, {2, 0, 2}, {1, 3, 1}, {3, 2, 2}, {4, 4, 4},
	} {
		if got := strongerHonorRank(tc.a, tc.b); got != tc.want {
			t.Fatalf("stronger(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

/*
================
TestAJobTierGrantsItsAcademyBuffWhileDressed

The fold (#3530): a Silver tier holder dressed as a trader carries
the Silver buff set with no camp rank at all; undressed it carries
nothing; a stronger camp rank still wins over the tier.
================
*/
func TestAJobTierGrantsItsAcademyBuffWhileDressed(t *testing.T) {
	rt, _, char, _ := newPvpPair(t)
	skills := rt.deps.SkillData().(staticSkillSource)
	for _, code := range []string{
		"SKILL_TC_COPPER_BUFF_01",
		"SKILL_TC_SILVER_BUFF_01",
		"SKILL_TC_GOLD_BUFF_01",
		"SKILL_TC_CROWN_BUFF_01",
	} {
		row := shippedOffense(t, code)
		skills[row.ID] = row
	}
	rt.JobHonor = tierSink{tier: jobhonor.TierSilver}
	have := func() map[uint32]bool {
		out := map[uint32]bool{}
		for _, e := range rt.effects.Snapshot(testDivision, char.Name) {
			if isHonorBuffSkillID(e.SkillID) {
				out[e.SkillID] = true
			}
		}
		return out
	}

	// Undressed, the tier grants nothing.
	rt.SyncTrainingCampHonorBuffs(testDivision, char.ID, 0)
	if got := have(); len(got) != 0 {
		t.Fatalf("undressed tier holder carries %v", got)
	}

	// Dressed as a trader, the Silver tier's set lands (20160, 20161).
	char.MissionInventory = []domain.InventoryRow{{Slot: enterworld.JobSuitSlot,
		TypeFlags: wire.PackTypeFlags(3, 1, 7, domain.JobTrader), StackCount: 1}}
	rt.SyncTrainingCampHonorBuffs(testDivision, char.ID, 0)
	if got := have(); !got[HonorBuffCopperSkillID] || !got[HonorBuffSilverSkillID] || got[HonorBuffGoldSkillID] || got[HonorBuffCrownSkillID] {
		t.Fatalf("dressed Silver holder carries %v, want copper+silver", got)
	}

	// A stronger camp rank (King) wins over the Silver tier.
	rt.SyncTrainingCampHonorBuffs(testDivision, char.ID, 1)
	if got := have(); !got[HonorBuffCrownSkillID] {
		t.Fatalf("camp King over tier Silver carries %v, want the crown set", got)
	}

	// Back to the tier alone, the crown retires again.
	rt.SyncTrainingCampHonorBuffs(testDivision, char.ID, 0)
	if got := have(); got[HonorBuffCrownSkillID] || !got[HonorBuffSilverSkillID] {
		t.Fatalf("tier alone again carries %v, want copper+silver", got)
	}
}
