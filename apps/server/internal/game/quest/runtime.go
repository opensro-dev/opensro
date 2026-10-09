/*
===========================================================================

runtime.go - authoritative quest acceptance, progress and reward transactions

Inventory, scalar rewards and journal state commit through one character
door. Network frames describe committed state; they never drive progression.

===========================================================================
*/
package quest

import (
	"fmt"
	"math"
	"sync"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/calendar"
)

const maxQuestWireRecords = 255

/*
================
Runtime

Owns definition lookups and quest mutations. Completion pays rewards and
appends history; abandonment removes only the active record. Client 75C1D0
shares their presentation branch, but persistence preserves the distinction.
================
*/
type Runtime struct {
	gatherMu   sync.Mutex
	gatherJobs map[int64]gatherJob
	// SpawnQuestMonster places a script monster radiusMin + fraction *
	// radiusSpan from the character, inside the caller's character door.
	SpawnQuestMonster func(c *enterworld.Character, codename string, radiusMin, radiusSpan float64) bool
	CaptureRoll       func() (uint32, error)
	CalendarNow       func() calendar.Value
	calendarMu        sync.Mutex
	periodStarts      map[uint32]uint32
	calendarHour      uint8
	calendarNextMs    int64
	PlanInventory     func(*enterworld.Character, []inventory.ItemAmount, []inventory.ItemAmount) ([]enterworld.InventoryRow, []wire.Frame, error)
	deps              Dependencies
	Defs              *Definitions
	// ApplyExperience is the progression updater used inside the
	// quest-reward authority transaction. It opens no door itself, allowing
	// quest completion, gold, and experience to commit atomically.
	ApplyExperience func(character *enterworld.Character, expDelta, skillExpDelta int64, sourceGid uint32) ([]wire.Frame, bool)
}

/*
================
OpResult

Quest state and rewards remain private. Only the level-up presentation may
fan out to peers through Broadcast.
================
*/
type OpResult struct {
	Frames    []wire.Frame
	Broadcast []wire.Frame
}

/*
================
NewRuntime

Reject incomplete reward wiring before any character can accept a quest.
================
*/
func NewRuntime(
	deps Dependencies,
	defs *Definitions,
	applyExperience func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool),
) (*Runtime, error) {
	if defs == nil {
		return nil, fmt.Errorf("quest runtime: nil definitions")
	}
	if applyExperience == nil {
		for _, def := range defs.All() {
			for _, stage := range def.Stages {
				if stage.RewardExp > 0 || stage.RewardSkillExp > 0 {
					return nil, fmt.Errorf("quest %s stage requires experience granter", def.Codename)
				}
			}
			if def.RewardExp > 0 || def.RewardSkillExp > 0 {
				return nil, fmt.Errorf("quest runtime: definition %s pays %d exp but no exp granter is wired (progression GrantExperience)", def.Codename, def.RewardExp)
			}
		}
	}
	rt := &Runtime{deps: deps, Defs: defs, ApplyExperience: applyExperience, CalendarNow: calendar.Current, periodStarts: make(map[uint32]uint32)}
	for _, def := range defs.All() {
		rt.periodStarts[def.RefID] = def.PeriodStartLimit
	}
	return rt, nil
}

/*
================
BuildActiveQuestRecord

Untimed records omit the duration flag so the client retains its unlimited
sentinel. Objective values format the authored SN_CON text; talk objectives
carry the no-array sentinel. U08 carries the native run/limit nibble pair.
================
*/
func BuildActiveQuestRecord(def *Definition, progress uint32) enterworld.ActiveQuestRecord {
	if len(def.Stages) > 0 {
		def, _ = definitionAtStage(def, 0)
	}
	if def.Objective == ObjectiveParallel {
		record := enterworld.ActiveQuestRecord{RefID: def.RefID, Stage: def.stageIndex, U08: repeatTitleByte(def, 0), Flags: 0x08 | 0x10, U10: def.KindByte}
		for i := 0; i < missionCount(def); i++ {
			record.Contents = append(record.Contents, BuildActiveQuestRecord(missionDefinition(def, i), 0).Contents...)
		}
		return record
	}
	node := enterworld.ActiveQuestContentsNode{
		Tag:         def.missionIndex + 1,
		Kind:        1,
		Description: def.ContentsSymbol,
	}
	switch def.Objective {
	case ObjectiveCollect, ObjectiveKill:
		if progress > objectiveRequired(def) {
			progress = objectiveRequired(def)
		}
		if progress >= objectiveRequired(def) {
			node.Kind = 0
			node.CompletionReached = true
		}
		node.ObjectiveValues = []uint32{progress}
	default:
		node.ObjectiveSentinel = true
	}
	return enterworld.ActiveQuestRecord{
		RefID:    def.RefID,
		Stage:    def.stageIndex,
		U08:      repeatTitleByte(def, 0),
		Flags:    0x08 | 0x10,
		U10:      def.KindByte,
		Contents: []enterworld.ActiveQuestContentsNode{node},
	}
}

