/*
===========================================================================

styria_reward_test.go - Styria Clash rewards: the table, the paying

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/styria"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestStyriaRewardSkillExpBands

The banded skill exp at every band edge, as the later client's
table authors it (110's authored spike included).
================
*/
func TestStyriaRewardSkillExpBands(t *testing.T) {
	edges := []struct {
		level int
		want  int64
	}{
		{1, 160}, {9, 160}, {10, 380}, {19, 380}, {20, 670}, {55, 2500},
		{99, 8320}, {100, 11040}, {109, 11040}, {110, 150800}, {140, 150800},
	}
	for _, edge := range edges {
		if got := styria.RewardSkillExp(edge.level); got != edge.want {
			t.Fatalf("level %d skill exp = %d, want %d", edge.level, got, edge.want)
		}
	}
}

/*
================
TestStyriaRewardForResults

Job EXP splits by result while the banded skill exp does not; the
stones follow 5/1/2, and the rune seals ride a win only.
================
*/
func TestStyriaRewardForResults(t *testing.T) {
	jobExp, skillExp, items := styriaRewardFor(styriaResultWin, 20)
	if jobExp != 20 || skillExp != 670 {
		t.Fatalf("win exps = %d/%d, want 20 job and 670 skill", jobExp, skillExp)
	}
	if len(items) != 4 || items[0].Count != 1 || items[2].Count != 5 || items[3].Count != 5 {
		t.Fatalf("win items: %+v", items)
	}
	jobExp, skillExp, items = styriaRewardFor(styriaResultLose, 20)
	if jobExp != 3 || skillExp != 670 || len(items) != 2 || items[0].Count != 1 {
		t.Fatalf("lose reward: %d/%d %+v", jobExp, skillExp, items)
	}
	jobExp, _, items = styriaRewardFor(styriaResultDraw, 110)
	if jobExp != 6 || len(items) != 2 || items[0].Count != 2 {
		t.Fatalf("draw reward: %d %+v", jobExp, items)
	}
	if styriaResultFor(styriaOutcomeHunters, styriaSideHunters) != styriaResultWin ||
		styriaResultFor(styriaOutcomeHunters, styriaSideThieves) != styriaResultLose ||
		styriaResultFor(styriaOutcomeDraw, styriaSideThieves) != styriaResultDraw {
		t.Fatal("the per-side result misread the outcome")
	}
}

/*
================
skillHolding

A character's whole skill-exp holding: the spendable points at
SkillExpPerSP apiece plus the accumulator toward the next one.
================
*/
func skillHolding(c *enterworld.Character) int64 {
	var points, exp int64
	if c.SkillPoints != nil {
		points = *c.SkillPoints
	}
	if c.SkillExp != nil {
		exp = *c.SkillExp
	}
	return points*progression.SkillExpPerSP + exp
}

/*
================
TestStyriaMatchPaysRewardsAtTheClock

A decided match pays every entered participant: the winner's job
EXP over the loser's, the same banded skill exp to both, and the
reward items skipped (this fixture's item table has no reward
rows) without failing the exps beside them. The granted half is
TestStyriaDoneWhenHunterAndThief's (#4375).
================
*/
func TestStyriaMatchPaysRewardsAtTheClock(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	thief := styriaTestCharacter(2, "Thief", domain.JobThief)
	hunter.Job = domain.CharacterJob{Type: domain.JobHunter, Grade: 1}
	thief.Job = domain.CharacterJob{Type: domain.JobThief, Grade: 1}
	rt, clock := newStyriaRuntime(t, hunter, thief)
	// The shipped level data, so the real job-exp add runs: its
	// contribution rule is what the assertions below read.
	rt.deps.(*enterworld.Deps).Levels = enterworld.NewTextdataLevels(gamedatatest.TextdataDir(t))
	prog := progression.NewRuntime(rt.deps)
	rt.UpdateExperience = prog.ExperienceUpdater()
	rt.UpdateJobExperience = prog.JobExperienceUpdater()

	match := styriaStartApplied(t, rt, clock, hunter, thief)
	rt.styriaRecordDeath(testDivision, thief, hunter, clock.NowMs())

	hunterSkill, thiefSkill := skillHolding(hunter), skillHolding(thief)
	clock.now = time.UnixMilli(match.endsMs)
	rt.advanceStyriaMatches(clock.NowMs())

	if got := hunter.Job.WeeklyReward; int64(got) != styria.RewardJobExpWin {
		t.Fatalf("winner job contribution = %d, want %d", got, styria.RewardJobExpWin)
	}
	if got := thief.Job.WeeklyReward; int64(got) != styria.RewardJobExpLose {
		t.Fatalf("loser job contribution = %d, want %d", got, styria.RewardJobExpLose)
	}
	if got := skillHolding(hunter) - hunterSkill; got != 670 {
		t.Fatalf("winner skill exp gained %d, want the level-20 band 670", got)
	}
	if got := skillHolding(thief) - thiefSkill; got != 670 {
		t.Fatalf("loser skill exp gained %d, want the level-20 band 670", got)
	}
	// Paid once: a second tick past the clock pays nothing more.
	rt.advanceStyriaMatches(clock.NowMs())
	if got := skillHolding(hunter) - hunterSkill; got != 670 {
		t.Fatalf("skill exp after a second tick %d, want one payment of 670", got)
	}
}
