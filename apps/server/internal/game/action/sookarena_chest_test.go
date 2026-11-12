/*
===========================================================================
sookarena_chest_test.go - atomic claims, stale requests and recovery policies.
===========================================================================
*/
package action

import (
	"encoding/json"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"reflect"
	"strings"
	"testing"
	"time"
)

/*
================
chestTestReward
================
*/
func chestTestReward() domain.InventoryRow {
	return domain.InventoryRow{Slot: 0, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE", TypeFlags: testItems()["ITEM_CH_SWORD_01_A_RARE"].TypeFlags(), StackCount: 1, Durability: 100, VarianceBits: "0"}
}

/*
================
TestSookChestCashoutAndDeath
================
*/
func TestSookChestCashoutAndDeath(t *testing.T) {
	for _, dead := range []bool{false, true} {
		c := instanceWorldTestCharacter(11, "SettlementChest")
		rt, clock := sookFixture(t, c)
		rt.enterSookArena(testDivision, c)
		run := rt.sookRun(testDivision, c)
		run.program.Started = clock.Now().UnixMilli() - 300000
		run.program.StageStarted = run.program.Started
		run.program.Kills = 8
		rt.advanceSookArena(clock.Now().UnixMilli())
		c.SookChest.Items = []domain.InventoryRow{chestTestReward()}
		beforeBag := len(c.MissionInventory)
		beforeGold := goldOf(c)
		id := c.SookChest.ID
		if dead {
			hp := int64(0)
			c.CurrentHP = &hp
			clock.now = clock.now.Add(time.Second)
			rt.advanceSookArena(clock.Now().UnixMilli())
			clock.now = clock.now.Add(5 * time.Second)
			rt.advanceSookArena(clock.Now().UnixMilli())
		} else {
			rt.HandleArenaDecision(testDivision, c, append([]byte{1}, []byte(id)...))
		}
		if c.SookChest != nil || domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance || goldOf(c) != beforeGold+10000 {
			t.Fatal("end did not settle and return")
		}
		want := beforeBag
		if !dead {
			want++
		}
		if len(c.MissionInventory) != want {
			t.Fatal("death/claim inventory policy")
		}
		rt.HandleArenaDecision(testDivision, c, append([]byte{1}, []byte(id)...))
		if goldOf(c) != beforeGold+10000 || len(c.MissionInventory) != want {
			t.Fatal("duplicate decision paid twice")
		}
	}
}

/*
================
TestSookCheckpointReconnectDoesNotSpendEntry
================
*/
func TestSookCheckpointReconnectDoesNotSpendEntry(t *testing.T) {
	c := instanceWorldTestCharacter(11, "ResumeChest")
	rt, clock := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	run.program.Started = clock.Now().UnixMilli() - 300000
	run.program.StageStarted = run.program.Started
	run.program.Kills = 1
	rt.advanceSookArena(clock.Now().UnixMilli())
	id := c.SookChest.ID
	entries := c.SookArenaEntries
	rt.forgetSookArena(testDivision, c.Name)
	if c.SookChest == nil || !c.SookChest.Checkpoint {
		t.Fatal("safe checkpoint lost")
	}
	next, _ := sookFixture(t, c)
	next.enterSookArena(testDivision, c)
	resumed := next.sookRun(testDivision, c)
	if resumed == nil || resumed.program.PausedAt == 0 || c.SookChest.ID != id || c.SookArenaEntries != entries || resumed.lease == (instance.Lease{}) {
		t.Fatal("checkpoint resume changed entry or loot identity")
	}
}

/*
================
TestSookChestAtomicClaim
================
*/
func TestSookChestAtomicClaim(t *testing.T) {
	c := instanceWorldTestCharacter(11, "Chest")
	rt, _ := sookFixture(t, c)
	c.InventorySize = 14
	c.MissionInventory = []domain.InventoryRow{chestTestReward()}
	c.MissionInventory[0].Slot = 13
	c.SookChest = &domain.ArenaChest{ID: "one", Pending: true, Items: []domain.InventoryRow{chestTestReward(), chestTestReward()}}
	c.SookChest.Items[1].Slot = 1
	before := domain.CloneCharacter(c)
	if _, fault := rt.claimSookChest(c); fault == "" {
		t.Fatal("full bag claimed")
	}
	if !reflect.DeepEqual(c.MissionInventory, before.MissionInventory) || !reflect.DeepEqual(c.SookChest, before.SookChest) {
		t.Fatal("failed claim changed bag or chest")
	}
	c.InventorySize = 16
	frames, fault := rt.claimSookChest(c)
	if fault != "" || len(frames) == 0 || c.SookChest != nil || len(c.MissionInventory) != 3 {
		t.Fatalf("claim failed %s", fault)
	}
	if _, fault := rt.claimSookChest(c); fault == "" || len(c.MissionInventory) != 3 {
		t.Fatal("claim paid twice")
	}
}

/*
================
TestSookChestDecisionsAndRestart
================
*/
func TestSookChestDecisionsAndRestart(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		c := instanceWorldTestCharacter(11, "RecoverChest")
		rt, _ := sookFixture(t, c)
		rt.enterSookArena(testDivision, c)
		c.SookChest.Items = []domain.InventoryRow{chestTestReward()}
		c.SookChest.Stage = 1
		if shutdown {
			rt.PrepareSookShutdown()
		}
		rt.forgetSookArena(testDivision, c.Name)
		if shutdown && (c.SookChest == nil || !c.SookChest.Pending) {
			t.Fatal("server drain lost earned rewards")
		}
		if !shutdown && c.SookChest != nil {
			t.Fatal("combat disconnect avoided the risk")
		}
	}
	c := instanceWorldTestCharacter(12, "Decision")
	rt, clock := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	run.program.Started = clock.Now().UnixMilli() - 300000
	run.program.StageStarted = run.program.Started
	run.program.Kills = 1
	rt.advanceSookArena(clock.Now().UnixMilli())
	if run.program.PausedAt == 0 || c.SookChest.Stage != 1 {
		t.Fatal("checkpoint was not saved")
	}
	id := c.SookChest.ID
	rt.HandleArenaDecision(testDivision, c, append([]byte{2}, []byte("stale")...))
	if run.program.ResumeAt != 0 {
		t.Fatal("stale request continued")
	}
	rt.HandleArenaDecision(testDivision, c, append([]byte{2}, []byte(id)...))
	if run.program.ResumeAt != clock.Now().UnixMilli()+15000 {
		t.Fatal("continue did not count down")
	}
	clock.now = clock.now.Add(15 * time.Second)
	rt.advanceSookArena(clock.Now().UnixMilli())
	if run.program.PausedAt != 0 || c.SookChest.Checkpoint {
		t.Fatal("resume failed")
	}
}