/*
================
activeQuestIndex

Call under the character read or mutation door while the list can change.
================
*/
func activeQuestIndex(character *enterworld.Character, refID uint32) int {
	for index, record := range character.ActiveQuests {
		if record.RefID == refID {
			return index
		}
	}
	return -1
}

/*
================
questCompleted

Completion history survives logout and governs repeat acceptance.
================
*/
func questCompleted(character *enterworld.Character, refID uint32) bool {
	for _, id := range character.CompletedQuestIds {
		if id == refID {
			return true
		}
	}
	return false
}

/*
================
objectiveMet

Recheck inventory truth at the authority boundary. Persisted counters may
be stale after a catalog correction or an item transaction.
================
*/
func objectiveMet(c *enterworld.Character, def *Definition, record enterworld.ActiveQuestRecord) bool {
	if waitingBranch(def, record) {
		return record.WaitAchieved
	}
	if _, capture := captureRuleForQuest(def.Codename); capture && record.RemainingMinutes == 0 {
		return false
	}
	if def.TimeLimitMinutes > 0 && record.RemainingMinutes == 0 {
		return false
	}
	switch def.Objective {
	case ObjectiveParallel:
		if len(def.Objectives) < 2 {
			return false
		}
		for i := range def.Objectives {
			m := missionDefinition(def, i)
			if !objectiveMet(c, m, missionRecord(record, m)) {
				return false
			}
		}
		return true
	case ObjectiveTalk:
		return true
	case ObjectiveDelivery:
		return deliveryMet(c, def)
	case ObjectiveCollect:
		return heldCollectCount(c, def) >= def.CollectCount
	case ObjectiveKill:
		return recordProgress(record) >= def.KillCount
	default:
		return false
	}
}

/*
================
heldCollectCount

Return progress saturated at the objective count, not the raw item balance.
Callers hold the character door while reading mutable inventory rows.
================
*/
func heldCollectCount(character *enterworld.Character, def *Definition) uint32 {
	var held int64
	for _, row := range character.MissionInventory {
		if inventory.InBag(character, row.Slot) && row.RefObjID == def.CollectItemRefID {
			count := row.StackCount
			if count < 1 {
				// A present row with no stack word is one item (the
				// equip rows' shape).
				count = 1
			}
			held += count
		}
	}
	if held < 0 {
		return 0
	}
	if held > int64(def.CollectCount) {
		return def.CollectCount
	}
	return uint32(held)
}

/*
================
recordProgress

Reads the first objective counter from the persisted journal projection.
================
*/
func recordProgress(record enterworld.ActiveQuestRecord) uint32 {
	if len(record.Contents) == 0 || len(record.Contents[0].ObjectiveValues) == 0 {
		return 0
	}
	return record.Contents[0].ObjectiveValues[0]
}

