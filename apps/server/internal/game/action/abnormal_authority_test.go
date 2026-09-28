/*
===========================================================================

abnormal_authority_test.go - status effects across character transactions

The production character store cannot take a read lock from its own write
transaction. Reject that callback order directly so a regression fails without
hanging the test process, then verify the status and damage actually commit.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
transactionCheckedCharacters
================
*/
type transactionCheckedCharacters struct {
	t       *testing.T
	source  enterworld.CharacterSource
	writing *bool
}

/*
================
CharactersForDivision
================
*/
func (s transactionCheckedCharacters) CharactersForDivision(division string) []*enterworld.Character {
	s.t.Helper()
	if *s.writing {
		s.t.Fatal("character lookup re-entered the authority write transaction")
	}
	return s.source.CharactersForDivision(division)
}

/*
================
TestAttackStatusResolvesSourceBeforeAuthorityTransaction
================
*/
func TestAttackStatusResolvesSourceBeforeAuthorityTransaction(t *testing.T) {
	const burnTag = 0x6275
	const burnLevel = 10
	const guaranteedChance = 100
	const burnPower = 5
	rt, clock, character, target := newCombatTestRuntime(t, 100000)
	deps := rt.deps.(*enterworld.Deps)
	writing := false
	deps.Characters = transactionCheckedCharacters{t: t, source: deps.Characters, writing: &writing}
	deps.ReadCharacter = func(_ string, read func()) {
		if writing {
			t.Fatal("character snapshot re-entered the authority write transaction")
		}
		read()
	}
	commits := 0
	deps.UpdateCharacters = func(_ []*enterworld.Character, _ string, update func() bool) bool {
		if writing {
			t.Fatal("nested character transaction")
		}
		writing = true
		defer func() { writing = false }()
		changed := update()
		if changed {
			commits++
		}
		return changed
	}
	skills := deps.Skills.(staticSkillSource)
	skill := skills[2]
	index, ok := abnormal.SourceIndex(burnTag)
	if !ok {
		t.Fatal("burn source missing")
	}
	skill.Abnormal.Params[index] = abnormal.Param{Present: true, Args: [6]uint32{burnLevel, guaranteedChance, burnPower}}
	skills[skill.ID] = skill
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	result := rt.HandleTargetInteract(testDivision, character, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	if result.DiagnosticRefusal != "" {
		t.Fatal(result.DiagnosticRefusal)
	}
	after, ok := rt.Monsters.Get(testDivision, target.Gid)
	if !ok || after.CurrentHP >= target.CurrentHP || after.AbnormalMask()&abnormal.Burn.Bit() == 0 || commits != 1 {
		t.Fatalf("attack failed to commit damage and burn: HP=%d mask=%x commits=%d", after.CurrentHP, after.AbnormalMask(), commits)
	}
	plan, ok := rt.Monsters.PlanAbnormalUpdate(testDivision, target.Gid, clock.NowMs()+2001)
	if !ok || len(plan.Effects.Hits) == 0 || !plan.Effects.Hits[0].Credited {
		t.Fatalf("prepared source lost periodic damage credit: %+v", plan.Effects)
	}
}
