/*
===========================================================================

battlestate_cos_test.go - a hit on the pet puts its owner in battle

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
==================
TestMonsterHitOnThePetPutsTheOwnerInBattle

52A1E0 -> 4E1DF0: the owner enters battle when a monster strikes the pet,
publishing channel 8 once; a second hit only restarts the countdown.
==================
*/
func TestMonsterHitOnThePetPutsTheOwnerInBattle(t *testing.T) {
	rt, clock, c, instance := newCombatTestRuntime(t, 100)
	instance.Ref.DefaultSkillIDs[0] = 2
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	skills := rt.deps.SkillData().(staticSkillSource)
	strike := skills[2]
	strike.Attack.Min, strike.Attack.Max, strike.Attack.Percent = 1, 1, 100
	skills[2] = strike
	c.BattleUntilMs = 0
	gid, ok := enterworld.CosObjectIDForCharacter(c)
	if !ok {
		t.Fatal("no pet gid")
	}
	equipCombatTestPet(t, rt, c, 4)

	entered := battleStateFrame(enterworld.ObjectIDForCharacter(c), true)
	count := func(frames []uint16, payloads [][]byte) int {
		n := 0
		for i, op := range frames {
			if op == entered.Opcode && bytes.Equal(payloads[i], entered.Payload) {
				n++
			}
		}
		return n
	}
	for hit, want := range []int{1, 0} {
		now := clock.NowMs()
		result := rt.MonsterBasicAttack(testDivision, instance, gid, 2, now)
		if !result.Accepted {
			t.Fatalf("hit %d on the pet refused: %+v", hit, result)
		}
		var ops []uint16
		var payloads [][]byte
		for _, f := range result.Frames {
			ops, payloads = append(ops, f.Opcode), append(payloads, f.Payload)
		}
		if got := count(ops, payloads); got != want {
			t.Fatalf("hit %d: %d battle entries, want %d", hit, got, want)
		}
		if c.BattleUntilMs != now+battleStateMs {
			t.Fatalf("hit %d: battle until %d, want %d", hit, c.BattleUntilMs, now+battleStateMs)
		}
		clock.Advance(3000)
	}
}