/*
================
StartQuest

Acceptance validates availability and plans delivery grants before adding
the journal record. Reacceptance uses current inventory, never stale progress.
================
*/
func (rt *Runtime) StartQuest(character *enterworld.Character, codename string) (OpResult, error) {
	if character == nil {
		return OpResult{}, fmt.Errorf("quest start: nil character")
	}
	codename, branch, branched := parseBranchToken(codename)
	def, ok := rt.Defs.ByCodename(codename)
	if !ok {
		return OpResult{}, fmt.Errorf("quest start: codename %s has no loaded definition", codename)
	}
	// A branching offer is accepted only with one of its replies.
	if branched != (len(def.OfferBranches) > 0) || branch >= max(1, len(def.OfferBranches)) {
		return OpResult{}, fmt.Errorf("quest start: %s needs one of its %d offer replies", codename, len(def.OfferBranches))
	}
	var refusal error
	var record enterworld.ActiveQuestRecord
	var inventoryFrames []wire.Frame
	changed := rt.deps.Update(character, "quest-start", func() bool {
		rt.calendarMu.Lock()
		defer rt.calendarMu.Unlock()
		if !rt.calendarAvailableLocked(def, false) {
			refusal = fmt.Errorf("quest start: %s calendar condition unavailable", codename)
			return false
		}
		if character.DeletePending {
			refusal = fmt.Errorf("quest start: character is pending deletion")
			return false
		}
		if activeQuestIndex(character, def.RefID) >= 0 {
			refusal = fmt.Errorf("quest start: %s (id %d) is already active", codename, def.RefID)
			return false
		}
		// The native login sections carry u8 counts. Never accept a record
		// which would disappear behind the login composer's 255-row boundary.
		if len(character.ActiveQuests) >= maxQuestWireRecords {
			refusal = fmt.Errorf("quest start: active quest wire capacity reached")
			return false
		}
		if !canAcceptAgain(character, def) {
			refusal = fmt.Errorf("quest start: %s (id %d) is already completed", codename, def.RefID)
			return false
		}
		if !prerequisitesMet(character, def) {
			refusal = fmt.Errorf("quest start: %s prerequisites are incomplete", codename)
			return false
		}
		level := int64(1)
		if character.Level != nil {
			level = *character.Level
		}
		if level < int64(def.Level) || (def.CountryByte != 3 && int(def.CountryByte) != enterworld.NativeCountryByte9C(character)) {
			refusal = fmt.Errorf("quest start: %s is unavailable for this level/country", codename)
			return false
		}
		var acceptanceItems []inventory.ItemAmount
		if def.Objective == ObjectiveDelivery {
			acceptanceItems = deliveryAmounts(def)
		}
		supply, suppliesTraps := captureSupplyForQuest(def.Codename)
		suppliesTraps = suppliesTraps && !supply.afterCompletion
		if suppliesTraps {
			acceptanceItems = append(acceptanceItems, inventory.ItemAmount{Codename: supply.item, Count: uint32(supply.count)})
		}
		// 897680 takes what it consumes before it grants the delivery, in the
		// same inventory transaction here.
		acceptanceTaken := acceptanceConsumption(character, def)
		if len(acceptanceItems) > 0 || len(acceptanceTaken) > 0 {
			if rt.PlanInventory == nil {
				refusal = fmt.Errorf("quest acceptance inventory owner unavailable")
				return false
			}
			rows, frames, err := rt.PlanInventory(character, acceptanceTaken, acceptanceItems)
			if err != nil {
				refusal = inventoryRefusal(def, err)
				return false
			}
			character.MissionInventory = rows
			inventoryFrames = frames
		}
		// 8B2420 grants Cerberus's first scissors without stamping the day:
		// only a refill spends it.
		if suppliesTraps && !supply.spendAtGrant {
			setCaptureSupply(character, def.RefID, rt.CalendarNow().Day, false)
		}
		progress := uint32(0)
		if def.Objective == ObjectiveCollect {
			// A collect quest accepted while the character already
			// holds objective items starts at the held count (the
			// recompute-from-inventory rule the action hook keeps).
			progress = heldCollectCount(character, def)
		}
		record = BuildActiveQuestRecord(def, progress)
		if branched {
			record.Branch = uint8(branch)
			if wait := def.OfferBranches[branch].WaitMinutes; wait > 0 {
				record.WaitMinutes = wait
				record.Progress = packQuestMinutes(wait)
				record.Flags |= 4
			}
		}
		if def.TimeLimitMinutes > 0 {
			record.RemainingMinutes = def.TimeLimitMinutes
			record.Progress = packQuestMinutes(uint16(record.RemainingMinutes))
			record.Flags |= 4
		}
		record, _ = refreshMissions(character, def, record, "", 0)
		record.U08 = repeatTitleByte(def, completionCount(character, def.RefID))
		next := make([]enterworld.ActiveQuestRecord, 0, len(character.ActiveQuests)+1)
		next = append(next, character.ActiveQuests...)
		next = append(next, record)
		character.ActiveQuests = next
		if len(acceptanceItems) > 0 || len(acceptanceTaken) > 0 {
			updates, _ := rt.applyInventoryChange(character)
			inventoryFrames = append(inventoryFrames, updates...)
		}
		if def.PeriodStartLimit > 0 {
			rt.periodStarts[def.RefID]--
		}
		return true
	})
	if refusal != nil {
		return OpResult{}, refusal
	}
	if !changed {
		return OpResult{}, fmt.Errorf("quest start: character is no longer authoritative")
	}
	frames := append(inventoryFrames, wire.Frame{Opcode: OpQuestUpdate, Payload: EncodeQuestUpdateInsert(record)})
	if def.AcceptNoticeSymbol != "" {
		frames = append(frames, questNotification(def.AcceptNoticeSymbol))
	}
	return OpResult{Frames: frames}, nil
}

