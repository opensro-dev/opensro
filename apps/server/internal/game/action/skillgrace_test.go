/*
===========================================================================

skillgrace_test.go - a skill press inside cooldownGraceMs waits, never refuses

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
