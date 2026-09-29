/*
===========================================================================

resuscitation_test.go - atomic potion spending and immediate NPC services

Use the production inventory hooks so a gold refusal cannot consume a stack,
and a delayed dialog choice cannot reopen an already consumed service request.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
)

/*
================
TestResuscitationCommitsGoldPotionsRanksAndRefundTogether
================
*/
func TestResuscitationCommitsGoldPotionsRanksAndRefundTogether(t *testing.T) {
	c := testCharacter()
	c.Skills = []uint32{103}
	items := testItems()
	potion := &enterworld.ItemRef{RefObjID: 3673, Codename: "ITEM_QSP_ALL_POTION_1_01", TypeIDs: [4]int64{3, 3, 9, 0}}
	items[potion.Codename] = potion
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: potion.RefObjID, Codename: potion.Codename, StackCount: 2, TypeFlags: potion.TypeFlags()}}
	action, _ := newTestRuntime(c, items)
	deps := action.deps.(*enterworld.Deps)
	deps.Skills = withdrawalSkills{staticSkillSource{
		101: {ID: 101, Group: 77, Level: 1, SPCost: 2},
		102: {ID: 102, Group: 77, Level: 2, SPCost: 7, Masteries: [2]enterworld.SkillRequirement{{ID: 257, Level: 1}}},
		103: {ID: 103, Group: 77, Level: 3, SPCost: 14, Masteries: [2]enterworld.SkillRequirement{{ID: 257, Level: 2}}},
	}}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dg.txt"), []byte("1\t28\t42\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps.Levels = enterworld.NewTextdataLevels(dir)
	runtime := progression.NewRuntime(deps)
	runtime.Withdrawal = action.WithdrawalHooks()
	request := wire.NewWriter(9).U32(3673).U32(103).U8(1).Payload()
	before, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	result := runtime.HandleSkillWithdrawal(testDivision, c, request)
	after, err := json.Marshal(c)
	if err != nil || !bytes.Equal(before, after) || len(result.Frames) != 1 || !bytes.Equal(result.Frames[0].Payload, []byte{2, 4}) {
		t.Fatal("gold refusal mutated character", result, err)
	}
	*c.Gold = 10000
	result = runtime.HandleSkillWithdrawal(testDivision, c, request)
	if len(result.Frames) < 2 || *c.Gold != 3560 || c.SkillPoints == nil || *c.SkillPoints != 17 || len(c.MissionInventory) != 0 || c.Skills[0] != 101 {
		t.Fatal("resuscitation transaction", result, c)
	}
}

/*
================
TestNpcImmediateServiceConsumesTheDialogChoice
================
*/
func TestNpcImmediateServiceConsumesTheDialogChoice(t *testing.T) {
	c := testCharacter()
	rt := selectTestRuntime(c)
	rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true}
	npc := rt.NpcRoster[0]
	rt.Selected.Set(testDivision, c.Name, npc.ObjectID)
	rt.NpcDialogs.Put(testDivision, c.Name, npcDialogSession{
		NpcGID: npc.ObjectID, NpcCode: npc.Codename, Stage: npcDialogOptions,
		Options: []NpcQuestOption{{Codename: "withdraw", Immediate: true}},
	})
	calls := 0
	rt.NpcQuests.Finish = func(_ *enterworld.Character, code, npcCode string) ([]wire.Frame, error) {
		calls++
		if code != "withdraw" || npcCode != npc.Codename {
			t.Fatal(code, npcCode)
		}
		return []wire.Frame{{Opcode: 0x3230}}, nil
	}
	frames, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{5})
	if refusal != "" || len(frames) != 1 || frames[0].Opcode != 0x3230 || calls != 1 {
		t.Fatal(frames, refusal, calls)
	}
	if _, refusal = rt.HandleNpcDialogResponse(testDivision, c, []byte{5}); refusal == "" || calls != 1 {
		t.Fatal("replayed immediate choice", refusal, calls)
	}
}