/*
================
NpcOption

Semantic dialog row. Authored symbols remain localized by the client.
================
*/
type NpcOption struct {
	Codename, TitleSymbol, PromptSymbol      string
	AcceptResponseSymbol, DenyResponseSymbol string
	Pages                                    []OfferPage
	Branches                                 []NpcBranch
	Informational                            bool
	// SideTalk rows speak PromptSymbol and record it as heard through
	// AdvanceNpcQuest (89FDA0's pending bit).
	SideTalk bool
	Complete bool
	// AcceptRowSymbol, on an offer, is its one reply row, which accepts
	// (QuestSpec.OfferAcceptRowSymbol).
	AcceptRowSymbol string
}

/*
================
OptionsForNpc

Project active turn-ins before available offers under the character read door.
================
*/
func (rt *Runtime) OptionsForNpc(character *enterworld.Character, npcCodename string) []NpcOption {
	if character == nil || character.DeletePending {
		return nil
	}
	country := enterworld.NativeCountryByte9C(character)
	level := int64(1)
	if character.Level != nil {
		level = *character.Level
	}
	var completes, offers []NpcOption
	for _, def := range rt.Defs.All() {
		if def.StartNpcCodename == "" || (def.CountryByte != 3 && int(def.CountryByte) != country) {
			continue
		}
		active := activeQuestIndex(character, def.RefID) >= 0
		if active {
			def = branchDefinition(def, character.ActiveQuests[activeQuestIndex(character, def.RefID)])
		}
		if active && currentEndNpc(character, def) != npcCodename {
			if row, spoken := sideTalkOption(def, character.ActiveQuests[activeQuestIndex(character, def.RefID)], npcCodename); spoken {
				completes = append(completes, row)
				continue
			}
		}
		if active && len(def.Stages) > 0 {
			record := character.ActiveQuests[activeQuestIndex(character, def.RefID)]
			current, ok := definitionAtStage(def, record.Stage)
			if !ok || current.EndNpcCodename != npcCodename {
				continue
			}
			if stageObjectiveMet(character, current, record) {
				completes = append(completes, NpcOption{Codename: stageToken(def.Codename, record.Stage), TitleSymbol: def.TitleSymbol, PromptSymbol: current.CompletePromptSymbol, Pages: current.TalkPages, Complete: true})
			} else if current.NotAchievedSymbol != "" {
				// The stage's own BASIC_MENUSTRING_NOT_ACHIEVED line, as an
				// unstaged quest shows its own below.
				completes = append(completes, NpcOption{Codename: def.Codename, TitleSymbol: def.TitleSymbol, PromptSymbol: current.NotAchievedSymbol, Informational: true})
			}
			continue
		}
		if active && def.DeliveryNpcCodename == npcCodename && npcCodename != "" && heldCollectCount(character, def) < def.CollectCount {
			completes = append(completes, NpcOption{Codename: def.Codename, TitleSymbol: def.TitleSymbol, PromptSymbol: def.DeliveryPromptSymbol, Complete: true})
			continue
		}

		if active && questNpcMatches(def, def.EndNpcCodename, npcCodename) && (def.Objective == ObjectiveTalk || objectiveMet(character, def, character.ActiveQuests[activeQuestIndex(character, def.RefID)])) {
			if len(def.RewardChoices) > 0 {
				completes = append(completes, rewardChoiceOptions(def, country)...)
				continue
			}
			completes = append(completes, NpcOption{
				Codename: def.Codename, TitleSymbol: def.TitleSymbol,
				PromptSymbol: def.CompletePromptSymbol, Pages: def.TalkPages, Complete: true,
			})
			continue
		}
		// The level gates taking a quest, never reporting one already taken:
		// a character that lost a level keeps its turn-in (as MarkerStates).
		if !active && int64(def.Level) <= level && canAcceptAgain(character, def) && prerequisitesMet(character, def) && rt.calendarAvailable(def, false) && questNpcMatches(def, def.StartNpcCodename, npcCodename) {
			prompt := def.OfferPromptSymbol
			// Native 9206ec..92073f: DifferentString only selects the
			// after-one-clear offer when the persisted completion count > 0.
			if def.RepeatOfferPromptSymbol != "" && completionCount(character, def.RefID) > 0 {
				prompt = def.RepeatOfferPromptSymbol
			}
			offers = append(offers, NpcOption{
				Codename: def.Codename, TitleSymbol: def.TitleSymbol,
				PromptSymbol:         prompt,
				AcceptResponseSymbol: def.AcceptResponseSymbol, DenyResponseSymbol: def.DenyResponseSymbol,
				Pages: def.OfferPages, Branches: offerBranchRows(def), AcceptRowSymbol: def.OfferAcceptRowSymbol,
			})
		}
		if active && questNpcMatches(def, def.EndNpcCodename, npcCodename) && def.NotAchievedSymbol != "" {
			if supply, available := rt.captureSupplyOption(character, def, npcCodename); available {
				completes = append(completes, supply)
				continue
			}
			completes = append(completes, NpcOption{Codename: def.Codename, TitleSymbol: def.TitleSymbol, PromptSymbol: def.NotAchievedSymbol, Informational: true})
		}
		if !active {
			if supply, available := rt.captureSupplyOption(character, def, npcCodename); available {
				completes = append(completes, supply)
			}
		}
	}
	return append(completes, offers...)
}