/*
================
TestSookCheckpointFreezesReuseAndAbnormals
================
*/
func TestSookCheckpointFreezesReuseAndAbnormals(t *testing.T) {
	c := instanceWorldTestCharacter(11, "PausedChest")
	rt, clock := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	now := clock.Now().UnixMilli()
	run.program.Started = now - 300000
	run.program.StageStarted = run.program.Started
	run.program.Kills = 1
	c.OffensiveSkillCooldowns = map[uint32]int64{7: now + 15000}
	block := &abnormal.Block{}
	block.Slots[abnormal.Poison] = abnormal.Slot{Active: true, StartedAt: now - 1000, LastTickAt: now - 500}
	rt.storePlayerAbnormal(testDivision, c.Name, block)
	rt.advanceSookArena(now)
	if run.program.PausedAt == 0 {
		t.Fatal("negative state blocked safe checkpoint")
	}
	if out := rt.monsterStrikeCOS(monsterStrikeInput{division: testDivision}, c, nil, nil, simulation.Spawn{}); !out.alive {
		t.Fatal("paused companion impact admitted")
	}
	if frames := rt.advancePlayerAbnormal(testDivision, c.Name, now+2000); len(frames) != 0 {
		t.Fatal("paused negative state advanced")
	}
	rt.HandleArenaDecision(testDivision, c, append([]byte{2}, []byte(c.SookChest.ID)...))
	clock.now = clock.now.Add(15 * time.Second)
	rt.advanceSookArena(clock.Now().UnixMilli())
	if c.OffensiveSkillCooldowns[7] != now+15000 {
		t.Fatal("countdown should advance restored cooldown")
	}
	shifted := rt.playerAbnormal(testDivision, c.Name)
	if shifted.Slots[abnormal.Poison].StartedAt != now+14000 || block.Slots[abnormal.Poison].StartedAt != now-1000 {
		t.Fatal("abnormal duration or original snapshot changed")
	}
}

