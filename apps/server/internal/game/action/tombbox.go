/*
===========================================================================

tombbox.go - opening a Ch'in Tomb treasure box, and the keys tomb monsters drop

Port-only switch, native content (SRO_TOMB_TREASURE_BOXES, #4810; the
tables and rolls are game/tombbox). A box offers one row, "Open the box".
The v1.188 server's open handler checks, in order: two free bag slots
(else the bag-full line), the box's key (else the no-key line). It then
rolls one of nine outcomes, applies it and spends the key. The change
commits as one step inside the character's door: the key leaves with the
treasure, or nothing moves at all.

A tomb monster's death rolls every box's key in turn (the same events'
monster-death handler), each one an independent chance.

INFERENCE: the handler has no talk line for a treasure, so a granted
treasure answers the box's own greeting, which leaves the dialog where
it started. Summoned guards are aggressive: the summon exists to punish
the opener.

===========================================================================
*/
package action

import (
	"fmt"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/tombbox"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
tombBoxOpen

What one open committed or refused: the answer line, the opener's
frames, the frames observers see (an HP refresh), and whether guards
are to be summoned once the door has closed.
================
*/
type tombBoxOpen struct {
	answer        string
	actor, public []wire.Frame
	guards        bool
	err           error
}

/*
================
Runtime.tombBoxTalkOptions

The open row a box offers while the switch is on; any other NPC, or the
switch off, offers nothing.
================
*/
func (rt *Runtime) tombBoxTalkOptions(npc string) []NpcQuestOption {
	box, ok := tombbox.BoxAt(npc)
	if !rt.TombBoxes || !ok {
		return nil
	}
	return []NpcQuestOption{{Codename: tombbox.Service(box), TitleSymbol: tombbox.TalkOption, PromptSymbol: tombBoxGreeting(box), Immediate: true}}
}

/*
================
tombBoxGreeting

The box's quest line (textquest SN_<box>_QS), its talk prompt.
================
*/
func tombBoxGreeting(box tombbox.Box) string {
	return "SN_" + box.Npc + "_QS"
}

/*
================
Runtime.openTombBox

One click of the open row. The door commits the key and the treasure
together; guards are summoned and observers told of the opener's HP
only after it has closed.
================
*/
func (rt *Runtime) openTombBox(division string, character *enterworld.Character, npc string) ([]wire.Frame, error) {
	box, ok := tombbox.BoxAt(npc)
	if !rt.TombBoxes || !ok {
		return nil, fmt.Errorf("tomb box: %s offers no box", npc)
	}
	var open tombBoxOpen
	rt.deps.Update(character, "tomb-box-open", func() bool {
		return rt.openTombBoxInDoor(division, character, box, &open)
	})
	if open.err != nil {
		return nil, open.err
	}
	if open.answer == "" {
		return nil, fmt.Errorf("tomb box: %s could not commit", npc)
	}
	if open.guards {
		for range box.GuardCount {
			// No v1.150 guard row means no spawn; the line still answers
			// (#4810 slice 2 stages the rows).
			rt.SpawnQuestMonster(division, character, simulation.QuestMonsterSpawn{
				Codename: box.Guard, RadiusSpan: tombbox.GuardRadius, Aggressive: true,
			})
		}
	}
	if len(open.public) != 0 && rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(division, character.Name, open.public)
	}
	return append(open.actor, wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(open.answer)}), nil
}

/*
================
Runtime.openTombBoxInDoor

The refusals, the roll and the outcome, run inside the character's
door. A refusal changes nothing; an outcome whose plan fails (a bag that
cannot take the treasure) answers the bag-full line and keeps the key.
================
*/
func (rt *Runtime) openTombBoxInDoor(division string, c *enterworld.Character, box tombbox.Box, open *tombBoxOpen) bool {
	if freeBagSlots(c) < tombbox.MinFreeSlots {
		open.answer = tombbox.TalkBagFull
		return false
	}
	if bagTokens(c, box.Key) == 0 {
		open.answer = tombbox.TalkNoKey
		return false
	}
	if rt.DropRoll == nil {
		open.err = fmt.Errorf("tomb box: no roll source")
		return false
	}
	draw, err := rt.DropRoll()
	if err != nil {
		open.err = fmt.Errorf("tomb box roll: %w", err)
		return false
	}
	key := []inventory.ItemAmount{{Codename: box.Key, Count: 1}}
	outcome := tombbox.PickOutcome(box, draw)
	var rows []enterworld.InventoryRow
	var frames []wire.Frame
	open.answer = tombBoxGreeting(box)
	switch outcome {
	case tombbox.OutcomePotions:
		rows, frames, err = rt.PlanQuestInventory(c, key, []inventory.ItemAmount{{Codename: box.Potion, Count: box.PotionCount}})
	case tombbox.OutcomeRecipe:
		rows, frames, err = rt.PlanQuestInventory(c, key, []inventory.ItemAmount{{Codename: box.Recipe, Count: 1}})
	case tombbox.OutcomeItem, tombbox.OutcomeItemRepair, tombbox.OutcomeSeal, tombbox.OutcomeSealRepair:
		item, picked := rt.tombBoxItem(box, outcome, c.Name)
		if !picked {
			open.err = fmt.Errorf("tomb box: no equipment for outcome %d", outcome)
			return false
		}
		rows, frames, err = rt.planQuestInventoryItems(c, key, []inventory.Item{item})
	default:
		rows, frames, err = rt.PlanQuestInventory(c, key, nil)
	}
	if err != nil {
		open.answer = tombbox.TalkBagFull
		return false
	}
	c.MissionInventory = rows
	open.actor = append(open.actor, frames...)
	switch outcome {
	case tombbox.OutcomeDamage:
		open.answer = tombbox.TalkDamage
		rt.tombBoxDamage(division, c, box, open)
	case tombbox.OutcomeGuards:
		open.answer = tombbox.TalkGuards
		open.guards = true
	case tombbox.OutcomeGold:
		if box.Gold > 0 && goldOf(c) <= ^uint64(0)>>1-uint64(box.Gold) {
			setGold(c, goldOf(c)+uint64(box.Gold))
		}
		open.actor = append(open.actor, wire.Frame{Opcode: wire.OpPointsUpdate, Payload: wire.GoldRefresh{Balance: goldOf(c), Notify: true}.Encode()})
	}
	return true
}