/*
================
currentEndNpc

The end NPC of the active quest's current stage.
================
*/
func currentEndNpc(c *enterworld.Character, def *Definition) string {
	at := activeQuestIndex(c, def.RefID)
	if at < 0 {
		return ""
	}
	current, ok := definitionAtStage(def, c.ActiveQuests[at].Stage)
	if !ok {
		return ""
	}
	return current.EndNpcCodename
}

/*
================
CompleteTalkQuest

Talk objectives use the same atomic reward owner as combat and collection.
================
*/
func (rt *Runtime) CompleteTalkQuest(character *enterworld.Character, codename string) (OpResult, error) {
	def, ok := rt.Defs.ByCodename(codename)
	if !ok || def.Objective != ObjectiveTalk {
		return OpResult{}, fmt.Errorf("quest talk complete: invalid objective %s", codename)
	}
	return rt.completeReward(character, def)
}

/*
================
HandleGiveUp

Abandon an active, abandonable quest without appending completion history.
Cleanup items and journal removal share one transaction.
================
*/
func (rt *Runtime) HandleGiveUp(character *enterworld.Character, payload []byte) (OpResult, error) {
	refID, err := DecodeQuestRefRequest(payload)
	if err != nil {
		return OpResult{}, err
	}
	def, ok := rt.Defs.ByRefID(refID)
	if !ok {
		return OpResult{}, fmt.Errorf("quest give-up: id %d has no loaded definition", refID)
	}
	if def.KindByte == 2 {
		// The kind-2 window's button composes 0x729A, never 0x71EB
		// (sub_5c2440 @0x005c2465): a give-up for a reward-kind quest
		// is a crafted frame.
		return OpResult{}, fmt.Errorf("quest give-up: %s (id %d) is kind 2 (reward) - the client cannot compose 0x71EB for it", def.Codename, refID)
	}

	var refusal error
	var inventoryFrames []wire.Frame
	changed := rt.deps.Update(character, "quest-giveup", func() bool {
		if character == nil || character.DeletePending {
			refusal = fmt.Errorf("quest give-up: character unavailable")
			return false
		}
		at := activeQuestIndex(character, refID)
		if at < 0 {
			refusal = fmt.Errorf("quest give-up: %s (id %d) is not active", def.Codename, refID)
			return false
		}
		if len(def.Stages) > 0 && character.ActiveQuests[at].Stage > 0 {
			refusal = fmt.Errorf("quest tutorial already advanced; abandonment would reset paid stages")
			return false
		}
		rows, frames, err := rt.planQuestCleanup(character, def)
		if err != nil {
			refusal = err
			return false
		}
		character.MissionInventory, inventoryFrames = rows, frames
		next := make([]enterworld.ActiveQuestRecord, 0, len(character.ActiveQuests)-1)
		next = append(next, character.ActiveQuests[:at]...)
		next = append(next, character.ActiveQuests[at+1:]...)
		character.ActiveQuests = next
		objectives, _ := rt.applyInventoryChange(character)
		inventoryFrames = append(inventoryFrames, objectives...)
		rt.releasePeriodStart(def)
		return true
	})
	if refusal != nil {
		return OpResult{}, refusal
	}
	if !changed {
		return OpResult{}, fmt.Errorf("quest give-up: character is no longer authoritative")
	}
	if def.Codename == ivyMaterialQuest {
		rt.ForgetItemUse(character)
	}
	return OpResult{Frames: append(inventoryFrames, wire.Frame{Opcode: OpQuestUpdate, Payload: EncodeQuestUpdateAbandon(refID)})}, nil
}

