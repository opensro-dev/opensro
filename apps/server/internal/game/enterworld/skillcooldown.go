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
	return RemainingSkillCooldowns(deps.Skills, character, deps.clock().UnixMilli())
}

/*
================
RemainingSkillCooldowns - shared entry and checkpoint reuse projection.
================
*/
func RemainingSkillCooldowns(skills SkillDataSource, character *Character, now int64) []EntrySkillCooldown {
	rows := []EntrySkillCooldown{}
	if skills == nil {
		return rows
	}
	seen := map[uint32]bool{}
	for _, id := range character.Skills {
		skill, found := skills.SkillByID(id)
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

/*
================
EntryItemCooldown

An item cooldown group still running at entry (an Energy of Life scroll, a
premium item). Remaining time is relative to entry creation; the browser
reads the sweep's length from the group's authored duration.
================
*/
type EntryItemCooldown struct {
	Group       uint32 `json:"group"`
	RemainingMs uint32 `json:"remainingMs"`
}

/*
================
entryItemCooldowns

The character's item cooldown groups that have not ended. A teleport or a
reconnect re-enters the world and must not forget them: the server keeps
refusing the item ("still have time to reuse"), so the icon must keep
showing it.
================
*/
func entryItemCooldowns(deps *Deps, character *Character) []EntryItemCooldown {
	now := deps.clock().UnixMilli()
	rows := []EntryItemCooldown{}
	for group, until := range character.ItemGroupCooldowns {
		remaining := until - now
		if remaining <= 0 || remaining > int64(^uint32(0)) {
			continue
		}
		rows = append(rows, EntryItemCooldown{Group: group, RemainingMs: uint32(remaining)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Group < rows[j].Group })
	return rows
}