/*
================
TestSookChestReferencesPrecedePreview
================
*/
func TestSookChestReferencesPrecedePreview(t *testing.T) {
	c := instanceWorldTestCharacter(11, "MetadataChest")
	rt, _ := sookFixture(t, c)
	c.SookChest = &domain.ArenaChest{ID: "preview", Checkpoint: true, Stage: 1, Items: []domain.InventoryRow{chestTestReward()}}
	frames := rt.arenaChestFrames(c, nil, "")
	if len(frames) != 2 || frames[0].Opcode != opCommerceItemReferences || frames[1].Opcode != opArenaChest {
		t.Fatal("preview arrived without item metadata")
	}
	var refs struct {
		Items []struct {
			ID     uint32                  `json:"refObjId"`
			Fields enterworld.NativeFields `json:"nativeFields"`
		} `json:"items"`
	}
	if err := json.Unmarshal(frames[0].Payload, &refs); err != nil || len(refs.Items) != 1 || refs.Items[0].ID != 11459 {
		t.Fatal("missing chest item reference")
	}
}

/*
================
TestSookCheckpointSurvivesAdmissionCleanupBeforeSocketForget
================
*/
func TestSookCheckpointSurvivesAdmissionCleanupBeforeSocketForget(t *testing.T) {
	c := instanceWorldTestCharacter(11, "LeaseCleanupChest")
	rt, clock := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	run.program.Started = clock.Now().UnixMilli() - 300000
	run.program.StageStarted = run.program.Started
	run.program.Kills = 1
	rt.advanceSookArena(clock.Now().UnixMilli())
	id := c.SookChest.ID
	// Re-entry restores the town pose before the old socket retires.
	rt.recoverSookArena(testDivision, c)
	clock.now = clock.now.Add(100 * time.Millisecond)
	rt.advanceSookArena(clock.Now().UnixMilli())
	if c.SookChest == nil || c.SookChest.ID != id || !c.SookChest.Checkpoint {
		t.Fatal("admission cleanup forfeited checkpoint")
	}
}

/*
================
TestSookCheckpointSurvivesRetiredPopulationMembership
================
*/
func TestSookCheckpointSurvivesRetiredPopulationMembership(t *testing.T) {
	c := instanceWorldTestCharacter(11, "RetiredLeaseChest")
	rt, clock := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	run.program.Started = clock.Now().UnixMilli() - 300000
	run.program.StageStarted = run.program.Started
	run.program.Kills = 1
	rt.advanceSookArena(clock.Now().UnixMilli())
	id := c.SookChest.ID
	rt.characterAdmissions.Delete(simulation.WorldKey(testDivision, c.Name))
	clock.now = clock.now.Add(100 * time.Millisecond)
	rt.advanceSookArena(clock.Now().UnixMilli())
	if c.SookChest == nil || c.SookChest.ID != id || !c.SookChest.Checkpoint {
		t.Fatal("membership retirement forfeited checkpoint")
	}
}

/*
================
TestSookStageRewardBundleBoundsAndNativeReferences
================
*/
func TestSookStageRewardBundleBoundsAndNativeReferences(t *testing.T) {
	c := instanceWorldTestCharacter(11, "MixedRewards")
	rt, _ := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	level := int64(90)
	c.Level = &level
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	rt.deps.(*enterworld.Deps).Items = items
	n := uint32(0)
	rt.DropRoll = func() (uint32, error) { n = (n*1103515245 + 12345) & 0x7fff; return n, nil }
	for stage, b := range [][2]int{{3, 7}, {8, 15}, {18, 28}, {32, 45}, {50, 70}} {
		c.SookChest.Items = make([]domain.InventoryRow, arenaChestLimit)
		for i := range c.SookChest.Items {
			c.SookChest.Items[i].Codename = "PREVIOUS_STAGE"
		}
		if !rt.saveSookCheckpoint(run, c, stage+1) {
			t.Fatal("stage save failed")
		}
		rows := c.SookChest.Items
		if total := len(rows); total < b[0] || total > b[1] {
			t.Fatalf("stage %d items=%d, expected %v", stage+1, len(rows), b)
		}
		for index, row := range rows {
			if row.Slot != int64(index) {
				t.Fatal("noncontiguous reward slots")
			}
			ref, ok := items.ItemRefByID(row.RefObjID)
			if !ok || ref.Codename != row.Codename {
				t.Fatal("unknown reward")
			}
		}
		before, beforeRoll := domain.CloneCharacter(c), n
		if !rt.saveSookCheckpoint(run, c, stage+1) || n != beforeRoll || !reflect.DeepEqual(c.SookChest, before.SookChest) {
			t.Fatal("repeated checkpoint rerolled or changed rewards")
		}
	}
}

