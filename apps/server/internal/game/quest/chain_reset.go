/*
===========================================================================

chain_reset.go - quests that reopen earlier quests, and the kill exchange

The Roc chain (#3399) is a loop, not a line. QNO_RM_MEETROC_6 is offered
once QNO_RM_MEETROC_2 is done, and paying it (90D4C0) or giving it up
(90CE00) zeroes MEETROC_2's record and sets its completion count to 0
(player vtable +0x1B4, 571590), so the village chief offers MEETROC_2
again. Giving MEETROC_2 up (90C1C0) does the same to MEETROC_1. Each
MEETROC_6 run pays one of each ECT token, which the MEETROC_ECT quests
take 7, 10 and 15 of.

The port forgets the named quests' history the way /INITQ does
(forgetQuestHistory): the completed list, the count, ended state and
capture supplies go. Like /INITQ it sends no frame for them. The client
reads completions only at login, and the NPC's offer is the server's.

MEETROC_6's kill handler (90C9D0) is the other rule here. Each kill sets
its mission's bit. Once all seven are set, and the character holds no
Soul Fragment (_6_02) but holds a Soul Crystal (_6_03), every crystal
leaves, one fragment is granted, the record's achieved key (+3) takes
the kill mask and the achieved-now line is sent. CBasicQuest_vfEC
(922CF0) pays on that key alone, so without a crystal the seven kills
never complete the quest. The port's key is holding the fragment: the
generator projects it as the quest's last, required collect row.

===========================================================================
*/
package quest

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
KillExchange

Once every kill mission is complete: when GrantItem is not held and
TakeItem is, every TakeItem stack leaves and one GrantItem is granted.
================
*/
type KillExchange struct {
	TakeItem  string
	GrantItem string
}

/*
================
validateChainItems

Every item the chain rules name must be in the shipped itemdata, and a kill
exchange needs kill missions to wait on.
================
*/
func validateChainItems(spec QuestSpec, items enterworld.ItemRefSource) error {
	codes := append([]string(nil), spec.QuestItemCleanup...)
	if ex := spec.KillExchange; ex != nil {
		if ex.TakeItem == "" || ex.GrantItem == "" || ex.TakeItem == ex.GrantItem {
			return fmt.Errorf("quest %s kill exchange needs two different items", spec.Codename)
		}
		if spec.Objective != ObjectiveParallel {
			return fmt.Errorf("quest %s kill exchange without parallel missions", spec.Codename)
		}
		codes = append(codes, ex.TakeItem, ex.GrantItem)
	}
	if spec.GiveUpKeepsCollected && spec.Objective != ObjectiveCollect && spec.Objective != ObjectiveParallel {
		return fmt.Errorf("quest %s keeps collected items without collecting any", spec.Codename)
	}
	for _, code := range codes {
		if items == nil {
			return fmt.Errorf("quest %s needs the shipped itemdata to resolve %s", spec.Codename, code)
		}
		if ref, ok := items.ItemRefByCodename(code); !ok || ref == nil {
			return fmt.Errorf("quest %s names unresolved item %s", spec.Codename, code)
		}
	}
	return nil
}

/*
================
forgetQuests

Forgets the history of every quest in ids. Returns whether anything changed.
================
*/
func forgetQuests(c *enterworld.Character, ids []uint32) bool {
	changed := false
	for _, id := range ids {
		if forgetQuestHistory(c, id) {
			changed = true
		}
	}
	return changed
}

/*
================
questItemCleanup

Tops planned consumption up to every held stack of the quest's cleanup
items, so an item the turn-in or the mission cleanup already takes is never
taken twice.
================
*/
func questItemCleanup(c *enterworld.Character, def *Definition, planned []inventory.ItemAmount) []inventory.ItemAmount {
	var out []inventory.ItemAmount
	for _, code := range def.QuestItemCleanup {
		held := captureItemCount(c, code)
		for _, item := range planned {
			if item.Codename == code {
				held -= min(held, item.Count)
			}
		}
		if held > 0 {
			out = append(out, inventory.ItemAmount{Codename: code, Count: held})
		}
	}
	return out
}

/*
================
killMissionsReached

True when the record holds every kill mission of def, each complete.
================
*/
func killMissionsReached(def *Definition, record enterworld.ActiveQuestRecord) bool {
	kills := 0
	for i := 0; i < missionCount(def); i++ {
		m := missionDefinition(def, i)
		if m.Objective != ObjectiveKill {
			continue
		}
		kills++
		node := missionRecord(record, m)
		if len(node.Contents) != 1 || !missionCompletionReached(node.Contents[0]) {
			return false
		}
	}
	return kills > 0
}

/*
================
applyKillExchange

The kill handler's exchange for the active record at index at, inside the
caller's character transaction. The take and the grant are one inventory
plan: a bag with no room leaves both items where they are, and the next
kill tries again (native deletes the crystals first and dumps when the
grant fails). The refreshed journal, and with it the achieved-now line,
comes from applyInventoryChange.
================
*/
func (rt *Runtime) applyKillExchange(c *enterworld.Character, def *Definition, at int) []wire.Frame {
	ex := def.KillExchange
	if ex == nil || rt.PlanInventory == nil || !killMissionsReached(def, c.ActiveQuests[at]) {
		return nil
	}
	held := captureItemCount(c, ex.TakeItem)
	if held == 0 || captureItemCount(c, ex.GrantItem) > 0 {
		return nil
	}
	rows, frames, err := rt.PlanInventory(c, []inventory.ItemAmount{{Codename: ex.TakeItem, Count: held}},
		[]inventory.ItemAmount{{Codename: ex.GrantItem, Count: 1}})
	if err != nil {
		return nil
	}
	c.MissionInventory = rows
	objectives, _ := rt.applyInventoryChange(c)
	return append(frames, objectives...)
}
