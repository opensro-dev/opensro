package quest

import (
	"errors"
	"opensro.online/server/internal/testsupport/clientdata"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
)

type rewardTestLevels struct{}

func (rewardTestLevels) SkillPointCost(int64) (int64, bool) { return 0, true }
func (rewardTestLevels) ExpRequired(level int64) (int64, bool) {
	if level == 1 {
		return 10_000, true
	}
	return 0, false
}
func (rewardTestLevels) MonsterExpBasis(int64) (int64, bool) { return 1, true }

func rewardTestSkillSeeder(string, []uint32) ([]uint32, error) {
	return []uint32{1}, nil
}

func TestQuestRewardUsesOneCharacterTransaction(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := filepath.Join(t.TempDir(), "authority")
	open := func() *store.Store {
		authority, err := store.Open(dir, store.Options{DefaultSkills: rewardTestSkillSeeder})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		return authority
	}

	authority := open()
	definitions := loadTestDefinitions(t)
	definition, ok := definitions.ByRefID(29)
	if !ok {
		t.Fatal("test definition 29 missing")
	}
	definition.RewardExp = 475
	definition.RewardGold = 375
	definition.RewardItems = []RewardItemLead{{ItemCodename: "ITEM_ETC_HP_POTION_01", Count: 28}}

	level := int64(1)
	experience := int64(0)
	gold := int64(5_000)
	character := &enterworld.Character{
		Name:             "atomicreward",
		ModelCodename:    "CHAR_CH_MAN_ADVENTURER",
		Level:            &level,
		Experience:       &experience,
		Gold:             &gold,
		MissionInventory: potionInventory(10),
		ActiveQuests: []enterworld.ActiveQuestRecord{
			BuildActiveQuestRecord(definition, 10),
		},
	}
	if err := authority.CreateCharacter("global-official", "test-account", character); err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}
	character = authority.Characters().CharactersForDivision("global-official")[0]

	deps := &enterworld.Deps{
		Characters: authority.Characters(),
		Levels:     rewardTestLevels{},
	}
	deps.MutateCharacter = authority.MutateCharacter
	deps.UpdateCharacter = authority.UpdateCharacter
	deps.ReadCharacter = func(divisionID string, read func()) {
		authority.ReadCharacters(divisionID, func([]*enterworld.Character) {
			read()
		})
	}

	stats := progression.NewRuntime(deps)
	runtime, err := NewRuntime(deps, definitions, stats.ExperienceUpdater())
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	runtime.PlanInventory = action.NewRuntime(&enterworld.Deps{Items: fakeItems{}}, nil).PlanQuestInventory
	authority.FailCommits(errors.New("disk unavailable"))
	if _, err := runtime.HandleRewardSelect(character, u32le(29)); err != nil {
		t.Fatalf("HandleRewardSelect: %v", err)
	}
	if health := authority.Health(); health.FailedWrites != 1 {
		t.Fatalf("reward attempted %d commits, want exactly one: %+v", health.FailedWrites, health)
	}
	if len(character.ActiveQuests) != 0 ||
		len(character.CompletedQuestIds) != 1 ||
		character.Gold == nil || *character.Gold != 5_375 ||
		character.Experience == nil || *character.Experience != 475 ||
		len(character.MissionInventory) != 1 || character.MissionInventory[0].RefObjID != 3630 || character.MissionInventory[0].StackCount != 28 {
		t.Fatalf("reward planes tore in memory: %+v", character)
	}

	authority.FailCommits(nil)
	authority.MutateCharacter(character, "heal-reward", nil)
	authority.Close()

	reloadedAuthority := open()
	defer reloadedAuthority.Close()
	reloaded := reloadedAuthority.Characters().CharactersForDivision("global-official")[0]
	if len(reloaded.ActiveQuests) != 0 ||
		len(reloaded.CompletedQuestIds) != 1 ||
		reloaded.Gold == nil || *reloaded.Gold != 5_375 ||
		reloaded.Experience == nil || *reloaded.Experience != 475 ||
		len(reloaded.MissionInventory) != 1 || reloaded.MissionInventory[0].RefObjID != 3630 || reloaded.MissionInventory[0].StackCount != 28 {
		t.Fatalf("reward planes tore after restart: %+v", reloaded)
	}
}
