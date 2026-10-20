/*
===========================================================================

skillgrace_test.go - a skill press inside cooldownGraceMs, or anywhere in
the action-recovery window, waits instead of refusing

===========================================================================
*/

package action

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
coolingSmash

A character that knows Smash with its cooldown ending readyInMs from now.
================
*/
func coolingSmash(t *testing.T, readyInMs int64) (*Runtime, *fakeClock, *enterworld.Character, uint32, []byte) {
	t.Helper()
	rt, clock, c, target := newCombatTestRuntime(t, 1_000_000)
	skill := shippedOffense(t, "SKILL_CH_SWORD_SMASH_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(10000)
	ready := clock.NowMs() + readyInMs
	if skill.CoolTimeGroup != 0 {
		c.SharedSkillCooldowns = map[uint8]int64{skill.CoolTimeGroup: ready}
	} else {
		c.OffensiveSkillCooldowns = map[uint32]int64{skill.Group: ready}
	}
	press := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode()
	return rt, clock, c, skill.ID, press
}

/*
================
castOpenedAt

Ticks every 10 ms for up to 2 s and returns the clock time the skill's
cast bracket opened (B245 [1]), or 0.
================
*/
func castOpenedAt(rt *Runtime, clock *fakeClock, skillID uint32) int64 {
	for tick := 0; tick < 200; tick++ {
		clock.Advance(10 * time.Millisecond)
		for _, burst := range rt.TickHook()(clock.NowMs()) {
			for _, frame := range burst.Frames {
				if frame.Opcode == wire.OpSkillCastResult && len(frame.Payload) >= 6 && frame.Payload[0] == 1 &&
					binary.LittleEndian.Uint32(frame.Payload[2:6]) == skillID {
					return clock.NowMs()
				}
			}
		}
	}
	return 0
}

/*
================
TestPressInsideGraceWaitsForTheCooldown

100 ms early: queued (count 2), then cast once ready, never refused.
================
*/
func TestPressInsideGraceWaitsForTheCooldown(t *testing.T) {
	rt, clock, c, skillID, press := coolingSmash(t, 100)
	ready := clock.NowMs() + 100
	assertQueuedAction(t, rt.HandleTargetInteract(testDivision, c, press))
	opened := castOpenedAt(rt, clock, skillID)
	if opened == 0 || opened < ready {
		t.Fatalf("cast opened at %d, ready at %d", opened, ready)
	}
}

/*
================
TestPressBeyondGraceIsStillRefused
================
*/
func TestPressBeyondGraceIsStillRefused(t *testing.T) {
	rt, _, c, _, press := coolingSmash(t, cooldownGraceMs+150)
	refused := rt.HandleTargetInteract(testDivision, c, press)
	if len(refused.Frames) != 1 || !bytes.Equal(refused.Frames[0].Payload, []byte{2, 0x05}) {
		t.Fatalf("press = %+v, want refusal 0x3005", refused.Frames)
	}
}

/*
================
TestGracePressBehindAnOpenCastWaitsForBoth

Pressed while a swing is open and 100 ms before ready: it casts only after
both the swing has closed and the cooldown has ended.
================
*/
func TestGracePressBehindAnOpenCastWaitsForBoth(t *testing.T) {
	rt, clock, c, skillID, press := coolingSmash(t, 100)
	ready := clock.NowMs() + 100
	target := binary.LittleEndian.Uint32(press[7:11])
	rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: target}.Encode())
	if !rt.hasOpenSkillCast(testDivision, c.Name) {
		t.Fatal("fixture: the swing is not open")
	}
	assertQueuedAction(t, rt.HandleTargetInteract(testDivision, c, press))
	opened := castOpenedAt(rt, clock, skillID)
	if opened == 0 || opened < ready {
		t.Fatalf("cast opened at %d, ready at %d", opened, ready)
	}
}

/*
================
recoverySmash

A character that knows Smash while the common action-recovery timer a
previous cast left still has recoveryInMs to run; Smash itself is ready.
================
*/
func recoverySmash(t *testing.T, recoveryInMs int64) (*Runtime, *fakeClock, *enterworld.Character, uint32, []byte, int64) {
	t.Helper()
	rt, clock, c, target := newCombatTestRuntime(t, 1_000_000)
	skill := shippedOffense(t, "SKILL_CH_SWORD_SMASH_A_01")
	if !skillActionRecoveryApplies(skill) {
		t.Fatalf("fixture: %s is not action-recovery applicable", skill.Codename)
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(10000)
	recoveryEnd := clock.NowMs() + recoveryInMs
	c.SkillActionRecoveryUntilMs = recoveryEnd
	press := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode()
	return rt, clock, c, skill.ID, press, recoveryEnd
}

/*
================
TestPressInsideActionRecoveryQueuesUntilItEnds

Window 2: no cast is open, but the previous cast's recovery still has
1200 ms to run (far beyond the 150 ms grace). The press queues and the
cast opens only once the recovery ends; before the fix it was refused
0x3005 on the spot.
================
*/
func TestPressInsideActionRecoveryQueuesUntilItEnds(t *testing.T) {
	rt, clock, c, skillID, press, recoveryEnd := recoverySmash(t, 1200)
	assertQueuedAction(t, rt.HandleTargetInteract(testDivision, c, press))
	opened := castOpenedAt(rt, clock, skillID)
	if opened == 0 || opened < recoveryEnd {
		t.Fatalf("cast opened at %d, recovery ends at %d", opened, recoveryEnd)
	}
}

/*
================
TestAdvanceHoldsQueuedPressUntilActionRecoveryEnds

Window 1, at the release gate itself: a queued press whose cast has
already closed (no cast is open here) must stay queued while the
previous cast's action recovery runs, and release at its end. Before
the fix the advance released it at once and the replay died on the
recovery refusal (0x3005).
================
*/
func TestAdvanceHoldsQueuedPressUntilActionRecoveryEnds(t *testing.T) {
	rt, clock, c, _, press, recoveryEnd := recoverySmash(t, 1200)
	key := simulation.WorldKey(testDivision, c.Name)
	rt.actionSessions.Store(key, actionSessionPublication{
		division: testDivision, name: c.Name, characterID: c.ID, queued: true,
		pending: &basicAttackIntent{DivisionID: testDivision, CharacterName: c.Name, SingleCast: true,
			Deferred: &deferredObjectAction{payload: press}},
	})
	if frames := rt.advanceQueuedActionSessions(clock.NowMs() + 100); len(frames) != 0 {
		t.Fatalf("released during the recovery: %+v", frames)
	}
	stored, _ := rt.actionSessions.Load(key)
	if session := stored.(actionSessionPublication); !session.queued || session.pending == nil {
		t.Fatal("the pending press was dropped during the recovery")
	}
	frames := rt.advanceQueuedActionSessions(recoveryEnd)
	if len(frames) != 1 || len(frames[0].Frames) != 1 || frames[0].Frames[0].Opcode != wire.OpActionState {
		t.Fatalf("release at the recovery end = %+v", frames)
	}
	stored, _ = rt.actionSessions.Load(key)
	if session := stored.(actionSessionPublication); session.queued || session.pending != nil {
		t.Fatal("the pending press was not consumed at release")
	}
}