/*
================
TestSookEarlyStagesNeverRollSun
================
*/
func TestSookEarlyStagesNeverRollSun(t *testing.T) {
	c := instanceWorldTestCharacter(11, "EarlyRewards")
	rt, _ := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	level := int64(90)
	c.Level = &level
	rt.deps.(*enterworld.Deps).Items = enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	rt.AttackPetExpansion = true
	n := uint32(17)
	rt.DropRoll = func() (uint32, error) { n = n*1664525 + 1013904223; return n, nil }
	for stage := 1; stage <= 2; stage++ {
		for index := 0; index < 300; index++ {
			item, ok := rt.arenaReward(rt.sookRun(testDivision, c), c, stage, index)
			if !ok {
				t.Fatal("missing early reward")
			}
			if stage == 1 && (strings.HasSuffix(item.Codename, "B_RARE") || companion.SealTier(item.RefObjID) == 2) {
				t.Fatalf("stage 1 Moon: %s", item.Codename)
			}
			if strings.HasSuffix(item.Codename, "C_RARE") || companion.SealTier(item.RefObjID) == 3 {
				t.Fatalf("early Sun: stage %d %s", stage, item.Codename)
			}
		}
	}
}

/*
================
TestSookSealRollProbabilities
================
*/
func TestSookSealRollProbabilities(t *testing.T) {
	// Normal, Star, Moon, Sun counts across the native 32768-value sample space.
	expected := [][4]int{{32113, 655, 0, 0}, {31704, 983, 81, 0}, {31278, 1310, 164, 16}, {30852, 1638, 246, 32}, {30393, 1966, 328, 81}}
	for stage, want := range expected {
		var got [4]int
		var value uint32
		rt := Runtime{DropRoll: func() (uint32, error) { return value, nil }}
		for value = 0; value < 32768; value++ {
			got[rt.arenaRewardTier(stage+1)]++
		}
		if got != want {
			t.Fatalf("stage %d seals=%v want=%v", stage+1, got, want)
		}
	}
}

/*
================
TestSookCountdownAllowsRealSelfBuffWithoutResettingReuse
================
*/
func TestSookCountdownAllowsRealSelfBuffWithoutResettingReuse(t *testing.T) {
	c := instanceWorldTestCharacter(11, "Preparation")
	rt, clock := sookFixture(t, c)
	row := shippedOffense(t, "SKILL_CH_LIGHTNING_GYEONGGONG_A_01")
	rt.deps.(*enterworld.Deps).Skills = staticSkillSource{row.ID: row}
	c.Skills = append(c.Skills, row.ID)
	c.CurrentMP = testInt64(1000)
	rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	now := clock.Now().UnixMilli()
	run.program.Started = now - 320000
	run.program.StageStarted = now - 300000
	c.OffensiveSkillCooldowns = map[uint32]int64{7: now + 15000}
	rt.advanceSookArena(now)
	request := wire.SkillAction{ActionId: row.ID}.Encode()
	rt.HandleTargetInteract(testDivision, c, request)
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatal("buff cast during decision")
	}
	clock.now = clock.now.Add(30 * time.Second)
	rt.HandleArenaDecision(testDivision, c, append([]byte{2}, []byte(c.SookChest.ID)...))
	if c.OffensiveSkillCooldowns[7] != now+45000 {
		t.Fatal("decision did not freeze prior reuse")
	}
	rt.HandleTargetInteract(testDivision, c, request)
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatal("self buff refused in countdown")
	}
	deadline := skillCooldownDeadline(c, row)
	clock.now = clock.now.Add(15 * time.Second)
	rt.advanceSookArena(clock.Now().UnixMilli())
	if skillCooldownDeadline(c, row) != deadline {
		t.Fatal("stage start overwrote countdown cast cooldown")
	}
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatal("stage start lost self buff")
	}
	if c.OffensiveSkillCooldowns[7] != now+45000 {
		t.Fatal("countdown refroze prior reuse")
	}
}

