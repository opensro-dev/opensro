package quest

import (
	"fmt"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
)

// QuestStage is an ordered, persisted objective. Completing it pays its own
// reward and advances atomically; only the last stage completes the quest.
// Go's authority transaction owns this lifecycle, not the NPC UI session.
type QuestStage struct {
	QuestSpec
	ContentsSymbol string
	EquippedItem   string
	collectRef     uint32
	endNpcRef      uint32
	deliveryNpcRef uint32
}

func stageToken(code string, stage uint16) string { return fmt.Sprintf("%s@%d", code, stage) }

func parseStageToken(token string) (string, uint16, bool) {
	code, suffix, found := strings.Cut(token, "@")
	if !found {
		return code, 0, false
	}
	n, err := strconv.ParseUint(suffix, 10, 16)
	if err != nil {
		return token, 0, false
	}
	return code, uint16(n), true
}

func definitionAtStage(root *Definition, stage uint16) (*Definition, bool) {
	if len(root.Stages) == 0 {
		return root, stage == root.stageIndex
	}
	if int(stage) >= len(root.Stages) {
		return nil, false
	}
	s := root.Stages[stage]
	d := *root
	d.QuestSpec = s.QuestSpec
	d.Codename, d.KindByte = root.Codename, root.KindByte
	d.ContentsSymbol, d.CollectItemRefID, d.stageIndex = s.ContentsSymbol, s.collectRef, stage
	d.endNpcRef, d.deliveryNpcRef = s.endNpcRef, s.deliveryNpcRef
	// Keep the equip predicate with the resolved stage, without exposing the
	// remaining stage graph to BuildActiveQuestRecord's initial-state branch.
	d.requiredEquippedItem = s.EquippedItem
	return &d, true
}

func stageObjectiveMet(c *enterworld.Character, def *Definition, record enterworld.ActiveQuestRecord) bool {
	if !objectiveMet(c, def, record) {
		return false
	}
	if def.requiredEquippedItem == "" {
		return true
	}
	for _, row := range c.MissionInventory {
		if row.Slot >= 0 && row.Slot < int64(inventory.EquipmentSlotEnd) && row.Codename == def.requiredEquippedItem {
			return true
		}
	}
	return false
}

// Tutorial equipment has gender-specific media rows. The item planner still
// resolves and validates the actual row before any reward or stage commits.
func rewardItemForCharacter(c *enterworld.Character, code string) string {
	if strings.HasSuffix(enterworld.RaceGenderKey(c, c.ModelCodename), "_W") {
		return strings.Replace(code, "_M_", "_W_", 1)
	}
	return code
}

func loadStages(def *Definition, items enterworld.ItemRefSource) error {
	if len(def.Stages) > 255 {
		return fmt.Errorf("quest %s too many stages", def.Codename)
	}
	// Do not mutate global specification slices while resolving media IDs.
	def.Stages = append([]QuestStage(nil), def.Stages...)
	for i := range def.Stages {
		s := &def.Stages[i]
		if s.ContentsSymbol == "" || s.EndNpcCodename == "" || s.CompletePromptSymbol == "" || len(s.Stages) > 0 || len(s.Objectives) > 0 {
			return fmt.Errorf("quest %s stage %d incomplete contract", def.Codename, i)
		}
		switch s.Objective {
		case ObjectiveTalk:
		case ObjectiveKill:
			if s.KillCount == 0 || len(s.KillMonsterCodenames) == 0 {
				return fmt.Errorf("quest %s stage %d missing kill objective", def.Codename, i)
			}
		case ObjectiveCollect:
			if items == nil || s.CollectCount == 0 {
				return fmt.Errorf("quest %s stage %d missing inventory contract", def.Codename, i)
			}
			ref, ok := items.ItemRefByCodename(s.CollectItemCodename)
			if !ok || ref == nil {
				return fmt.Errorf("quest %s stage %d unknown item", def.Codename, i)
			}
			s.collectRef = ref.RefObjID
		default:
			return fmt.Errorf("quest %s stage %d unsupported objective", def.Codename, i)
		}
		if s.RewardExp < 0 || s.RewardGold < 0 || s.RewardSkillExp < 0 {
			return fmt.Errorf("negative stage reward")
		}
		for _, reward := range s.RewardItems {
			if items == nil || reward.Count == 0 || reward.Count > 65535 {
				return fmt.Errorf("invalid stage reward or missing ItemRefSource")
			}
			for _, code := range []string{reward.ItemCodename, strings.Replace(reward.ItemCodename, "_M_", "_W_", 1)} {
				if ref, ok := items.ItemRefByCodename(code); !ok || ref == nil {
					return fmt.Errorf("quest %s unresolved stage reward %s", def.Codename, code)
				}
			}
		}
	}
	return nil
}
