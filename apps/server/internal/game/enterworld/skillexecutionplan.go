/*
===========================================================================

skillexecutionplan.go - immutable catalog routes for complete skill programs

Compilers admit complete producers. Runtime state stays with action owners;
recognizing an instruction alone never enables a route.

===========================================================================
*/

package enterworld

/*
================
SkillExecutionKind

An executable authority route, not an individual effect opcode.
================
*/
type SkillExecutionKind uint8

const (
	SkillExecutionUnsupported SkillExecutionKind = iota
	SkillExecutionOffense
	SkillExecutionRecovery
	SkillExecutionInstantEffect
	SkillExecutionPassive
	SkillExecutionTimedEffect
	SkillExecutionPosition
	SkillExecutionThreat
)

/*
================
SkillExecutionPlan

Immutable stage descriptors backed by resident catalog storage.
================
*/
type SkillExecutionPlan struct {
	source *skillStorage
	ids    []uint32
	kind   SkillExecutionKind
	stages []residentSkill
}

/*
================
Kind
================
*/
func (p SkillExecutionPlan) Kind() SkillExecutionKind { return p.kind }

/*
================
Len
================
*/
func (p SkillExecutionPlan) Len() int {
	if p.source != nil {
		return len(p.ids)
	}
	return len(p.stages)
}

/*
================
Stage

Return a detached row so consumers cannot mutate the published catalog.
================
*/
func (p SkillExecutionPlan) Stage(i int) SkillRow {
	if p.source != nil {
		return p.source.get(p.ids[i])
	}
	return p.stages[i].value()
}

/*
================
skillPlanRows

Temporary graph lookup used while building immutable catalog plans.
================
*/
type skillPlanRows map[uint32]SkillRow

/*
================
SkillByID
================
*/
func (s skillPlanRows) SkillByID(id uint32) (SkillRow, bool) { r, ok := s[id]; return r, ok }

/*
================
SkillByCodename
================
*/
func (s skillPlanRows) SkillByCodename(name string) (SkillRow, bool) {
	for _, r := range s {
		if r.Codename == name {
			return r, true
		}
	}
	return SkillRow{}, false
}

/*
================
compileExecutionPlan

Admit the whole offensive graph before considering independent self routes.
================
*/
func compileExecutionPlan(source SkillDataSource, root SkillRow) SkillExecutionPlan {
	if root.ChainSub {
		return SkillExecutionPlan{}
	}
	if stages, ok := validateOffensiveSequence(source, root.ID); ok {
		return SkillExecutionPlan{kind: SkillExecutionOffense, stages: compactSkillStages(stages)}
	}
	// Existing self/passive compilers reject linked programs. Preserve that
	// complete-program restriction until a mixed-stage authority is verified.
	if root.ChainNext != 0 {
		return SkillExecutionPlan{}
	}
	kind := SkillExecutionUnsupported
	switch {
	case root.Threat.Only:
		kind = SkillExecutionThreat
	case root.PositionEffect.Pinned:
		kind = SkillExecutionPosition
	case root.Recovery.SelfFlatPinned:
		kind = SkillExecutionRecovery
	case root.InstantSelfEffectPinned || root.Imbue.Pinned:
		kind = SkillExecutionInstantEffect
	case root.TimedEffect.Pinned && !root.TimedEffect.Persistent:
		kind = SkillExecutionTimedEffect
	case root.PassiveParameters.Pinned || root.PassiveCritical.Pinned || root.PassiveDefense.Pinned:
		kind = SkillExecutionPassive
	}
	if kind == SkillExecutionUnsupported {
		return SkillExecutionPlan{}
	}
	return SkillExecutionPlan{kind: kind, stages: []residentSkill{compactSkill(root)}}
}

/*
================
ExecutionPlan

Publish only after the catalog's one-time load completes.
================
*/
func (t *TextdataSkills) ExecutionPlan(id uint32) SkillExecutionPlan {
	t.once.Do(t.load)
	return t.plans[id]
}

/*
================
compactSkillStages

Intern validated stages without retaining mutable parser rows.
================
*/
func compactSkillStages(rows []SkillRow) []residentSkill {
	out := make([]residentSkill, len(rows))
	for i, row := range rows {
		out[i] = compactSkill(row)
	}
	return out
}
