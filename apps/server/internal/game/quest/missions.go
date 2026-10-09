/*
===========================================================================

missions.go - parallel missions inside one quest's acceptance and reward

A parallel quest runs several counters (collect, kill) under one journal
record and one reward. This file loads them, maps persisted nodes back to
their missions, and rebuilds counters from inventory and kills.

===========================================================================
*/
package quest

import (
	"fmt"
	"slices"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
MissionSpec

Describes simultaneous counters within one acceptance/reward
transaction. Native collection data has a separate item, count and target
array per mission (86bf20 -> 929f1b); these are not tutorial stages.
================
*/
type MissionSpec struct {
	ContentsSymbol       string
	Objective            ObjectiveKind
	CollectItemCodename  string
	CollectCount         uint32
	KillMonsterCodenames []string
	KillCount            uint32
	KillRanks            []uint8
	MonsterDrop          *MonsterDropRule
	collectRef           uint32
	// A delivery mission is a two-leg hand-over at its own NPC (QuestSpec's
	// fields of the same names). Its items are granted at acceptance with
	// every other mission's, and its node latches when it is handed over
	// (91CEB0 keeps quest-user +3 as a bit per mission).
	DeliveryItems       []RewardItemLead
	DeliveryKeepsItems  bool
	HandOverNpcCodename string
	HandOverSymbol      string
	HandOverPages       []OfferPage
	ExchangeItems       []RewardItemLead
	ExchangeFullSymbol  string
	NotAchievedSymbol   string
	// PendingNoticeSymbol (mission +0x108) is sent when another mission is
	// handed over while this one still waits ("Doji's medicine has been
	// delivered. Deliver Bori's book to Chau."). Inference: its only writer is
	// QuestLua_LoadQuestConfiguration (92A7E8), and neither the deliver handler
	// nor 91CEB0 reads it; every line reads as that reminder.
	PendingNoticeSymbol string
	deliveryRefs        []uint32
	handOverNpcRef      uint32
}

/*
================
loadMissions
================
*/
func loadMissions(def *Definition, symbols []string, items enterworld.ItemRefSource) error {
	if def.Objective != ObjectiveParallel {
		if len(def.Objectives) != 0 {
			return fmt.Errorf("quest %s missions on a scalar objective", def.Codename)
		}
		return nil
	}
	if len(def.Objectives) < 2 || len(def.Objectives) > 255 || len(def.Stages) != 0 || def.MonsterDrop != nil || def.CollectCount != 0 || def.KillCount != 0 {
		return fmt.Errorf("quest %s invalid parallel mission contract", def.Codename)
	}
	def.Objectives = append([]MissionSpec(nil), def.Objectives...)
	collected := map[string]bool{}
	for i := range def.Objectives {
		m := &def.Objectives[i]
		if !slices.Contains(symbols, m.ContentsSymbol) {
			return fmt.Errorf("quest %s mission %d missing contents", def.Codename, i)
		}
		switch m.Objective {
		case ObjectiveCollect:
			if items == nil || m.CollectCount == 0 || m.CollectItemCodename == "" || collected[m.CollectItemCodename] || m.KillCount != 0 || len(m.KillMonsterCodenames) != 0 {
				return fmt.Errorf("quest %s mission %d invalid collection", def.Codename, i)
			}
			ref, ok := items.ItemRefByCodename(m.CollectItemCodename)
			if !ok || ref == nil {
				return fmt.Errorf("quest %s mission %d unresolved item", def.Codename, i)
			}
			m.collectRef = ref.RefObjID
			collected[m.CollectItemCodename] = true
		case ObjectiveKill:
			if m.KillCount == 0 || len(m.KillMonsterCodenames) == 0 || m.CollectCount != 0 || m.CollectItemCodename != "" {
				return fmt.Errorf("quest %s mission %d invalid kill targets", def.Codename, i)
			}
		case ObjectiveDelivery:
			// A parallel delivery completes only through the report, so each
			// one is a two-leg hand-over at its own NPC.
			if m.HandOverNpcCodename == "" {
				return fmt.Errorf("quest %s mission %d parallel delivery without a hand-over", def.Codename, i)
			}
			md := missionDefinition(def, i)
			if err := loadDelivery(md, items); err != nil {
				return err
			}
			if err := validateDeliveryExtras(md.QuestSpec, items); err != nil {
				return err
			}
			m.deliveryRefs = md.deliveryRefs
		default:
			return fmt.Errorf("quest %s mission %d unsupported kind", def.Codename, i)
		}
		if err := validateKillRanks(missionDefinition(def, i).QuestSpec); err != nil {
			return err
		}
		if err := validateMonsterDrop(missionDefinition(def, i).QuestSpec); err != nil {
			return err
		}
	}
	return nil
}

/*
================
missionCount
================
*/
func missionCount(def *Definition) int {
	if def.Objective == ObjectiveParallel {
		return len(def.Objectives)
	}
	return 1
}

/*
================
missionDefinition
================
*/
func missionDefinition(def *Definition, index int) *Definition {
	if def.Objective != ObjectiveParallel {
		return def
	}
	m := def.Objectives[index]
	d := *def
	d.Objectives = nil
	d.Objective, d.ContentsSymbol = m.Objective, m.ContentsSymbol
	d.missionIndex = uint8(index)
	d.CollectItemCodename, d.CollectCount, d.CollectItemRefID = m.CollectItemCodename, m.CollectCount, m.collectRef
	d.KillRanks = m.KillRanks
	d.KillMonsterCodenames, d.KillCount, d.MonsterDrop = m.KillMonsterCodenames, m.KillCount, m.MonsterDrop
	d.DeliveryItems, d.DeliveryKeepsItems, d.deliveryRefs = m.DeliveryItems, m.DeliveryKeepsItems, m.deliveryRefs
	d.HandOverNpcCodename, d.HandOverSymbol, d.HandOverPages = m.HandOverNpcCodename, m.HandOverSymbol, m.HandOverPages
	d.ExchangeItems, d.ExchangeFullSymbol, d.handOverNpcRef = m.ExchangeItems, m.ExchangeFullSymbol, m.handOverNpcRef
	if m.NotAchievedSymbol != "" {
		d.NotAchievedSymbol = m.NotAchievedSymbol
	}
	return &d
}

/*
================
missionNodeIndex

Native 91BF04 publishes mission-data +8 plus one as the wire tag.
Captions are not identities: compiled EU_GENERAL_2 uses the same caption
for two independent missions. Old port records used tag 1 for every node;
migrate those only when the caption identifies exactly one persisted node.
================
*/
func missionNodeIndex(nodes []enterworld.ActiveQuestContentsNode, tag uint8, description string) int {
	for i, node := range nodes {
		if node.Tag == tag && node.Description == description {
			return i
		}
	}
	found := -1
	for i, node := range nodes {
		if node.Description == description {
			if found != -1 {
				return -1
			}
			found = i
		}
	}
	return found
}

/*
================
missionRecord
================
*/
func missionRecord(record enterworld.ActiveQuestRecord, def *Definition) enterworld.ActiveQuestRecord {
	if i := missionNodeIndex(record.Contents, def.missionIndex+1, def.ContentsSymbol); i >= 0 {
		record.Contents = []enterworld.ActiveQuestContentsNode{record.Contents[i]}
		return record
	}
	record.Contents = nil
	return record
}

/*
================
NormalizeEntryRecords

Operates on the detached bootstrap snapshot. Legacy
tag-1 siblings must be repaired before the client's tag-keyed merge sees
them, not deferred until the next kill. No acceptance or reward is replayed.
A character who finished a superseded chain before its replacement existed
is shown the replacement as done (CompletedBy); the server's own gates read
the same rule, so nothing is persisted here.
================
*/
func (rt *Runtime) NormalizeEntryRecords(c *enterworld.Character) error {
	creditPredecessorCompletions(c, rt.Defs)
	for i, record := range c.ActiveQuests {
		root, ok := rt.Defs.ByRefID(record.RefID)
		if !ok {
			continue
		}
		def, ok := definitionAtStage(root, record.Stage)
		if !ok {
			return fmt.Errorf("quest %s invalid persisted stage %d", root.Codename, record.Stage)
		}
		// Records saved before the journal carried targets gain them here, so
		// the enter-world section already names the NPC to visit.
		c.ActiveQuests[i], _ = withJournalTargets(c, def, c.ActiveQuests[i])
		if def.Objective != ObjectiveParallel {
			continue
		}
		nodes := make([]enterworld.ActiveQuestContentsNode, missionCount(def))
		used := make(map[int]bool)
		for j := range nodes {
			m := missionDefinition(def, j)
			at := missionNodeIndex(record.Contents, m.missionIndex+1, m.ContentsSymbol)
			if at < 0 || used[at] {
				return fmt.Errorf("quest %s unresolved persisted mission %d", def.Codename, j)
			}
			used[at] = true
			nodes[j] = record.Contents[at]
			nodes[j].Tag = m.missionIndex + 1
		}
		if len(used) != len(record.Contents) {
			return fmt.Errorf("quest %s orphan persisted mission", def.Codename)
		}
		c.ActiveQuests[i].Contents = nodes
	}
	return nil
}

/*
================
collectsItems
================
*/
func collectsItems(def *Definition) bool {
	if def.Objective == ObjectiveDelivery {
		return true
	}
	for i := 0; i < missionCount(def); i++ {
		if kind := missionDefinition(def, i).Objective; kind == ObjectiveCollect || kind == ObjectiveDelivery {
			return true
		}
	}
	return false
}

/*
================
collectionConsumption

What the turn-in takes: each collect mission's count, or the delivery
items unless the mission keeps them (QuestSpec.DeliveryKeepsItems).
================
*/
func collectionConsumption(def *Definition) []inventory.ItemAmount {
	if def.Objective == ObjectiveDelivery {
		// A hand-over already took the items at its own NPC.
		if def.DeliveryKeepsItems || def.HandOverNpcCodename != "" {
			return nil
		}
		return deliveryAmounts(def)
	}
	var out []inventory.ItemAmount
	for i := 0; i < missionCount(def); i++ {
		m := missionDefinition(def, i)
		if m.Objective == ObjectiveCollect {
			out = append(out, inventory.ItemAmount{Codename: m.CollectItemCodename, Count: m.CollectCount})
		}
	}
	return out
}

/*
================
refreshMissions

Rebuild contents while retaining the envelope (run number, timers, targets,
stage). Inventory changes must never erase a simultaneous kill counter.
================
*/
func refreshMissions(c *enterworld.Character, def *Definition, record enterworld.ActiveQuestRecord, killed string, rarity uint8) (enterworld.ActiveQuestRecord, bool) {
	// A waiting branch's progress is its timer (89E050), never item counts.
	if waitingBranch(def, record) {
		return record, false
	}
	updated := record
	updated.Contents = nil
	changed := false
	for i := 0; i < missionCount(def); i++ {
		m := missionDefinition(def, i)
		old := missionRecord(record, m)
		progress := recordProgress(old)
		if m.Objective == ObjectiveCollect {
			progress = heldCollectCount(c, m)
		}
		if m.Objective == ObjectiveKill && killed != "" && killTargetMatches(m, killed, rarity) && progress < m.KillCount {
			progress++
		}
		next := BuildActiveQuestRecord(m, progress).Contents[0]
		if len(old.Contents) == 1 {
			next.CompletionReached = next.CompletionReached || missionCompletionReached(old.Contents[0])
		}
		// A handed-over delivery stays complete in the journal: no later
		// inventory change undoes 91CA00's latch.
		if m.Objective == ObjectiveDelivery && next.CompletionReached {
			next.Kind = 0
		}
		if len(old.Contents) != 1 || old.Contents[0].Tag != next.Tag || old.Contents[0].Kind != next.Kind || !slices.Equal(old.Contents[0].ObjectiveValues, next.ObjectiveValues) {
			changed = true
		}
		updated.Contents = append(updated.Contents, next)
	}
	updated, retargeted := withJournalTargets(c, def, updated)
	return updated, changed || retargeted
}

/*
================
missionCompletionReached
================
*/
func missionCompletionReached(node enterworld.ActiveQuestContentsNode) bool {
	return node.CompletionReached || node.Kind == 0 || node.Kind == 2
}

/*
================
encodeMissionProgress

Native mission gates return 2 only on the first threshold hit. Persisted
complete state is 0; the edge belongs to the ordered publication, not login.
================
*/
func encodeMissionProgress(previous, next enterworld.ActiveQuestRecord) []byte {
	next.Flags &^= 4 // Contents updates must not rearm the independent client timer.
	next.Contents = slices.Clone(next.Contents)
	for i := range next.Contents {
		node := &next.Contents[i]
		if node.Kind != 0 {
			continue
		}
		at := missionNodeIndex(previous.Contents, node.Tag, node.Description)
		if at >= 0 && !missionCompletionReached(previous.Contents[at]) {
			node.Kind = 2
		}
	}
	return EncodeQuestUpdateUpdate(next)
}

/*
================
missionProgressFrames

The journal update for one refreshed record, then the quest's ACHIEVED_NOW
banner on the publication where every mission first stands complete. The
completion edge is the same one encodeMissionProgress marks with kind 2;
collection completion is sticky, so dropping and regaining an item does
not repeat the banner.
================
*/
func missionProgressFrames(def *Definition, previous, next enterworld.ActiveQuestRecord) []wire.Frame {
	frames := []wire.Frame{{Opcode: OpQuestUpdate, Payload: encodeMissionProgress(previous, next)}}
	if def.AchievedNowSymbol != "" && !allMissionsReached(previous) && allMissionsReached(next) {
		frames = append(frames, questNotification(def.AchievedNowSymbol))
	}
	return frames
}

/*
================
allMissionsReached
================
*/
func allMissionsReached(record enterworld.ActiveQuestRecord) bool {
	if len(record.Contents) == 0 {
		return false
	}
	for _, node := range record.Contents {
		if !missionCompletionReached(node) {
			return false
		}
	}
	return true
}

/*
================
killTargetMatches

Native 91bc82..91bc9b indexes the rank by the matching species, and
CGObjMob::GetMonsterClass (4c1c60) returns only the low rarity nibble.
================
*/
func killTargetMatches(def *Definition, code string, rarity uint8) bool {
	for i, target := range def.KillMonsterCodenames {
		if target == code && (len(def.KillRanks) == 0 || i < len(def.KillRanks) && def.KillRanks[i] == rarity&15) {
			return true
		}
	}
	return false
}

/*
================
validateKillRanks
================
*/
func validateKillRanks(spec QuestSpec) error {
	if len(spec.KillRanks) == 0 {
		return nil
	}
	if spec.Objective != ObjectiveKill || len(spec.KillRanks) != len(spec.KillMonsterCodenames) {
		return fmt.Errorf("quest %s rank/species arity mismatch", spec.Codename)
	}
	for _, rank := range spec.KillRanks {
		if rank > 15 {
			return fmt.Errorf("quest %s rank outside native nibble", spec.Codename)
		}
	}
	return nil
}
