/*
===========================================================================

deathpenalty_test.go - the native death penalty arithmetic

4E6980's float32 rate constants, the murderer's SP loss, the 0x101 share
a premium keeps, and the job death of 4E6820.

===========================================================================
*/

package progression

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/pk"
)

/*
================
deathLevels

Level 20 needs 1000 EXP (a multiple of 50: trunc(1000 * 0.02f) is 19),
basis 30, gold minimum 40, trader grade 1 needs 500.
================
*/
type deathLevels struct{ staticLevels }

func (deathLevels) ExpRequired(level int64) (int64, bool) { return 1000, level == 20 }
func (deathLevels) MonsterExpBasis(level int64) (int64, bool) {
	return 30, level > 0
}
func (deathLevels) WithdrawalGoldBasis(level int64) (int64, bool) { return 40, level == 20 }
func (deathLevels) JobExpRequired(grade int64, job uint8) (int64, bool) {
	return 500, grade == 1 && job == 1
}

/*
================
TestDeathLossUsesTheFloatConstant
================
*/
func TestDeathLossUsesTheFloatConstant(t *testing.T) {
	ordinary := OrdinaryDeathPenalty()
	if loss, ok := deathPenaltyLoss(deathLevels{}, 20, ordinary); !ok || loss != 19 {
		t.Fatalf("loss = %d %v, want trunc(1000 * 0.02f) = 19", loss, ok)
	}
	// The cap (30 * 100) does not bind; a 50 percent premium keeps 9.
	kept := ordinary
	kept.ReductionPercent = 50
	if loss, _ := deathPenaltyLoss(deathLevels{}, 20, kept); loss != 19-9 {
		t.Fatalf("kept loss = %d, want 10", loss)
	}
	// A player's kill of an ordinary victim: 0.4 percent, cap 30 * 20.
	rule, _ := pk.DeathLoss(pk.DeathPlayer, 20, 0, true, false)
	if loss, _ := deathPenaltyLoss(deathLevels{}, 20, pk.DeathPenalty{Rule: rule}); loss != 4 {
		t.Fatalf("player-kill loss = %d, want trunc(1000 * 0.004f) = 4", loss)
	}
}

/*
================
TestMurdererLosesSkillPoints
================
*/
func TestMurdererLosesSkillPoints(t *testing.T) {
	character := levelupTestCharacter()
	character.Level = int64Ptr(11)
	character.MaxLevel = int64Ptr(11)
	character.Experience = int64Ptr(30000)
	character.SkillPoints = int64Ptr(45)
	rt := newTestRuntime(character)
	rule, _ := pk.DeathLoss(pk.DeathMonster, 11, 500, false, false)
	result := rt.ApplyDeathPenalty(character, pk.DeathPenalty{Rule: rule})
	if *character.SkillPoints != 0 {
		t.Fatalf("SP = %d, want 45 - min(60, 45) = 0", *character.SkillPoints)
	}
	if len(result.Frames) == 0 {
		t.Fatal("no frames for the murderer's death")
	}
}

/*
================
TestJobDeathTakesJobExperience

4E6820: job EXP trunc(40 * 10 * 0.125) * 3 = 150, then EXP basis 30 * 6.
================
*/
func TestJobDeathTakesJobExperience(t *testing.T) {
	character := levelupTestCharacter()
	character.Level = int64Ptr(20)
	character.MaxLevel = int64Ptr(20)
	character.Experience = int64Ptr(900)
	character.Job.Type = 1
	character.Job.Grade = 1
	character.Job.Exp = 400
	rt := newTestRuntime(character)
	rt.deps.(*enterworld.Deps).Levels = deathLevels{}
	rt.ApplyDeathPenalty(character, pk.DeathPenalty{Job: true, KillerLevel: 18})
	if character.Job.Exp != 250 {
		t.Fatalf("job exp = %d, want 250", character.Job.Exp)
	}
	if *character.Experience != 900-180 {
		t.Fatalf("exp = %d, want 720", *character.Experience)
	}
}

/*
================
TestJobExpGradesUpOnce
================
*/
func TestJobExpGradesUpOnce(t *testing.T) {
	c := &enterworld.Character{}
	c.Job.Type, c.Job.Grade, c.Job.Exp = 1, 1, 400
	frames, ok := AddJobExp(c, deathLevels{}, 300)
	if !ok || c.Job.Grade != 2 || c.Job.Exp != 200 || len(frames) != 1 || frames[0].Opcode != OpJobExpUpdate {
		t.Fatalf("job = %+v frames %v", c.Job, frames)
	}
}

/*
================
TestJobExpMovesThiefAndHunterContribution

60DD90 -> 60E0A0: a thief's or hunter's job EXP delta moves the week's
contribution, floored at 0 and capped at 2,000,000,000; a trader's does
not (traders contribute on sales).
================
*/
func TestJobExpMovesThiefAndHunterContribution(t *testing.T) {
	hunter := &enterworld.Character{}
	hunter.Job.Type, hunter.Job.Grade, hunter.Job.Exp, hunter.Job.WeeklyReward = 3, 1, 100, 10
	AddJobExp(hunter, deathLevels{}, 50)
	if hunter.Job.WeeklyReward != 60 {
		t.Fatalf("hunter contribution = %d, want 60", hunter.Job.WeeklyReward)
	}
	AddJobExp(hunter, deathLevels{}, -90)
	if hunter.Job.WeeklyReward != 0 {
		t.Fatalf("a loss left contribution %d, want 0", hunter.Job.WeeklyReward)
	}
	hunter.Job.WeeklyReward = 1999999990
	AddJobExp(hunter, deathLevels{}, 50)
	if hunter.Job.WeeklyReward != 2000000000 {
		t.Fatalf("contribution %d passed the cap", hunter.Job.WeeklyReward)
	}
	trader := &enterworld.Character{}
	trader.Job.Type, trader.Job.Grade, trader.Job.Exp = 1, 1, 100
	AddJobExp(trader, deathLevels{}, 50)
	if trader.Job.WeeklyReward != 0 {
		t.Fatalf("a trader's job EXP contributed %d", trader.Job.WeeklyReward)
	}
}
