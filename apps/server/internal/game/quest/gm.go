/*
===========================================================================

gm.go - the GM's quest tools: /INITQ, /REMOVEQ, /COMPQ and /RESETQ

The v1.150 client composes them as GM subcommands 0x1B, 0x1E, 0x1D and 0x1C
(ChatInput_HandleGmCommand): the discriminator byte, then a sized ANSI
quest
codename for all but /RESETQ. They act on the GM's own journal. The shipped
client names no effect and no server binary is at hand, so the meanings are
INFERRED from the names and the Korean developers' habit of "init" for
"reset to the starting state":

	/REMOVEQ q  drop q from the active journal; completion history stays
	/INITQ q    return q to its never-taken state: drop it from the active
	            journal and forget its completions, count, ending and supply
	/COMPQ q    complete the active q as if its objective were met, through
	            the one reward owner (settleReward, force)
	/RESETQ     /INITQ for every quest the character holds or has finished

The GM-only privilege check is the dispatcher's. These tools never bypass
the journal's transactions: every change is one character-door update.

===========================================================================
*/
package quest

import (
	"fmt"
	"slices"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
gmDefinition

The codename as typed, then upper-cased: authored codenames are upper case
and the client sends the line's own case.
================
*/
func (rt *Runtime) gmDefinition(codename string) (*Definition, error) {
	if rt == nil || rt.Defs == nil {
		return nil, fmt.Errorf("quest gm: no definitions")
	}
	if def, ok := rt.Defs.ByCodename(codename); ok {
		return def, nil
	}
	if def, ok := rt.Defs.ByCodename(strings.ToUpper(codename)); ok {
		return def, nil
	}
	return nil, fmt.Errorf("quest gm: %q has no loaded definition", codename)
}

/*
================
forgetQuestHistory

Erases what remembers q as taken: the completed list, the completion count,
the ended mark and the capture supply stamp. Callers hold the character door.
================
*/
func forgetQuestHistory(c *enterworld.Character, id uint32) bool {
	changed := false
	if questCompleted(c, id) {
		c.CompletedQuestIds = slices.DeleteFunc(slices.Clone(c.CompletedQuestIds), func(v uint32) bool { return v == id })
		changed = true
	}
	if questEnded(c, id) {
		c.EndedQuestIds = slices.DeleteFunc(slices.Clone(c.EndedQuestIds), func(v uint32) bool { return v == id })
		changed = true
	}
	if _, ok := c.QuestCompletionCounts[id]; ok {
		delete(c.QuestCompletionCounts, id)
		changed = true
	}
	if _, ok := c.QuestSupplies[id]; ok {
		delete(c.QuestSupplies, id)
		changed = true
	}
	return changed
}

/*
================
GMRemoveQuest

/REMOVEQ: the give-up of 0x71EB without its client-side limits (a reward-kind
quest and a paid tutorial stage can be dropped by a GM), and without history.
================
*/
func (rt *Runtime) GMRemoveQuest(c *enterworld.Character, codename string) (OpResult, error) {
	return rt.gmForget(c, codename, false)
}

/*
================
GMInitQuest

/INITQ: /REMOVEQ plus the quest's history, so the NPC offers it again.
================
*/
func (rt *Runtime) GMInitQuest(c *enterworld.Character, codename string) (OpResult, error) {
	return rt.gmForget(c, codename, true)
}

/*
================
gmForget

One transaction: abandon the active record when there is one, then optionally
forget the history. /REMOVEQ of a quest that is not active is a refusal; an
/INITQ with nothing to forget still succeeds, as the state it asks for holds.
================
*/
func (rt *Runtime) gmForget(c *enterworld.Character, codename string, history bool) (OpResult, error) {
	if c == nil {
		return OpResult{}, fmt.Errorf("quest gm: nil character")
	}
	def, err := rt.gmDefinition(codename)
	if err != nil {
		return OpResult{}, err
	}
	var refusal error
	var frames []wire.Frame
	abandoned, ran := false, false
	changed := rt.deps.Update(c, "gm-quest-forget", func() bool {
		ran = true
		if c.DeletePending {
			refusal = fmt.Errorf("quest gm: character is pending deletion")
			return false
		}
		dirty := false
		if at := activeQuestIndex(c, def.RefID); at >= 0 {
			inventoryFrames, err := rt.abandonActiveLocked(c, def, at)
			if err != nil {
				refusal = err
				return false
			}
			frames = append(inventoryFrames, wire.Frame{Opcode: OpQuestUpdate, Payload: EncodeQuestUpdateAbandon(def.RefID)})
			abandoned, dirty = true, true
		} else if !history {
			refusal = fmt.Errorf("quest gm: %s is not active", def.Codename)
			return false
		}
		if history && forgetQuestHistory(c, def.RefID) {
			dirty = true
		}
		return dirty
	})
	if refusal != nil {
		return OpResult{}, refusal
	}
	if !changed && !ran {
		return OpResult{}, fmt.Errorf("quest gm: character is no longer authoritative")
	}
	if abandoned && def.Codename == ivyMaterialQuest {
		rt.ForgetItemUse(c)
	}
	return OpResult{Frames: frames}, nil
}

/*
================
GMResetQuests

/RESETQ: every active quest is abandoned with its items, then every history
list is emptied. The cleanup of every record is planned before the first
change, so one that cannot be planned refuses the whole reset.
================
*/
func (rt *Runtime) GMResetQuests(c *enterworld.Character) (OpResult, error) {
	if c == nil || rt == nil || rt.Defs == nil {
		return OpResult{}, fmt.Errorf("quest gm: nil character")
	}
	var refusal error
	var frames []wire.Frame
	forgotten := false
	changed := rt.deps.Update(c, "gm-quest-reset", func() bool {
		if c.DeletePending {
			refusal = fmt.Errorf("quest gm: character is pending deletion")
			return false
		}
		for _, record := range c.ActiveQuests {
			if def, ok := rt.Defs.ByRefID(record.RefID); ok {
				if _, _, err := rt.planQuestCleanup(c, def); err != nil {
					refusal = err
					return false
				}
			}
		}
		for len(c.ActiveQuests) > 0 {
			refID := c.ActiveQuests[0].RefID
			if def, ok := rt.Defs.ByRefID(refID); ok {
				inventoryFrames, err := rt.abandonActiveLocked(c, def, 0)
				if err != nil {
					refusal = err
					return len(frames) > 0
				}
				frames = append(frames, inventoryFrames...)
				if def.Codename == ivyMaterialQuest {
					forgotten = true
				}
			} else {
				c.ActiveQuests = slices.Clone(c.ActiveQuests[1:])
			}
			frames = append(frames, wire.Frame{Opcode: OpQuestUpdate, Payload: EncodeQuestUpdateAbandon(refID)})
		}
		c.CompletedQuestIds, c.EndedQuestIds = nil, nil
		c.QuestCompletionCounts, c.QuestSupplies = nil, nil
		return true
	})
	if forgotten {
		rt.ForgetItemUse(c)
	}
	if refusal != nil {
		return OpResult{Frames: frames}, refusal
	}
	if !changed {
		return OpResult{}, fmt.Errorf("quest gm: character is no longer authoritative")
	}
	return OpResult{Frames: frames}, nil
}

/*
================
GMCompleteQuest

/COMPQ: settles the active quest through settleReward with force, one stage
per pass for a staged quest, so the rewards, experience, history and the next
stage's record are the ordinary ones. A refusal after some stages keeps them:
the frames of what was committed come back with the error.
================
*/
func (rt *Runtime) GMCompleteQuest(c *enterworld.Character, codename string) (OpResult, error) {
	if c == nil {
		return OpResult{}, fmt.Errorf("quest gm: nil character")
	}
	def, err := rt.gmDefinition(codename)
	if err != nil {
		return OpResult{}, err
	}
	var out OpResult
	for pass := 0; pass <= len(def.Stages); pass++ {
		stage, active := rt.gmActiveStage(c, def)
		if !active {
			if pass == 0 {
				return OpResult{}, fmt.Errorf("quest gm: %s is not active", def.Codename)
			}
			return out, nil
		}
		var expected *uint16
		npc := ""
		if len(def.Stages) > 0 {
			current, ok := definitionAtStage(def, stage)
			if !ok {
				return out, fmt.Errorf("quest gm: %s has no stage %d", def.Codename, stage)
			}
			expected, npc = &stage, current.EndNpcCodename
		}
		result, err := rt.settleReward(c, def, expected, npc, noRewardChoice, true)
		out.Frames = append(out.Frames, result.Frames...)
		out.Broadcast = append(out.Broadcast, result.Broadcast...)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

/*
================
gmActiveStage

Reads the active record's stage under the character door. An Update that
returns false commits nothing; it is the quest package's read door.
================
*/
func (rt *Runtime) gmActiveStage(c *enterworld.Character, def *Definition) (stage uint16, active bool) {
	rt.deps.Update(c, "gm-quest-stage", func() bool {
		if at := activeQuestIndex(c, def.RefID); at >= 0 {
			stage, active = c.ActiveQuests[at].Stage, true
		}
		return false
	})
	return stage, active
}

/*
================
firstOfferedChoice

The reward choice /COMPQ takes when the quest offers several: the first one
the character may pick.
================
*/
func firstOfferedChoice(def *Definition, country int) int {
	for i := range def.RewardChoices {
		if rewardChoiceOffered(def, country, i) {
			return i
		}
	}
	return noRewardChoice
}

/*
================
clampToHeld

Limits each consumed item to what the bag holds, so a forced completion takes
the objective items that are there and not the ones that are not.
================
*/
func clampToHeld(c *enterworld.Character, consume []inventory.ItemAmount) []inventory.ItemAmount {
	out := make([]inventory.ItemAmount, 0, len(consume))
	for _, item := range consume {
		item.Count = min(item.Count, captureItemCount(c, item.Codename))
		if item.Count > 0 {
			out = append(out, item)
		}
	}
	return out
}
