/*
===========================================================================

interactionlock_test.go - a stall or an exchange pins its player (#3433)

While a player keeps a stall, visits one or trades, the movement lane's
interaction gate holds and no direct skill is admitted, so neither a walk
nor a teleport (Ghost Walk, a dash) carries the player out of it. An
item's own skill keeps the item-use door's rule.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
interactionLockSkill

A hand-built teleport row with no weapon, resource or target gate, so the
interaction gate is the only refusal the admission can reach.
================
*/
func interactionLockSkill() enterworld.SkillRow {
	skill := enterworld.SkillRow{ID: 9001}
	skill.RequiredWeaponKinds = [2]uint8{0xff, 0xff}
	skill.CastGate.Tele = true
	return skill
}

/*
================
TestStallAndExchangePinThePlayer
================
*/
func TestStallAndExchangePinThePlayer(t *testing.T) {
	skill := interactionLockSkill()
	for _, tc := range []struct {
		name  string
		enter func(rt *Runtime, c *enterworld.Character)
		leave func(rt *Runtime, c *enterworld.Character)
	}{
		{
			name: "stall keeper",
			enter: func(rt *Runtime, c *enterworld.Character) {
				if err := rt.Stalls.Open(testDivision, c.Name, "stall", 0); err != nil {
					t.Fatal(err)
				}
			},
			leave: func(rt *Runtime, c *enterworld.Character) { rt.Stalls.Close(testDivision, c.Name) },
		},
		{
			name: "stall visitor",
			enter: func(rt *Runtime, c *enterworld.Character) {
				if err := rt.Stalls.Open(testDivision, "Host", "stall", 0); err != nil {
					t.Fatal(err)
				}
				if _, err := rt.Stalls.Enter(testDivision, c.Name, "Host"); err != nil {
					t.Fatal(err)
				}
			},
			leave: func(rt *Runtime, c *enterworld.Character) {
				if _, err := rt.Stalls.Leave(testDivision, c.Name); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:  "exchange",
			enter: func(rt *Runtime, c *enterworld.Character) { rt.Exchanges.Open(testDivision, c.Name, "Partner") },
			leave: func(rt *Runtime, c *enterworld.Character) { rt.Exchanges.Close(testDivision, c.Name) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, c, _ := newCombatTestRuntime(t, 100000)
			now := clock.NowMs()
			if rt.PlayerInteractionLocked(testDivision, c.Name) {
				t.Fatal("a free player is interaction locked")
			}
			if got := rt.skillAdmission(testDivision, c, skill, now, nil, nil, admitCommand); got != 0 {
				t.Fatalf("a free player's teleport refused %#x", got)
			}

			tc.enter(rt, c)
			if !rt.PlayerInteractionLocked(testDivision, c.Name) {
				t.Fatal("the player walks out of the interaction")
			}
			if got := rt.skillAdmission(testDivision, c, skill, now, nil, nil, admitCommand); got != 0x3009 {
				t.Fatalf("teleport inside the interaction %#x, want 0x3009", got)
			}
			indirect := admitContext{Indirect: true}
			if got := rt.contextSkillAdmission(testDivision, c, skill, now, nil, nil, admitCommand, indirect); got != 0 {
				t.Fatalf("an item's skill took the direct cast's gate %#x", got)
			}

			tc.leave(rt, c)
			if rt.PlayerInteractionLocked(testDivision, c.Name) {
				t.Fatal("the gate outlived the interaction")
			}
			if got := rt.skillAdmission(testDivision, c, skill, now, nil, nil, admitCommand); got != 0 {
				t.Fatalf("teleport after the interaction closed refused %#x", got)
			}
		})
	}
}
