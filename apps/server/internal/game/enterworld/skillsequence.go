/*
===========================================================================

skillsequence.go - admission of complete offensive skill graphs

Column nine links skill records, not impact rows. Every stage must be
executable before the root can own a resource debit or a cast token.

===========================================================================
*/

package enterworld

const maximumSkillStages = 32

/*
================
OffensiveSequence

Use the immutable catalog plan when available; synthetic sources pass through
the same graph validator instead of bypassing its completeness checks.
================
*/
func OffensiveSequence(source SkillDataSource, rootID uint32) ([]SkillRow, bool) {
	if compiled, ok := source.(interface {
		ExecutionPlan(uint32) SkillExecutionPlan
	}); ok {
		plan := compiled.ExecutionPlan(rootID)
		if plan.Kind() != SkillExecutionOffense {
			return nil, false
		}
		rows := make([]SkillRow, plan.Len())
		for i := range rows {
			rows[i] = plan.Stage(i)
		}
		return rows, true
	}
	return validateOffensiveSequence(source, rootID)
}

/*
================
validateOffensiveSequence

Missing links, cycles, foreign groups and unsupported effects refuse the whole
cast. Guided charges have an arrival-owned lifetime rather than a timed close.
================
*/
func validateOffensiveSequence(source SkillDataSource, rootID uint32) ([]SkillRow, bool) {
	if source == nil {
		return nil, false
	}
	root, ok := source.SkillByID(rootID)
	if !ok || root.ChainSub {
		return nil, false
	}
	seen := map[uint32]bool{}
	var sequence []SkillRow
	row := root
	for len(sequence) < maximumSkillStages {
		if seen[row.ID] || row.Group != root.Group || row.Level != root.Level ||
			(!row.OffensiveStagePinned && !(row.DirectOffensePinned && row.ChainNext == 0)) {
			return nil, false
		}
		lifetime, valid := row.ActionLifecycleMs()
		if !valid || lifetime == 0 && !row.PositionEffect.Charge {
			return nil, false
		}
		// A linked stage is charged by its root alone. Dare Devil and Crutial
		// Rush (TWOHANDA_CRY_B, DUALA_WHIRLWIND_B) author their root's HP
		// ratio again on the second stage; the owner's rule is that the HP is
		// consumed once, at the start, so only that repetition is tolerated.
		if len(sequence) > 0 && (!row.ChainSub || row.Consumption.HP != 0 || row.Consumption.MP != 0 ||
			row.Consumption.HPPercent != 0 && row.Consumption.HPPercent != root.Consumption.HPPercent ||
			row.Consumption.MPPercent != 0 || !row.Consumption.Pinned) {
			return nil, false
		}
		seen[row.ID] = true
		sequence = append(sequence, row)
		if row.ChainNext == 0 {
			return sequence, true
		}
		row, ok = source.SkillByID(row.ChainNext)
		if !ok {
			return nil, false
		}
	}
	return nil, false
}
