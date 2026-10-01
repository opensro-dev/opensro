package quest

import (
	"bytes"
	"math"
	"opensro.online/server/internal/testsupport/licensed"
	"strings"
	"testing"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

func TestCreditGoldSaturatesAndHealsInvalidStorage(t *testing.T) {
	ceiling := int64(math.MaxInt64)
	if got := creditGold(&ceiling, 375); got != math.MaxInt64 {
		t.Fatalf("credit at ceiling = %d, want saturation", got)
	}
	negative := int64(-500)
	if got := creditGold(&negative, 375); got != 375 {
		t.Fatalf("credit over negative storage = %d, want healed reward balance", got)
	}
}

func testRuntime(t *testing.T) *Runtime {
	t.Helper()
	rt, err := NewRuntime(&enterworld.Deps{}, loadTestDefinitions(t),
		func(character *enterworld.Character, expDelta, skillExpDelta int64, sourceGid uint32) ([]wire.Frame, bool) {
			return []wire.Frame{{Opcode: wire.OpExpUpdate, Payload: []byte{byte(expDelta)}}}, true
		})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	rt.PlanInventory = action.NewRuntime(&enterworld.Deps{Items: fakeItems{}}, nil).PlanQuestInventory
	return rt
}

func questCharacter() *enterworld.Character {
	level := int64(20) // eligible for the level-20 resuscitation collection fixture
	return &enterworld.Character{ID: 3, Name: "asd2", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: &level}
}

// potionInventory answers count objective items in one stack row.
func potionInventory(count int64) []enterworld.InventoryRow {
	return []enterworld.InventoryRow{{Slot: 20, RefObjID: 3674, Codename: "ITEM_QSP_ALL_POTION_1_02", StackCount: count}}
}

func TestNewRuntimeRefusesExpRewardWithoutGranter(t *testing.T) {
	licensed.RequireGameData(t)
	t.Parallel()
	_, err := NewRuntime(&enterworld.Deps{}, loadTestDefinitions(t), nil)
	if err == nil || !strings.Contains(err.Error(), "QNO_EU_TUTORIAL_1") {
		t.Fatalf("the chef definition pays exp; a nil granter must refuse naming it, got %v", err)
	}
}

func TestStartQuestEmitsInsertAndPersists(t *testing.T) {
	licensed.RequireGameData(t)
	t.Parallel()
	rt := testRuntime(t)
	character := questCharacter()

	result, err := rt.StartQuest(character, "QTUTORIAL_CH")
	if err != nil {
		t.Fatalf("StartQuest: %v", err)
	}
	if len(character.ActiveQuests) != 1 || character.ActiveQuests[0].RefID != 2 {
		t.Fatalf("active list = %+v, want the tutorial record", character.ActiveQuests)
	}
	record := character.ActiveQuests[0]
	if record.Flags != 0x18 || record.U10 != 1 {
		t.Fatalf("record = %+v, want flags 0x18 / kind 1", record)
	}
	if len(record.Contents) != 1 || !record.Contents[0].ObjectiveSentinel {
		t.Fatalf("a talk objective must carry the 0xFF sentinel node, got %+v", record.Contents)
	}
	if len(result.Frames) != 1 || result.Frames[0].Opcode != OpQuestUpdate {
		t.Fatalf("frames = %+v, want one 0x31ED", result.Frames)
	}
	if want := EncodeQuestUpdateInsert(record); !bytes.Equal(result.Frames[0].Payload, want) {
		t.Fatalf("op-1 payload = % X, want % X", result.Frames[0].Payload, want)
	}

	if _, err := rt.StartQuest(character, "QTUTORIAL_CH"); err == nil {
		t.Fatal("an already-active quest must refuse")
	}
	character.ActiveQuests = nil
	character.CompletedQuestIds = []uint32{2}
	if _, err := rt.StartQuest(character, "QTUTORIAL_CH"); err == nil {
		t.Fatal("a completed quest must refuse")
	}
	if _, err := rt.StartQuest(character, "QNO_NOT_CURATED"); err == nil {
		t.Fatal("an unknown codename must refuse")
	}
}

func TestStartCollectQuestCountsHeldItems(t *testing.T) {
	licensed.RequireGameData(t)
	t.Parallel()
	rt := testRuntime(t)
	character := questCharacter()
	character.MissionInventory = potionInventory(4)

	if _, err := rt.StartQuest(character, "QSP_ALL_POTION_1"); err != nil {
		t.Fatalf("StartQuest: %v", err)
	}
	record := character.ActiveQuests[0]
	if got := record.Contents[0].ObjectiveValues; len(got) != 1 || got[0] != 4 {
		t.Fatalf("progress = %v, want the held count 4", got)
	}
	if record.Contents[0].Kind != 1 {
		t.Fatal("an unfinished objective paints ING (kind 1)")
	}
}

func TestHandleGiveUpAbandons(t *testing.T) {
	licensed.RequireGameData(t)
	t.Parallel()
	rt := testRuntime(t)
	character := questCharacter()
	if _, err := rt.StartQuest(character, "QTUTORIAL_CH"); err != nil {
		t.Fatal(err)
	}

	result, err := rt.HandleGiveUp(character, u32le(2))
	if err != nil {
		t.Fatalf("HandleGiveUp: %v", err)
	}
	if len(character.ActiveQuests) != 0 {
		t.Fatal("give-up must remove the active record")
	}
	if len(character.CompletedQuestIds) != 0 {
		t.Fatal("give-up must NOT append the completed list (the op-3/op-4 declared asymmetry)")
	}
	if want := EncodeQuestUpdateAbandon(2); len(result.Frames) != 1 || !bytes.Equal(result.Frames[0].Payload, want) {
		t.Fatalf("frames = %+v, want one op-4 abandon", result.Frames)
	}
}

func TestHandleGiveUpRefusals(t *testing.T) {
	licensed.RequireGameData(t)
	t.Parallel()
	rt := testRuntime(t)
	character := questCharacter()

	if _, err := rt.HandleGiveUp(character, []byte{1, 2}); err == nil {
		t.Fatal("a malformed body must refuse")
	}
	if _, err := rt.HandleGiveUp(character, u32le(0x7fffffff)); err == nil {
		t.Fatal("an unknown id must refuse (typed, loud)")
	}
	if _, err := rt.HandleGiveUp(character, u32le(2)); err == nil {
		t.Fatal("a known but inactive quest must refuse")
	}
	// The kind-2 window composes 0x729A, never 0x71EB.
	character.MissionInventory = potionInventory(10)
	if _, err := rt.StartQuest(character, "QSP_ALL_POTION_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.HandleGiveUp(character, u32le(29)); err == nil {
		t.Fatal("a kind-2 quest give-up is a crafted frame and must refuse")
	}
}

func TestHandleRewardSelectTurnsIn(t *testing.T) {
	licensed.RequireGameData(t)
	t.Parallel()
	rt := testRuntime(t)
	character := questCharacter()
	character.MissionInventory = potionInventory(10)
	if _, err := rt.StartQuest(character, "QSP_ALL_POTION_1"); err != nil {
		t.Fatal(err)
	}

	result, err := rt.HandleRewardSelect(character, u32le(29))
	if err != nil {
		t.Fatalf("HandleRewardSelect: %v", err)
	}
	if len(character.ActiveQuests) != 0 {
		t.Fatal("turn-in must remove the active record")
	}
	if len(character.CompletedQuestIds) != 1 || character.CompletedQuestIds[0] != 29 {
		t.Fatalf("completed = %v, want [29]", character.CompletedQuestIds)
	}
	// Inventory receipts, quest removal, then the native success cue.
	if len(result.Frames) != 5 || !bytes.Equal(result.Frames[3].Payload, EncodeQuestUpdateComplete(29)) || result.Frames[4].Opcode != 0xb29a || !bytes.Equal(result.Frames[4].Payload, []byte{1, 29, 0, 0, 0}) {
		t.Fatalf("frames = %+v, want inventory, op-3 complete and B29A success", result.Frames)
	}
}

func TestHandleRewardSelectPaysEvidencedRewards(t *testing.T) {
	licensed.RequireGameData(t)
	t.Parallel()
	// The chef quest pays 475 exp / 375 gold (the shard join). Its
	// objective is TALK, which this server cannot complete yet - the
	// payout machinery is exercised by driving the record to a MET
	// collect state through a definitions tweak-free route: reuse the
	// potion quest's collect completion but attach the chef rewards via
	// a definitions copy is NOT possible without inventing - so this
	// test pins the two payout arms directly instead: gold through the
	// record door, exp through the granter.
	rt := testRuntime(t)
	character := questCharacter()
	character.MissionInventory = potionInventory(10)
	baseGold := int64(5000)
	character.Gold = &baseGold
	if _, err := rt.StartQuest(character, "QSP_ALL_POTION_1"); err != nil {
		t.Fatal(err)
	}
	// Point the potion definition at the chef rewards for THIS runtime
	// instance only: the payout arms are definition-driven and the
	// curated values are pinned by TestLoadDefinitionsResolvesTheCuratedTable.
	def, _ := rt.Defs.ByRefID(29)
	def.RewardExp = 475
	def.RewardGold = 375

	result, err := rt.HandleRewardSelect(character, u32le(29))
	if err != nil {
		t.Fatalf("HandleRewardSelect: %v", err)
	}
	if character.Gold == nil || *character.Gold != 5375 {
		t.Fatalf("gold = %v, want 5000+375 through the record door", character.Gold)
	}
	if len(result.Frames) != 7 {
		t.Fatalf("frames = %+v, want op-3 + gold refresh + exp burst + success cue", result.Frames)
	}
	if result.Frames[4].Opcode != wire.OpPointsUpdate {
		t.Fatalf("frame 1 opcode = 0x%04X, want the 0x3126 gold refresh", result.Frames[1].Opcode)
	}
	refresh, err := wire.DecodeGoldRefresh(result.Frames[4].Payload)
	if err != nil || refresh.Balance != 5375 {
		t.Fatalf("gold refresh = %+v (%v), want 5375", refresh, err)
	}
	if result.Frames[5].Opcode != wire.OpExpUpdate {
		t.Fatalf("frame 2 opcode = 0x%04X, want the granter's exp burst", result.Frames[2].Opcode)
	}
}

func TestRewardProgressionBroadcastExposesOnlyLevelPresentation(t *testing.T) {
	licensed.RequireGameData(t)
	t.Parallel()
	rt := testRuntime(t)
	character := questCharacter()
	character.MissionInventory = potionInventory(10)
	if _, err := rt.StartQuest(character, "QSP_ALL_POTION_1"); err != nil {
		t.Fatal(err)
	}
	def, _ := rt.Defs.ByRefID(29)
	def.RewardExp = 475
	rt.ApplyExperience = func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) {
		return []wire.Frame{
			{Opcode: wire.OpLevelUpEffect, Payload: wire.EncodeLevelUpEffect(100001)},
			{Opcode: wire.OpBaseStats, Payload: []byte{1}},
			{Opcode: wire.OpExpUpdate, Payload: []byte{2}},
		}, true
	}

	result, err := rt.HandleRewardSelect(character, u32le(29))
	if err != nil {
		t.Fatalf("HandleRewardSelect: %v", err)
	}
	if len(result.Broadcast) != 1 || result.Broadcast[0].Opcode != wire.OpLevelUpEffect {
		t.Fatalf("reward broadcast = %+v, want only 0x36B0", result.Broadcast)
	}
}

