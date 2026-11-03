/*
===========================================================================

monsterline_test.go - the line-of-sight phase of a monster's skill command

A monster whose line to its target is blocked (a floor of a stacked cave,
a wall) is refused before any cast starts, on every strike path: a
character, a summoned pet, and a tempted monster's neighbour. A clear line
and a runtime without a line-of-sight query still strike.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
blockedLine

A line-of-sight query that reports every line blocked and counts its calls.
================
*/
func blockedLine(calls *int) func(simulation.Spawn, simulation.NavOwner, simulation.Spawn) bool {
	return func(simulation.Spawn, simulation.NavOwner, simulation.Spawn) bool {
		*calls++
		return false
	}
}

/*
================
TestMonsterStrikeRefusedThroughBlockedLine
================
*/
func TestMonsterStrikeRefusedThroughBlockedLine(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	m.Ref.DefaultSkillIDs[0] = 2
	calls := 0
	rt.LineOfSight = blockedLine(&calls)
	before := enterworld.CurrentHP(c)
	result := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
	if result.Accepted || result.Refusal != simulation.MonsterAttackCommandRejected || calls != 1 ||
		len(result.Frames) != 0 || enterworld.CurrentHP(c) != before || rt.castTokenCounter != 0 {
		t.Fatalf("blocked line still struck: %+v calls=%d", result, calls)
	}
}

/*
================
TestMonsterStrikeThroughClearLine

A clear line, and a runtime with no line-of-sight query, accept the command.
================
*/
func TestMonsterStrikeThroughClearLine(t *testing.T) {
	for _, name := range []string{"clear", "no-query"} {
		t.Run(name, func(t *testing.T) {
			rt, clock, c, m := newCombatTestRuntime(t, 100)
			m.Ref.DefaultSkillIDs[0] = 2
			calls := 0
			if name == "clear" {
				rt.LineOfSight = func(simulation.Spawn, simulation.NavOwner, simulation.Spawn) bool {
					calls++
					return true
				}
			} else {
				rt.LineOfSight = nil
			}
			result := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
			if !result.Accepted || name == "clear" && calls != 1 {
				t.Fatalf("clear line refused: %+v calls=%d", result, calls)
			}
		})
	}
}

/*
================
TestMonsterCosStrikeRefusedThroughBlockedLine
================
*/
func TestMonsterCosStrikeRefusedThroughBlockedLine(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	equipCombatTestPet(t, rt, c, 2)
	m.Ref.DefaultSkillIDs[0] = 2
	calls := 0
	rt.LineOfSight = blockedLine(&calls)
	before := c.ActiveCOS.CurrentHP
	result := rt.MonsterBasicAttack(testDivision, m, c.ActiveCOS.GID, 2, clock.NowMs())
	if result.Accepted || result.Refusal != simulation.MonsterAttackCommandRejected || calls != 1 ||
		len(result.Frames) != 0 || c.ActiveCOS.CurrentHP != before || rt.castTokenCounter != 0 {
		t.Fatalf("blocked line still struck the pet: %+v calls=%d", result, calls)
	}
}

/*
================
TestTemptedMonsterRefusedThroughBlockedLine

The tempted monster keeps trying, but no strike reaches its neighbour.
================
*/
func TestTemptedMonsterRefusedThroughBlockedLine(t *testing.T) {
	f := newTemptationFight(t, 0)
	f.castTemptation(t)
	calls := 0
	f.rt.LineOfSight = blockedLine(&calls)
	bHP := f.hp(f.b.Gid)
	if f.run(func() bool { return f.hp(f.b.Gid) < bHP }) {
		t.Fatalf("tempted monster struck through a blocked line: b hp %d->%d", bHP, f.hp(f.b.Gid))
	}
	if calls == 0 {
		t.Fatal("the tempted strike never asked the line-of-sight query")
	}
}