/*
================
HandleRewardSelect

Only kind-2 quests admit the native reward-window request. Acknowledge after
the reward transaction so failed or replayed requests cannot signal success.
================
*/
func (rt *Runtime) HandleRewardSelect(character *enterworld.Character, payload []byte) (OpResult, error) {
	refID, err := DecodeQuestRefRequest(payload)
	if err != nil {
		return OpResult{}, err
	}
	def, ok := rt.Defs.ByRefID(refID)
	if !ok {
		return OpResult{}, fmt.Errorf("quest reward: id %d has no loaded definition", refID)
	}
	if def.KindByte != 2 {
		return OpResult{}, fmt.Errorf("quest reward: %s (id %d) is kind %d - only kind 2 opens the reward window (sub_5c26e0)", def.Codename, refID, def.KindByte)
	}

	result, err := rt.completeReward(character, def)
	if err != nil {
		return result, err
	}
	// 75C370 consumes successful B29A and triggers SND_QUEST. The
	// journal delta alone cannot acknowledge the native reward operation.
	ack := append([]byte{1}, payload...)
	result.Frames = append(result.Frames, wire.Frame{Opcode: 0xb29a, Payload: ack})
	return result, nil
}

/*
================
completeReward

Unstaged quests enter the shared transaction without a stage confirmation.
================
*/
func (rt *Runtime) completeReward(character *enterworld.Character, def *Definition) (OpResult, error) {
	return rt.completeRewardAt(character, def, nil, "")
}

/*
================
completeRewardAt

Plan inventory before committing any reward. Stage identity, objective truth
and exchange quantities are rechecked inside the same character door.
================
*/
func (rt *Runtime) completeRewardAt(character *enterworld.Character, def *Definition, expectedStage *uint16, npc string) (OpResult, error) {
	return rt.completeRewardChoice(character, def, expectedStage, npc, noRewardChoice)
}

