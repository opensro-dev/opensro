/*
===========================================================================

battlestate_transition_test.go - battle entry, death and timed exit

Exercise actor and victim publication through the public combat lane.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
)

/*
===============================================================================

BATTLE STATE

===============================================================================
*/

// battleFrameValue returns the channel-8 value of a frame, or -1.
/*
================
battleFrameValue
================
*/
func battleFrameValue(f wire.Frame) int {
	if f.Opcode != wire.OpObjectStateRefresh {
		return -1
	}
	refresh, err := wire.DecodeObjectStateRefresh(f.Payload)
	if err != nil || refresh.StateType != wire.StateChannelBattle {
		return -1
	}
	return int(refresh.Value)
}

// Striking from peace enters battle and publishes channel 8 = 1 after the
// strike's burst; a player already in battle publishes nothing.
/*
================
TestBattleStateEnteredByStrike
================
*/
func TestBattleStateEnteredByStrike(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	c.BattleUntilMs = 0
	now := clock.NowMs()
	first := rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	first = assertAndSeparateActionSession(t, first)
	if _, damage, _ := assertSkillDamageOpen(t, first.Frames, 2, enterworld.ObjectIDForCharacter(c), target.Gid); damage == 0 {
		t.Fatal("no strike")
	}
	if c.BattleUntilMs != now+battleStateMs {
		t.Fatalf("striker battle until %d, want %d", c.BattleUntilMs, now+battleStateMs)
	}
	last := first.Frames[len(first.Frames)-1]
	if battleFrameValue(last) != 1 || battleFrameValue(first.Broadcast[len(first.Broadcast)-1]) != 1 {
		t.Fatalf("strike burst %+v lacks a trailing channel-8 entry", first.Frames)
	}
	if again := rt.enterBattleState(testDivision, c, now+1); len(again) != 0 {
		t.Fatalf("already in battle, yet published %+v", again)
	}
}

// Being struck enters battle; death leaves it before LIFE-dead; the timer
// runs out after exactly 20 s.
/*
================
TestBattleStateStruckDeathAndExpiry
================
*/
func TestBattleStateStruckDeathAndExpiry(t *testing.T) {
	rt, clock, c, monster := newCombatTestRuntime(t, 100)
	c.BattleUntilMs = 0
	now := clock.NowMs()
	monster.Ref.DefaultSkillIDs[0] = 2
	skills := rt.deps.SkillData().(staticSkillSource)
	hit := skills[2]
	hit.Attack.Min, hit.Attack.Max, hit.Attack.Percent = 1, 1, 100
	skills[2] = hit
	r := rt.MonsterBasicAttack(testDivision, monster, enterworld.ObjectIDForCharacter(c), 2, now)
	if !r.Accepted || !r.TargetAlive {
		t.Fatalf("nonfatal hit %+v", r)
	}
	if c.BattleUntilMs != now+battleStateMs {
		t.Fatalf("struck battle until %d, want %d", c.BattleUntilMs, now+battleStateMs)
	}
	last := r.Frames[len(r.Frames)-1]
	if battleFrameValue(wire.Frame{Opcode: last.Opcode, Payload: last.Payload}) != 1 {
		t.Fatalf("struck burst %+v lacks a trailing channel-8 entry", r.Frames)
	}

	hit.Attack.Min, hit.Attack.Max = 1<<20, 1<<20
	skills[2] = hit
	r = rt.MonsterBasicAttack(testDivision, monster, enterworld.ObjectIDForCharacter(c), 2, now+1)
	if !r.Accepted || r.TargetAlive || c.BattleUntilMs != 0 {
		t.Fatalf("fatal hit %+v, battle until %d", r, c.BattleUntilMs)
	}
	exit, dead := -1, -1
	for i, f := range r.Frames {
		frame := wire.Frame{Opcode: f.Opcode, Payload: f.Payload}
		if battleFrameValue(frame) == 0 {
			exit = i
		}
		if refresh, err := wire.DecodeObjectStateRefresh(f.Payload); f.Opcode == wire.OpObjectStateRefresh && err == nil &&
			refresh.StateType == wire.StateChannelLife && refresh.Value == wire.LifeStateDead {
			dead = i
		}
	}
	if exit < 0 || dead < 0 || exit > dead {
		t.Fatalf("death frames %+v: battle exit %d, LIFE-dead %d", r.Frames, exit, dead)
	}

	rt2, clock2, c2, _ := newCombatTestRuntime(t, 100)
	c2.BattleUntilMs = 0
	start := clock2.NowMs()
	rt2.enterBattleState(testDivision, c2, start)
	if out := rt2.TickHook()(start + battleStateMs - 1); c2.BattleUntilMs == 0 || hasBattleExit(out) {
		t.Fatal("battle ended early")
	}
	out := rt2.TickHook()(start + battleStateMs)
	if c2.BattleUntilMs != 0 || !hasBattleExit(out) {
		t.Fatalf("battle did not end at 20 s: until %d, frames %+v", c2.BattleUntilMs, out)
	}
}

/*
================
hasBattleExit
================
*/
func hasBattleExit(batches []simulation.DivisionFrames) bool {
	for _, batch := range batches {
		for _, f := range batch.Frames {
			if battleFrameValue(wire.Frame{Opcode: f.Opcode, Payload: f.Payload}) == 0 {
				return true
			}
		}
	}
	return false
}
