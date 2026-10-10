/*
===========================================================================

skills.go - C2 skills.json: every player skill rank and its masteries

The rows are the server's skill references (enterworld.TextdataSkills):
each rank of a Chinese or European player skill line that a player trains
(chain stages are part of their root). The status is the server's own
press router (action.SkillPressHandled): a rank no owner runs does nothing
when pressed, so it is missing. A passive is never pressed; it is
implemented when its execution plan is a passive one (applied on learn). Names, icons,
the mastery list and the tooltip lines come from the client's skill
window data and are filled by the Node stage.

===========================================================================
*/
package catalogue

import (
	"sort"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// skillIssue is where skill gaps are tracked.
const skillIssue = "https://github.com/opensro-dev/opensro/issues?q=is%3Aissue+label%3Askills"

/*
================
SkillRequirement

A mastery or skill group the rank needs, at this level.
================
*/
type SkillRequirement struct {
	ID    uint32 `json:"id"`
	Level int64  `json:"level"`
}

/*
================
Skill
================
*/
type Skill struct {
	ID       uint32 `json:"id"`
	Codename string `json:"codename"`
	Name     string `json:"name"`
	// Mastery is the native mastery id (257 blade, 513 warrior, ...).
	Mastery uint32 `json:"mastery"`
	Group   uint32 `json:"group"`
	Level   int64  `json:"level"`
	Race    string `json:"race" enum:"race"`
	Icon    string `json:"icon,omitempty"`
	SPCost  int64  `json:"spCost"`
	// Masteries are the mastery levels the rank needs; Prerequisites the
	// skill groups (and their levels) it needs learned first.
	Masteries     []SkillRequirement `json:"masteries"`
	Prerequisites []SkillRequirement `json:"prerequisites"`
	Tooltip       []TooltipRow       `json:"tooltip"`
	Standing
}

/*
================
SkillMastery

One mastery as the skill window lists it; filled by the Node stage.
================
*/
type SkillMastery struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
	Race string `json:"race" enum:"race"`
	Icon string `json:"icon,omitempty"`
}

/*
================
SkillsFile
================
*/
type SkillsFile struct {
	Header
	Masteries []SkillMastery `json:"masteries"`
	Skills    []Skill        `json:"skills"`
}

/*
================
SkillSource

The skill rows, the server's press router and its execution plans.
================
*/
type SkillSource struct {
	Rows    []enterworld.SkillRow
	Handled func(enterworld.SkillRow) bool
	Plan    func(id uint32) enterworld.SkillExecutionKind
}

/*
================
BuildSkills

Every trainable player skill rank, in id order.
================
*/
func BuildSkills(source SkillSource) []Skill {
	skills := []Skill{}
	for _, row := range source.Rows {
		if row.ChainSub || !playerSkill(row.Codename) {
			continue
		}
		s := Skill{ID: row.ID, Codename: row.Codename, Name: row.Codename, Mastery: row.Masteries[0].ID, Group: row.Group,
			Level: row.Level, Race: itemRace(int64(row.RequiredRace)), Icon: webIcon(row.Icon), SPCost: row.SPCost,
			Masteries: requirements(row.Masteries[:]), Prerequisites: requirements(row.Prerequisites[:]), Tooltip: []TooltipRow{}}
		s.Standing = skillStanding(source.Handled(row) || source.Plan(row.ID) == enterworld.SkillExecutionPassive)
		s.Standing = applyGaps("skill", row.ID, s.Standing)
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].ID < skills[j].ID })
	return skills
}

/*
================
playerSkill

A Chinese or European player skill line (the server's skill catalogue
draws the same line).
================
*/
func playerSkill(codename string) bool {
	return strings.HasPrefix(codename, "SKILL_CH_") || strings.HasPrefix(codename, "SKILL_EU_")
}

/*
================
skillStanding
================
*/
func skillStanding(handled bool) Standing {
	if !handled {
		return Standing{Status: StatusMissing, Gaps: []string{"Using this skill does nothing yet."}, Issue: skillIssue}
	}
	return Standing{Status: StatusImplemented}
}

/*
================
requirements
================
*/
func requirements(slots []enterworld.SkillRequirement) []SkillRequirement {
	out := []SkillRequirement{}
	for _, slot := range slots {
		if slot.ID != 0 {
			out = append(out, SkillRequirement{ID: slot.ID, Level: slot.Level})
		}
	}
	return out
}

/*
================
SkillStandings
================
*/
func SkillStandings(skills []Skill) []Standing {
	out := make([]Standing, len(skills))
	for i, s := range skills {
		out[i] = s.Standing
	}
	return out
}