/*
================
completeRewardChoice

completeRewardAt with the picked reward choice (noRewardChoice for a quest
without choices). A selection quest refuses any completion without one.
================
*/
func (rt *Runtime) completeRewardChoice(character *enterworld.Character, def *Definition, expectedStage *uint16, npc string, choice int) (OpResult, error) {
	refID := def.RefID
	if len(def.RewardChoices) > 0 && !rewardChoiceOffered(def, enterworld.NativeCountryByte9C(character), choice) {
		return OpResult{}, fmt.Errorf("quest reward: %s needs one of its %d reward choices", def.Codename, len(def.RewardChoices))
	}
	if len(def.RewardChoices) == 0 && choice != noRewardChoice {
		return OpResult{}, fmt.Errorf("quest reward: %s offers no reward choice", def.Codename)
	}
	if (len(def.RewardItems) != 0 || len(def.RewardChoices) != 0 || collectsItems(def)) && rt.PlanInventory == nil {
		return OpResult{}, fmt.Errorf("quest reward: %s requires an item reward owner", def.Codename)
	}
	var refusal error
	var goldFrame *wire.Frame
	var experienceFrames []wire.Frame
	var inventoryFrames []wire.Frame
	var objectiveFrames []wire.Frame
	var advanced *enterworld.ActiveQuestRecord
	root := def
	changed := rt.deps.Update(character, "quest-reward", func() bool {
		if character == nil || character.DeletePending {
			refusal = fmt.Errorf("quest reward: character unavailable")
			return false
		}
		at := activeQuestIndex(character, refID)
		if at < 0 {
			refusal = fmt.Errorf("quest reward: %s (id %d) is not active", def.Codename, refID)
			return false
		}
		if len(root.Stages) == 0 && character.ActiveQuests[at].Stage != 0 {
			refusal = fmt.Errorf("unexpected stage on an unstaged quest")
			return false
		}
		def = branchDefinition(def, character.ActiveQuests[at])
		if len(root.Stages) > 0 {
			if expectedStage == nil || character.ActiveQuests[at].Stage != *expectedStage {
				refusal = fmt.Errorf("quest stage confirmation is stale or absent")
				return false
			}
			var valid bool
			def, valid = definitionAtStage(root, *expectedStage)
			if !valid || npc == "" || npc != def.EndNpcCodename {
				refusal = fmt.Errorf("quest stage/NPC mismatch")
				return false
			}
		}
		if (len(def.RewardItems) > 0 || len(def.RewardChoices) > 0 || collectsItems(def)) && rt.PlanInventory == nil {
			refusal = fmt.Errorf("quest stage inventory owner unavailable")
			return false
		}
		if !stageObjectiveMet(character, def, character.ActiveQuests[at]) {
			refusal = fmt.Errorf("quest reward: %s (id %d) objective incomplete (%d/%d)", def.Codename, refID, recordProgress(character.ActiveQuests[at]), objectiveRequired(def))
			return false
		}
		// 8CF8F0 asks for the fee once the objective is met, before any
		// reward is paid: a character short of it keeps the quest.
		if def.TurnInGold > 0 && creditGold(character.Gold, 0) < def.TurnInGold {
			refusal = &dialogueRefusal{
				fmt.Errorf("quest reward: %s needs %d gold", def.Codename, def.TurnInGold),
				def.TurnInGoldShortSymbol,
			}
			return false
		}
		if (len(root.Stages) == 0 || int(def.stageIndex)+1 == len(root.Stages)) && !questCompleted(character, refID) && len(character.CompletedQuestIds) >= maxQuestWireRecords {
			refusal = fmt.Errorf("quest reward: completed quest wire capacity reached")
			return false
		}
		var inventoryRows []enterworld.InventoryRow
		if len(def.RewardItems) > 0 || len(def.RewardChoices) > 0 || collectsItems(def) {
			count, err := resuscitationExchangeCount(character, def)
			if err != nil {
				refusal = err
				return false
			}
			consume := collectionConsumption(def)
			for index := range consume {
				consume[index].Count *= count
			}
			if waitingBranch(def, character.ActiveQuests[at]) {
				// 89E670's waiting branch never needed the objective items:
				// whatever was gathered leaves with the quest instead.
				consume = consume[:0]
				for _, item := range collectionConsumption(def) {
					if held := captureItemCount(character, item.Codename); held > 0 {
						consume = append(consume, inventory.ItemAmount{Codename: item.Codename, Count: held})
					}
				}
			}
			consume = append(consume, captureSupplyCleanup(character, def)...)
			consume = append(consume, questToolCleanup(character, def)...)
			var grants []inventory.ItemAmount
			for _, r := range rewardItemsWithChoice(def, choice) {
				grants = append(grants, inventory.ItemAmount{Codename: rewardItemForCharacter(character, r.ItemCodename), Count: r.Count * count})
			}
			inventoryRows, inventoryFrames, err = rt.PlanInventory(character, consume, grants)
			if err != nil {
				refusal = inventoryRefusal(def, err)
				return false
			}
		}
		if def.RewardExp > 0 || def.RewardSkillExp > 0 {
			var applied bool
			experienceFrames, applied = rt.ApplyExperience(character, def.RewardExp, def.RewardSkillExp, 0)
			if !applied {
				refusal = fmt.Errorf("quest reward: %s (id %d) experience could not be applied", def.Codename, refID)
				return false
			}
		}
		if inventoryRows != nil {
			character.MissionInventory = inventoryRows
		}
		if len(root.Stages) > 0 && int(def.stageIndex)+1 < len(root.Stages) {
			nextDef, _ := definitionAtStage(root, def.stageIndex+1)
			progress := uint32(0)
			if nextDef.Objective == ObjectiveCollect {
				progress = heldCollectCount(character, nextDef)
			}
			record, _ := withJournalTargets(character, nextDef, BuildActiveQuestRecord(nextDef, progress))
			character.ActiveQuests[at] = record
			advanced = &record
		} else {
			next := make([]enterworld.ActiveQuestRecord, 0, len(character.ActiveQuests)-1)
			next = append(next, character.ActiveQuests[:at]...)
			next = append(next, character.ActiveQuests[at+1:]...)
			character.ActiveQuests = next
			recordCompletion(character, refID)
			if supply, exists := captureSupplyForQuest(def.Codename); exists && supply.afterCompletion {
				setCaptureSupply(character, refID, rt.CalendarNow().Day, false)
			}
			completed := make([]uint32, 0, len(character.CompletedQuestIds)+1)
			completed = append(completed, character.CompletedQuestIds...)
			if !questCompleted(character, refID) {
				completed = append(completed, refID)
			}
			character.CompletedQuestIds = completed
			// Finishing a superseded chain completes the quest that replaced it.
			creditPredecessorCompletions(character, rt.Defs)
		}
		objectiveFrames, _ = rt.applyInventoryChange(character)
		if def.RewardGold > 0 || def.TurnInGold > 0 {
			// The fee leaves after the reward is paid (8CF8F0), checked above.
			balance := creditGold(character.Gold, def.RewardGold) - def.TurnInGold
			character.Gold = &balance
			goldFrame = &wire.Frame{
				Opcode:  wire.OpPointsUpdate,
				Payload: wire.GoldRefresh{Balance: uint64(balance), Notify: true}.Encode(),
			}
		}
		// Slots follow the items and gold, as 924CF0 pays them. A grant past
		// the capacity limit is refused and the quest still completes: 4E19D0's
		// refusal is ignored by its caller.
		character.GrantInventoryExpansion(def.RewardInventorySlots)
		return true
	})
	if refusal != nil {
		return OpResult{}, refusal
	}
	if !changed {
		return OpResult{}, fmt.Errorf("quest reward: character is no longer authoritative")
	}

	frames := []wire.Frame{
		{Opcode: OpQuestUpdate, Payload: EncodeQuestUpdateComplete(refID)},
	}
	if advanced != nil {
		frames[0].Payload = EncodeQuestUpdateUpdate(*advanced)
		// A finished stage's own achieved-now line (Rahid 3's _13 once Ahmok
		// has talked: "Go speak with Town Chief Bukhra").
		if def.AchievedNowSymbol != "" {
			frames = append(frames, questNotification(def.AchievedNowSymbol))
		}
	}
	if advanced == nil && def.CompleteNoticeSymbol != "" {
		frames = append(frames, questNotification(def.CompleteNoticeSymbol))
	}
	if goldFrame != nil {
		frames = append(frames, *goldFrame)
	}
	frames = append(inventoryFrames, frames...)
	frames = append(frames, objectiveFrames...)
	frames = append(frames, experienceFrames...)
	return OpResult{
		Frames:    frames,
		Broadcast: wire.ProgressionBroadcastFrames(experienceFrames),
	}, nil
}