func TestHandleRewardSelectRefusals(t *testing.T) {
	licensed.RequireGameData(t)
	t.Parallel()
	rt := testRuntime(t)
	character := questCharacter()

	if _, err := rt.HandleRewardSelect(character, u32le(0x7fffffff)); err == nil {
		t.Fatal("an unknown id must refuse")
	}
	if _, err := rt.HandleRewardSelect(character, u32le(2)); err == nil {
		t.Fatal("a kind-1 quest cannot compose 0x729A - refuse")
	}
	character.MissionInventory = potionInventory(3)
	if _, err := rt.StartQuest(character, "QSP_ALL_POTION_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.HandleRewardSelect(character, u32le(29)); err == nil {
		t.Fatal("an incomplete objective (3/10) must refuse the turn-in")
	}
	if len(character.CompletedQuestIds) != 0 || len(character.ActiveQuests) != 1 {
		t.Fatal("a refusal must mutate nothing")
	}
}

func TestNotifyInventoryChangedEmitsProgress(t *testing.T) {
	licensed.RequireGameData(t)
	t.Parallel()
	rt := testRuntime(t)
	character := questCharacter()
	if _, err := rt.StartQuest(character, "QSP_ALL_POTION_1"); err != nil {
		t.Fatal(err)
	}

	// Pick up 7 objective items: one op-2 with progress 7, kind ING.
	character.MissionInventory = potionInventory(7)
	frames := rt.NotifyInventoryChanged(character)
	if len(frames) != 1 || frames[0].Opcode != OpQuestUpdate {
		t.Fatalf("frames = %+v, want one 0x31ED op-2", frames)
	}
	record := character.ActiveQuests[0]
	if got := record.Contents[0].ObjectiveValues[0]; got != 7 {
		t.Fatalf("persisted progress = %d, want 7", got)
	}
	if want := EncodeQuestUpdateUpdate(record); !bytes.Equal(frames[0].Payload, want) {
		t.Fatalf("op-2 payload = % X, want % X", frames[0].Payload, want)
	}

	// No change: no frame (the client's op-2 repaint is not free).
	if frames := rt.NotifyInventoryChanged(character); len(frames) != 0 {
		t.Fatalf("an unchanged count must emit nothing, got %+v", frames)
	}

	// Reaching the required count clamps and flips the node to END.
	character.MissionInventory = potionInventory(25)
	frames = rt.NotifyInventoryChanged(character)
	if len(frames) != 1 {
		t.Fatalf("frames = %+v, want the completion op-2", frames)
	}
	record = character.ActiveQuests[0]
	if record.Contents[0].ObjectiveValues[0] != 10 || record.Contents[0].Kind != 0 {
		t.Fatalf("record = %+v, want progress clamped at 10 and the END kind 0", record.Contents[0])
	}

	// Dropping items moves progress back DOWN (recompute-from-inventory).
	character.MissionInventory = potionInventory(2)
	frames = rt.NotifyInventoryChanged(character)
	if len(frames) != 1 || character.ActiveQuests[0].Contents[0].ObjectiveValues[0] != 2 {
		t.Fatalf("a drop must recompute down, got %+v", character.ActiveQuests)
	}

	// A quest this server's definitions do not know stays untouched.
	character.ActiveQuests = append(character.ActiveQuests, enterworld.ActiveQuestRecord{RefID: 0x7777, Flags: 0x40, TargetIds: []uint32{1}})
	before := character.ActiveQuests[1]
	rt.NotifyInventoryChanged(character)
	if got := character.ActiveQuests[1]; got.RefID != before.RefID || got.Flags != before.Flags {
		t.Fatal("a foreign/seeded record is not this lane's to move")
	}
}

func TestDefaultQuestSeeder(t *testing.T) {
	licensed.RequireGameData(t)
	t.Parallel()
	defs := loadTestDefinitions(t)
	seeder := DefaultQuestSeeder(defs)

	chinese, err := seeder(enterworld.RaceKeyChina)
	if err != nil {
		t.Fatalf("CH seed: %v", err)
	}
	if len(chinese) != 1 || chinese[0].RefID != 2 || chinese[0].Flags != 0x18 {
		t.Fatalf("CH seed = %+v, want the QTUTORIAL_CH record", chinese)
	}

	european, err := seeder(enterworld.RaceKeyEurope)
	if err != nil || len(european) != 0 {
		t.Fatalf("EU seed = %+v (%v), want empty (no race-1 row survives the codename join)", european, err)
	}

	// A definition-less set with a non-empty racial list refuses loud.
	empty, err := LoadDefinitions(NewCatalog(t.TempDir()), fakeItems{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DefaultQuestSeeder(empty)(enterworld.RaceKeyChina); err == nil {
		t.Fatal("an unresolvable seed codename must refuse, never seed short")
	}
}
