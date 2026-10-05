package domain

import "testing"

func TestCharacterSnapshotDetachesMutableRecordGraph(t *testing.T) {
	level := int64(10)
	spawnX := 20.0
	returnX := 30.0
	deathX := 40.0
	floor := int64(3)
	instance := uint32(0x20001)
	character := &Character{
		Name:                  "snapshotOwner",
		QuestCompletionCounts: map[uint32]uint32{42: 2},
		Level:                 &level,
		BlockedWhisperers:     []string{"whisper-a"},
		CompletedQuestIds:     []uint32{},
		MissionInventory: []InventoryRow{{
			Slot:         6,
			MagicOptions: []uint64{11},
		}},
		World: &CharacterWorld{
			PackedInstance:     &instance,
			Spawn:              &WorldSpawn{X: &spawnX},
			AuthoredAreaReturn: &WorldSpawn{X: &returnX},
			LastDeathPoint:     &WorldSpawn{X: &deathX},
			DungeonFloorIndex:  &floor,
			MoveSegment:        []byte(`{"x":1}`),
		},
		ActiveQuests: []ActiveQuestRecord{{
			Contents: []ActiveQuestContentsNode{{
				ObjectiveValues: []uint32{7},
			}},
			TargetIds: []uint32{8},
		}},
		TrackedQuests: []TrackedQuestRecord{{
			Tail6: []uint8{1, 2, 3},
		}},
		Friends:    []FriendRecord{{ID: 9, Name: "friend"}},
		QuickSlots: []QuickSlotBinding{{Slot: 4, Kind: 0x49, Payload: 123}},
		PetSkillWindows: []PetSkillWindow{{
			ItemRefObjID: 24001, Codename: "ITEM_MALL_PET_SKILL_COLD", EndUnixMs: 5000,
		}},
	}

	snapshot := character.Snapshot()
	character.QuestCompletionCounts[42] = 3
	if snapshot.QuestCompletionCounts[42] != 2 {
		t.Fatal("snapshot shared quest completion counts")
	}

	*character.Level = 11
	character.BlockedWhisperers[0] = "whisper-b"
	character.MissionInventory[0].MagicOptions[0] = 12
	*character.World.Spawn.X = 21
	*character.World.AuthoredAreaReturn.X = 31
	*character.World.LastDeathPoint.X = 41
	*character.World.DungeonFloorIndex = 4
	*character.World.PackedInstance = 0x30001
	character.World.MoveSegment[5] = '2'
	character.ActiveQuests[0].Contents[0].ObjectiveValues[0] = 10
	character.ActiveQuests[0].TargetIds[0] = 11
	character.TrackedQuests[0].Tail6[0] = 9
	character.Friends[0].Name = "changed"
	character.QuickSlots[0].Payload = 456
	// The tick sweep compacts this slice in place under the division lock
	// while a bootstrap reads its snapshot outside it.
	character.PetSkillWindows[0].EndUnixMs = 0

	if *snapshot.Level != 10 {
		t.Errorf("snapshot level = %d, want 10", *snapshot.Level)
	}
	if snapshot.BlockedWhisperers[0] != "whisper-a" {
		t.Errorf("snapshot blocked whisperer = %q, want whisper-a", snapshot.BlockedWhisperers[0])
	}
	if snapshot.MissionInventory[0].MagicOptions[0] != 11 {
		t.Errorf(
			"snapshot magic option = %d, want 11",
			snapshot.MissionInventory[0].MagicOptions[0],
		)
	}
	if *snapshot.World.Spawn.X != 20 || *snapshot.World.AuthoredAreaReturn.X != 30 || *snapshot.World.LastDeathPoint.X != 40 ||
		*snapshot.World.DungeonFloorIndex != 3 || *snapshot.World.PackedInstance != 0x20001 {
		t.Errorf("snapshot world retained mutable pointers: %+v", snapshot.World)
	}
	if string(snapshot.World.MoveSegment) != `{"x":1}` {
		t.Errorf(
			"snapshot move segment = %s, want original bytes",
			snapshot.World.MoveSegment,
		)
	}
	if snapshot.ActiveQuests[0].Contents[0].ObjectiveValues[0] != 7 ||
		snapshot.ActiveQuests[0].TargetIds[0] != 8 {
		t.Errorf(
			"snapshot quest retained mutable slices: %+v",
			snapshot.ActiveQuests[0],
		)
	}
	if snapshot.TrackedQuests[0].Tail6[0] != 1 {
		t.Errorf(
			"snapshot tracker tail = %v, want original",
			snapshot.TrackedQuests[0].Tail6,
		)
	}
	if snapshot.Friends[0].Name != "friend" {
		t.Errorf("snapshot friend = %+v, want original", snapshot.Friends[0])
	}
	if snapshot.QuickSlots[0].Payload != 123 {
		t.Errorf("snapshot quickslot = %+v, want detached payload 123", snapshot.QuickSlots[0])
	}
	if snapshot.PetSkillWindows[0].EndUnixMs != 5000 {
		t.Errorf("snapshot pet skill window = %+v, want detached deadline 5000", snapshot.PetSkillWindows[0])
	}
	if snapshot.CompletedQuestIds == nil || len(snapshot.CompletedQuestIds) != 0 {
		t.Errorf(
			"snapshot collapsed non-nil empty slice: %#v",
			snapshot.CompletedQuestIds,
		)
	}
}
