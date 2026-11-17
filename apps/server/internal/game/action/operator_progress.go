/*
===========================================================================

operator_progress.go - the operator's level and skill point corrections

Port-only, not native: the original ships no tool that sets a character's
level or SP. Like the stat reset it is staff tooling behind the operator
credential (the Agent's set-level and set-sp player operations), so it
carries no SRO_ flag; the composition root runs both under the transport
control lease, so the character is offline while they commit.

The level is never written by hand. The correction converts the target
into the EXP between the character's position on the curve and the start
of the target level, and walks it through the stat authority's unscaled
experience door (RefundExperience), the same walk a kill or a quest uses:
a raise grants the per-level STR, INT and stat points and moves the
MaxLevel watermark; a lowering keeps them, as the native level-down does.

===========================================================================
*/
package action

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
)

// operatorMaxSkillPoints is the positive signed range persistence and the
// char-data block share for SP (progression.clampSkillPoints).
const operatorMaxSkillPoints int64 = 0x7fffffff

// operatorExpStep is the largest delta one experience walk applies
// (progression.clampExpDelta's signed 32-bit ceiling); a long jump walks
// in steps of it.
const operatorExpStep int64 = 0x7fffffff

// operatorMaxExpSteps bounds the walk: level 1 to 140 of the v1.150 curve
// is far below this many steps, so running out means the walk is stuck.
const operatorMaxExpSteps = 4096

/*
================
operatorLevelDelta

The EXP that moves a character at (level, exp) to the first point of
target: positive to raise, negative to lower, -exp to stay on the level.
False when the curve has no row for a level on the way.
================
*/
func operatorLevelDelta(levels enterworld.LevelDataSource, level, exp, target int64) (int64, bool) {
	delta := -exp
	for l := level; l < target; l++ {
		required, ok := levels.ExpRequired(l)
		if !ok {
			return 0, false
		}
		delta += required
	}
	for l := target; l < level; l++ {
		required, ok := levels.ExpRequired(l)
		if !ok {
			return 0, false
		}
		delta -= required
	}
	return delta, true
}

/*
================
OperatorSetLevel

Moves the named character to the start of level (zero EXP into it).
levelCap is the shard's configured cap (progression.LevelCapFromEnv): a
target above it is refused, never clamped.
================
*/
func (rt *Runtime) OperatorSetLevel(division, name string, level, levelCap int64) error {
	if level < 1 || level > levelCap {
		return fmt.Errorf("level must be between 1 and %d", levelCap)
	}
	if rt.RefundExperience == nil || rt.deps.LevelData() == nil {
		return fmt.Errorf("level correction is not wired on this shard")
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		return fmt.Errorf("character not found")
	}
	var walkErr error
	rt.deps.Update(c, "operator-set-level", func() bool {
		if c.DeletePending {
			walkErr = fmt.Errorf("character is pending deletion")
			return false
		}
		changed := false
		for range operatorMaxExpSteps {
			current, exp := operatorCharacterLevel(c), operatorCharacterExp(c)
			delta, ok := operatorLevelDelta(rt.deps.LevelData(), current, exp, level)
			if !ok {
				walkErr = fmt.Errorf("the level curve has no row between %d and %d", current, level)
				return changed
			}
			if delta == 0 {
				return changed
			}
			step := min(max(delta, -operatorExpStep-1), operatorExpStep)
			if _, moved := rt.RefundExperience(c, step); !moved {
				walkErr = fmt.Errorf("the experience walk stopped at level %d", current)
				return changed
			}
			changed = true
		}
		walkErr = fmt.Errorf("the experience walk did not reach level %d", level)
		return changed
	})
	// A lowering walks as an EXP loss, which a resurrection would partly
	// give back; the correction is no death.
	c.LastExpLoss = 0
	if walkErr != nil {
		return walkErr
	}
	if got := operatorCharacterLevel(c); got != level {
		return fmt.Errorf("level correction ended at %d", got)
	}
	return nil
}

/*
================
OperatorSetSkillPoints

Sets the named character's free SP to an absolute value.
================
*/
func (rt *Runtime) OperatorSetSkillPoints(division, name string, sp int64) error {
	if sp < 0 || sp > operatorMaxSkillPoints {
		return fmt.Errorf("SP must be between 0 and %d", operatorMaxSkillPoints)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		return fmt.Errorf("character not found")
	}
	if !rt.deps.Update(c, "operator-set-sp", func() bool {
		if c.DeletePending {
			return false
		}
		value := sp
		c.SkillPoints = &value
		return true
	}) {
		return fmt.Errorf("SP correction refused")
	}
	return nil
}

/*
================
operatorCharacterLevel

An absent or invalid stored level reads as level 1, the progression rule.
================
*/
func operatorCharacterLevel(c *enterworld.Character) int64 {
	if c.Level == nil || *c.Level < 1 {
		return 1
	}
	return *c.Level
}

/*
================
operatorCharacterExp
================
*/
func operatorCharacterExp(c *enterworld.Character) int64 {
	if c.Experience == nil || *c.Experience < 0 {
		return 0
	}
	return *c.Experience
}