/*
================
creditGold

Saturate positive rewards instead of wrapping a persisted balance. Heal
negative balances to zero, matching the item-operation gold authority.
================
*/
func creditGold(stored *int64, reward int64) int64 {
	balance := int64(0)
	if stored != nil && *stored > 0 {
		balance = *stored
	}
	if reward <= 0 {
		return balance
	}
	if balance > math.MaxInt64-reward {
		return math.MaxInt64
	}
	return balance + reward
}

/*
================
NotifyInventoryChanged

Recompute owned collect objectives from current inventory and publish only
changed progress. Foreign journal records remain untouched.
================
*/
func (rt *Runtime) NotifyInventoryChanged(character *enterworld.Character) []wire.Frame {
	if character == nil || rt.Defs.Len() == 0 {
		return nil
	}
	var frames []wire.Frame
	rt.deps.Update(character, "quest-collect", func() bool {
		var changed bool
		frames, changed = rt.applyInventoryChange(character)
		return changed
	})
	return frames
}

/*
================
InventoryUpdater

Returns the updater that runs inside an existing item transaction. It opens
no nested authority door, so inventory and objective progress commit together.
================
*/
func (rt *Runtime) InventoryUpdater() func(*enterworld.Character) ([]wire.Frame, bool) {
	return rt.applyInventoryChange
}

/*
================
applyInventoryChange

Refresh collection missions without resetting unrelated kill or timer state.
================
*/
func (rt *Runtime) applyInventoryChange(character *enterworld.Character) ([]wire.Frame, bool) {
	if character == nil || character.DeletePending || rt.Defs.Len() == 0 {
		return nil, false
	}
	var frames []wire.Frame
	for index, record := range character.ActiveQuests {
		def, ok := rt.Defs.ByRefID(record.RefID)
		if ok {
			def, ok = definitionAtStage(def, record.Stage)
		}
		if !ok || !collectsItems(def) || def.TimeLimitMinutes > 0 && record.RemainingMinutes == 0 {
			continue
		}
		updated, changed := refreshMissions(character, def, record, "", 0)
		if !changed {
			continue
		}
		character.ActiveQuests[index] = updated
		frames = append(frames, missionProgressFrames(def, record, updated)...)
	}
	return frames, len(frames) > 0
}
