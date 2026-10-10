/*
===========================================================================

skillcooldown.go - remaining authoritative reuse deadlines at world entry

Browser entry restores the existing server cooldown manager rather than
starting new casts or trusting a browser cache. Match action/skillcost.go:
a nonzero shared group exclusively owns reuse, otherwise the skill group.

===========================================================================
*/
package enterworld

import "sort"

/*
================
EntrySkillCooldown

Remaining time is relative to entry creation, independent of the browser's
wall clock. Duration owns the sweep; the remaining deadline owns admission.
================
*/
type EntrySkillCooldown struct {
	Skill       uint32 `json:"skill"`
	RemainingMs uint32 `json:"remainingMs"`
	DurationMs  uint32 `json:"durationMs"`
}

/*
================
entrySkillCooldowns

Read the detached character used for the rest of the entry projection.
No cooldown is cleared or extended by reconnecting. The browser receives
only current learned references, so an upgrade restores its family's timer.
================
*/
func entrySkillCooldowns(deps *Deps, character *Character) []EntrySkillCooldown {
	rows := []EntrySkillCooldown{}
	if deps.Skills == nil {
		return rows
	}
	now := deps.clock().UnixMilli()
	seen := map[uint32]bool{}
	for _, id := range character.Skills {
		skill, found := deps.Skills.SkillByID(id)
		if !found || !skillHasUI(skill) || skill.CoolTimeMs == 0 || seen[id] {
			continue
		}
		seen[id] = true
		until := character.OffensiveSkillCooldowns[skill.Group]
		if skill.CoolTimeGroup != 0 {
			until = character.SharedSkillCooldowns[skill.CoolTimeGroup]
		}
		if until <= now {
			continue
		}
		remaining := until - now
		if remaining > int64(^uint32(0)) {
			continue
		}
		rows = append(rows, EntrySkillCooldown{Skill: id, RemainingMs: uint32(remaining), DurationMs: skill.CoolTimeMs})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Skill < rows[j].Skill })
	return rows
}