/*
================
Runtime.tombBoxDamage

Outcome 0: the box takes DamagePercent of the opener's maximum HP, and
never the last point (the server clamps the loss to current HP - 1). The
opener gets a vitals refresh, observers an HP refresh, as every HP
change outside combat sends (murdererdrain.go).
================
*/
func (rt *Runtime) tombBoxDamage(division string, c *enterworld.Character, box tombbox.Box, open *tombBoxOpen) {
	stats, _, err := rt.playerCombatStats(division, c)
	if err != nil {
		return
	}
	maxHP, _, currentHP, currentMP := keeperVitalsFrom(c, stats, nil)
	nextHP := max(currentHP-maxHP*box.DamagePercent/100, 1)
	if nextHP >= currentHP {
		return
	}
	c.CurrentHP = &nextHP
	gid := enterworld.ObjectIDForCharacter(c)
	open.actor = append(open.actor, wire.Frame{Opcode: simulation.OpVitalsUpdate,
		Payload: simulation.VitalsRefreshPayload(gid, simulation.Vitals{CurrentHP: uint32(nextHP), CurrentMP: uint32(currentMP)})})
	open.public = append(open.public, wire.Frame{Opcode: simulation.OpVitalsUpdate,
		Payload: simulation.HPRefreshPayload(gid, 0, uint32(nextHP))})
}

/*
================
Runtime.tombBoxItem

Outcomes 5-8: one piece drawn uniformly from the top-degree bucket of
its kind, with the box's plus, made the way any drop makes it (variance,
options) and, for the not-repairable outcomes, marked so. A degree the
drop rules refuse (the beta caps) is stepped past like an empty one.
================
*/
func (rt *Runtime) tombBoxItem(box tombbox.Box, outcome int, owner string) (inventory.Item, bool) {
	repair := outcome == tombbox.OutcomeItemRepair || outcome == tombbox.OutcomeSealRepair
	index := 0
	if repair {
		index = 1
	}
	seal, plus := 0, box.ItemPlus[index]
	if outcome == tombbox.OutcomeSeal || outcome == tombbox.OutcomeSealRepair {
		seal, plus = box.SealGrade[index], box.SealPlus[index]
	}
	admit := func(codename string) bool {
		ref, ok := rt.deps.ItemReferences().ItemRefByCodename(codename)
		return ok && ref != nil && !rt.AboveDegree9Drops.Excludes(ref.TypeIDs, enterworld.ItemDegree(ref))
	}
	codes := loot.TreasureBoxEquipment(loot.TreasureBoxTopDegree, seal, admit)
	if len(codes) == 0 || rt.DropRoll == nil {
		return inventory.Item{}, false
	}
	draw, err := rt.DropRoll()
	if err != nil {
		return inventory.Item{}, false
	}
	return rt.prepareSelectedItem(loot.DropItem{Codename: codes[int(draw)%len(codes)], Count: 1, Plus: plus, NonRepair: !repair}, owner)
}

/*
================
freeBagSlots

The empty bag slots, the room the open demands first.
================
*/
func freeBagSlots(c *enterworld.Character) int {
	used := map[int64]bool{}
	for _, row := range c.MissionInventory {
		if row.Slot >= int64(inventory.EquipmentSlotEnd) {
			used[row.Slot] = true
		}
	}
	free := 0
	for slot := int64(inventory.EquipmentSlotEnd); slot < int64(inventory.BagEnd(c)); slot++ {
		if !used[slot] {
			free++
		}
	}
	return free
}

/*
================
Runtime.tombKeyDrops

A tomb monster's death rolls each box's key once: its own chance, eight
times for a party monster, over two 15-bit draws. Every key that wins
drops at the monster for the kill's owner. Nothing with the switch off.
================
*/
func (rt *Runtime) tombKeyDrops(defeated monster.Instance, at simulation.Spawn, owner string, now time.Time) []grounditem.Item {
	if !rt.TombBoxes || rt.DropRoll == nil || !tombbox.IsTombMonster(defeated.Ref.Codename) {
		return nil
	}
	var keys []grounditem.Item
	for _, box := range tombbox.Boxes {
		hi, err := rt.DropRoll()
		if err != nil {
			return keys
		}
		lo, err := rt.DropRoll()
		if err != nil {
			return keys
		}
		if !tombbox.KeyDrops(box, isPartyMonster(defeated), hi, lo) {
			continue
		}
		if key, ok := rt.prepareSelectedDrop(loot.DropItem{Codename: box.Key, Count: 1, Assigned: true}, at, owner, now); ok {
			keys = append(keys, key)
		}
	}
	return keys
}
