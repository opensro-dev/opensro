/*
===========================================================================

tiger_chain_test.go - the authored chain heads load and gate their chains

QNO_CH_GENARAL_1 (57) and QNO_CH_SPECIAL_2 (61) are the prerequisites the
already-shipped GENARAL_2/GENARAL_3 and SPECIAL_3 rows demand; before the
curated specs existed no character could ever be offered those quests.

===========================================================================
*/
package quest

import (
	"testing"
)

/*
================
TestTigerChainHeadsLoadAndGate
================
*/
func TestTigerChainHeadsLoadAndGate(t *testing.T) {
	defs, _ := loadShippedDefinitions(t)

	general1, ok := defs.ByCodename("QNO_CH_GENARAL_1")
	if !ok || general1.RefID != 57 || general1.Objective != ObjectiveParallel || len(general1.Objectives) != 3 {
		t.Fatalf("GENARAL_1 = %+v, want ref 57 with three parallel missions", general1)
	}
	counts := []uint32{1, 20, 20}
	for i, want := range counts {
		if general1.Objectives[i].KillCount != want {
			t.Fatalf("GENARAL_1 mission %d count = %d, want %d (v1.150 SN_CON text)", i, general1.Objectives[i].KillCount, want)
		}
	}
	if general1.RewardExp != 6700 || general1.RewardSkillExp != 2050 || general1.RewardGold != 0 || len(general1.RewardItems) != 2 {
		t.Fatalf("GENARAL_1 rewards = exp %d sp %d gold %d items %v", general1.RewardExp, general1.RewardSkillExp, general1.RewardGold, general1.RewardItems)
	}
	general2, ok := defs.ByCodename("QNO_CH_GENARAL_2")
	if !ok || len(general2.RequiredQuestIDs) != 1 || general2.RequiredQuestIDs[0] != 57 {
		t.Fatalf("GENARAL_2 prerequisites = %v, want [57]", general2.RequiredQuestIDs)
	}

	special2, ok := defs.ByCodename("QNO_CH_SPECIAL_2")
	if !ok || special2.RefID != 61 || special2.CollectCount != 100 || special2.MaxCompletions != 2 {
		t.Fatalf("SPECIAL_2 = ref %d collect %d max %d, want 61 / 100 / 2", special2.RefID, special2.CollectCount, special2.MaxCompletions)
	}
	if len(special2.RequiredQuestIDs) != 1 || special2.RequiredQuestIDs[0] != 48 {
		t.Fatalf("SPECIAL_2 prerequisites = %v, want [48]", special2.RequiredQuestIDs)
	}

	c := questCharacter()
	level := int64(30)
	c.Level = &level
	if !offerAvailable(c, general1) {
		t.Fatal("GENARAL_1 not offered to a fresh level-30 character")
	}
	if offerAvailable(c, general2) {
		t.Fatal("GENARAL_2 offered before GENARAL_1 completion")
	}
	if offerAvailable(c, special2) {
		t.Fatal("SPECIAL_2 offered before SPECIAL_1 completion")
	}
	c.CompletedQuestIds = append(c.CompletedQuestIds, 57, 48)
	c.QuestCompletionCounts = map[uint32]uint32{57: 1, 48: 1}
	if !offerAvailable(c, general2) {
		t.Fatal("GENARAL_2 not offered after GENARAL_1 completion")
	}
	if !offerAvailable(c, special2) {
		t.Fatal("SPECIAL_2 not offered after SPECIAL_1 completion")
	}
	// SPECIAL_2 pays out twice (achievement limit 2), never a third time.
	c.CompletedQuestIds = append(c.CompletedQuestIds, 61)
	c.QuestCompletionCounts[61] = 2
	if offerAvailable(c, special2) {
		t.Fatal("SPECIAL_2 offered after its two completions")
	}
}
