/*
===========================================================================

sidetalk.go - lines other NPCs speak while a quest runs

A side talk is a dialogue mission that never completes the quest (Rahid 4's
slaves, CQNO_RM_OLDWOMAN_4_DialogMissionTalk 89FDA0). Its first line is
spoken once and recorded as heard; afterwards the NPC answers its repeat
line. A side talk without a repeat line speaks the same line every time.

===========================================================================
*/
package quest

import (
	"fmt"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// sideTalkPrefix marks the dialogue token that records a side talk heard.
const sideTalkPrefix = "side-talk:"

/*
================
sideTalkToken
================
*/
func sideTalkToken(code string, index int) string {
	return sideTalkPrefix + code + branchSeparator + strconv.Itoa(index)
}

/*
================
sideTalkOption

The row npc offers for an active def, if it has a side talk there: the
pending line, which AdvanceNpcQuest records as heard, or the repeat line.
================
*/
func sideTalkOption(def *Definition, record enterworld.ActiveQuestRecord, npc string) (NpcOption, bool) {
	for i, side := range def.SideTalks {
		if side.NpcCodename != npc {
			continue
		}
		row := NpcOption{Codename: def.Codename, TitleSymbol: def.TitleSymbol, PromptSymbol: side.PromptSymbol, Informational: true}
		if side.RepeatSymbol == "" {
			return row, true
		}
		if record.SideTalksHeard&(1<<i) != 0 {
			row.PromptSymbol = side.RepeatSymbol
			return row, true
		}
		row.Codename, row.Informational, row.SideTalk = sideTalkToken(def.Codename, i), false, true
		return row, true
	}
	return NpcOption{}, false
}

/*
================
hearSideTalk

89FDA0's next step: clear the mission's pending bit and publish the record.
Only the side talk's own NPC records it, once, while the quest is active.
================
*/
func (rt *Runtime) hearSideTalk(c *enterworld.Character, token, npc string) (OpResult, error) {
	code, index, ok := parseBranchToken(strings.TrimPrefix(token, sideTalkPrefix))
	def, known := rt.Defs.ByCodename(code)
	if !ok || !known || index >= len(def.SideTalks) || index >= 8 || def.SideTalks[index].RepeatSymbol == "" ||
		def.SideTalks[index].NpcCodename != npc {
		return OpResult{}, fmt.Errorf("quest side talk %s refused at %s", token, npc)
	}
	var refusal error
	changed := rt.deps.Update(c, "quest-side-talk", func() bool {
		at := activeQuestIndex(c, def.RefID)
		if c == nil || c.DeletePending || at < 0 {
			refusal = fmt.Errorf("quest %s is not active", code)
			return false
		}
		if c.ActiveQuests[at].SideTalksHeard&(1<<index) != 0 {
			refusal = fmt.Errorf("quest side talk %s already heard", token)
			return false
		}
		c.ActiveQuests[at].SideTalksHeard |= 1 << index
		return true
	})
	if refusal != nil {
		return OpResult{}, refusal
	}
	if !changed {
		return OpResult{}, fmt.Errorf("quest side talk %s was not recorded", token)
	}
	return OpResult{}, nil
}