/*
================
TestSookRecoveryRewardsAreFiftyGrainsOnly
================
*/
func TestSookRecoveryRewardsAreFiftyGrainsOnly(t *testing.T) {
	c := instanceWorldTestCharacter(11, "GrainRewards")
	rt, _ := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	rt.deps.(*enterworld.Deps).Items = enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	n := uint32(17)
	rt.DropRoll = func() (uint32, error) { n = (n*1103515245 + 12345) & 32767; return n, nil }
	grains := 0
	for i := 0; i < 3000; i++ {
		item, ok := rt.arenaReward(rt.sookRun(testDivision, c), c, 1, 2)
		if !ok {
			t.Fatal("missing reward")
		}
		if strings.Contains(item.Codename, "_POTION_") || strings.Contains(item.Codename, "_CURE_") {
			t.Fatalf("forbidden supply %s", item.Codename)
		}
		if strings.Contains(item.Codename, "_SPOTION_") {
			grains++
			if item.Quantity != 50 {
				t.Fatalf("grain stack %d", item.Quantity)
			}
		}
	}
	if grains == 0 {
		t.Fatal("no grain rewards tested")
	}
}

/*
================
TestSookRewardsNeverPaySilk

No arena reward is a silk voucher (owner, 2026-11-12): every reward a
full stage-5 chest's worth of draws yields is gear or a supply.
================
*/
func TestSookRewardsNeverPaySilk(t *testing.T) {
	c := instanceWorldTestCharacter(11, "NoSilk")
	rt, _ := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	rt.deps.(*enterworld.Deps).Items = enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	n := uint32(17)
	rt.DropRoll = func() (uint32, error) { n = n*1664525 + 1013904223; return n, nil }
	for stage := 1; stage <= 5; stage++ {
		for index := 0; index < 70; index++ {
			item, ok := rt.arenaReward(rt.sookRun(testDivision, c), c, stage, index)
			if ok && strings.Contains(strings.ToUpper(item.Codename), "SILK") {
				t.Fatalf("stage %d draw %d pays silk: %s", stage, index, item.Codename)
			}
		}
	}
}

/*
================
TestSookFlagOffBlocksRewardsWithoutLosingSavedItems
================
*/
func TestSookFlagOffBlocksRewardsWithoutLosingSavedItems(t *testing.T) {
	c := instanceWorldTestCharacter(11, "DisabledRewards")
	rt, _ := sookFixture(t, c)
	rt.sookArena = nil
	c.SookChest = &domain.ArenaChest{ID: "saved", Pending: true, Stage: 1, Items: []domain.InventoryRow{chestTestReward()}}
	before := domain.CloneCharacter(c)
	probe := rt.HandleArenaDecision(testDivision, c, []byte{0})
	var projection arenaProjection
	if len(probe.Frames) != 1 || json.Unmarshal(probe.Frames[0].Payload, &projection) != nil || projection.Phase != "closed" || len(projection.Items) != 0 {
		t.Fatal("disabled arena exposed rewards")
	}
	rt.HandleArenaDecision(testDivision, c, append([]byte{1}, []byte("saved")...))
	if !reflect.DeepEqual(c, before) {
		t.Fatal("flag-off consumed or changed earned items")
	}
}

/*
================
TestSookStageRerollFailureKeepsSavedPreview
================
*/
func TestSookStageRerollFailureKeepsSavedPreview(t *testing.T) {
	c := instanceWorldTestCharacter(11, "FailedStageReward")
	rt, _ := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	rt.deps.(*enterworld.Deps).Items = enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	rt.DropRoll = constantDropRoll(0)
	c.SookChest.Stage, c.SookChest.Items = 1, []domain.InventoryRow{chestTestReward()}
	before := domain.CloneCharacter(c)
	rt.deps.(*enterworld.Deps).UpdateCharacter = func(_ *enterworld.Character, _ string, _ func() bool) bool { return false }
	if rt.saveSookCheckpoint(rt.sookRun(testDivision, c), c, 2) {
		t.Fatal("failed save accepted")
	}
	if !reflect.DeepEqual(c.SookChest, before.SookChest) {
		t.Fatal("failed save lost previous rewards")
	}
}

/*
================
TestSookChestCapacityTunableStopsAtTheStoredLimit

The operator's chest capacity is bounded by the rows a stored chest
holds, so no accepted value can roll more than the store keeps.
================
*/
func TestSookChestCapacityTunableStopsAtTheStoredLimit(t *testing.T) {
	for _, s := range tunables.Registry {
		if s.Key == tunables.SookChestCapacity {
			if s.Max != arenaChestLimit || s.Default > s.Max {
				t.Fatalf("capacity bounds %v..%v default %v, want max %d", s.Min, s.Max, s.Default, arenaChestLimit)
			}
			return
		}
	}
	t.Fatal("the chest capacity is not a registered tunable")
}
