/*
===========================================================================

coscombat_test.go - summoned targets in the common monster cast lifecycle

Preparation must retain the pet identity, release once, and keep each impact
on the wire. An owner staying connected cannot keep a departed pet targeted.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestMonsterCosCastRetainsTargetAndReleasesIndividualImpacts
================
*/
func TestMonsterCosCastRetainsTargetAndReleasesIndividualImpacts(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	equipCombatTestPet(t, rt, c, 2)
	m.Ref.DefaultSkillIDs[0] = 2
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.ActionCastingTimeMs, skill.Attack.ImpactCount = 1000, 2
	skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 10, 10, 100
	skills[2] = skill
	before, ownerBefore := c.ActiveCOS.CurrentHP, enterworld.CurrentHP(c)
	now := clock.NowMs()
	start := rt.MonsterBasicAttack(testDivision, m, c.ActiveCOS.GID, 2, now)
	if !start.Accepted || len(start.Frames) != 1 || c.ActiveCOS.CurrentHP != before {
		t.Fatalf("COS preparation: %+v", start)
	}
	token := binary.LittleEndian.Uint32(start.Frames[0].Payload[10:])
	if frames := rt.advanceMonsterCasts(now + 1000); len(frames) != 0 {
		t.Fatal("COS released at casting equality")
	}
	frames := rt.advanceMonsterCasts(now + 1001)
	if len(frames) == 0 || len(frames[0].Frames) == 0 || c.ActiveCOS.CurrentHP >= before || enterworld.CurrentHP(c) != ownerBefore {
		t.Fatalf("COS release missed or hit rider: %+v", frames)
	}
	release := frames[0].Frames[0]
	if release.Opcode != 0xb505 || release.Payload[0] != 1 || binary.LittleEndian.Uint32(release.Payload[1:]) != token ||
		binary.LittleEndian.Uint32(release.Payload[5:]) != c.ActiveCOS.GID || release.Payload[10] != 2 {
		t.Fatalf("COS release lost identity or impact count: %x", release.Payload)
	}
	if again := rt.advanceMonsterCasts(now + 2000); len(again) != 0 {
		t.Fatal("COS cast released twice")
	}
	if close := rt.drainSkillFinalizes(now + 1001 + int64(skill.ActionDurationMs)); len(close) == 0 {
		t.Fatal("COS cast bracket was never finalized")
	}
}

/*
================
TestMonsterCosCastCancelsDepartedOrReplacedPet
================
*/
func TestMonsterCosCastCancelsDepartedOrReplacedPet(t *testing.T) {
	for _, cause := range []string{"unsummoned", "dead", "replaced", "different-slot", "attacker-dead"} {
		t.Run(cause, func(t *testing.T) {
			rt, clock, c, m := newCombatTestRuntime(t, 100)
			equipCombatTestPet(t, rt, c, 2)
			m.Ref.DefaultSkillIDs[0] = 2
			skills := rt.deps.SkillData().(staticSkillSource)
			skill := skills[2]
			skill.ActionCastingTimeMs = 1000
			skills[2] = skill
			if start := rt.MonsterBasicAttack(testDivision, m, c.ActiveCOS.GID, 2, clock.NowMs()); !start.Accepted {
				t.Fatal("COS preparation refused")
			}
			switch cause {
			case "unsummoned":
				c.ActiveCOS.Summoned = false
			case "dead":
				c.ActiveCOS.CurrentHP = 0
			case "replaced":
				c.ActiveCOS.RefObjID++
			case "different-slot":
				c.ActiveCOS.InventorySlot++
			case "attacker-dead":
				rt.Monsters.ApplyDamage(testDivision, m.Gid, m.CurrentHP)
			}
			before := c.ActiveCOS.CurrentHP
			frames := rt.advanceMonsterCasts(clock.NowMs() + 1001)
			if c.ActiveCOS.CurrentHP != before || len(frames) != 1 || len(frames[0].Frames) != 1 || frames[0].Frames[0].Payload[0] != 2 {
				t.Fatalf("cancel changed HP or failed to close: %+v", frames)
			}
		})
	}
}

/*
================
TestMonsterHitOnMountedRiderLandsOnTheRide

CSkillManager_InitiateSkillCast (59B5F3) retargets a hostile single-target
skill aimed at a mounted player to the ridden COS. Unmounted, or once the
ride is dead, the rider keeps the target and the ride is never struck.
================
*/
func TestMonsterHitOnMountedRiderLandsOnTheRide(t *testing.T) {
	for _, state := range []string{"mounted", "unmounted", "ride-dead"} {
		rt, clock, c, m := newCombatTestRuntime(t, 100)
		equipCombatTestPet(t, rt, c, 2)
		c.ActiveCOS.Mounted = state != "unmounted"
		if state == "ride-dead" {
			c.ActiveCOS.CurrentHP = 0
		}
		m.Ref.DefaultSkillIDs[0] = 2
		skills := rt.deps.SkillData().(staticSkillSource)
		skill := skills[2]
		skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 10, 10, 100
		skills[2] = skill
		rt.CombatRoll = func() (uint32, error) { return 0, nil }
		rideBefore, riderBefore := c.ActiveCOS.CurrentHP, enterworld.CurrentHP(c)
		hit := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
		if state == "mounted" {
			if !hit.Accepted || c.ActiveCOS.CurrentHP >= rideBefore || enterworld.CurrentHP(c) != riderBefore {
				t.Fatalf("mounted hit: ride %d->%d, rider %d->%d", rideBefore, c.ActiveCOS.CurrentHP,
					riderBefore, enterworld.CurrentHP(c))
			}
			continue
		}
		if c.ActiveCOS.CurrentHP != rideBefore {
			t.Fatalf("%s: the ride was struck (%d->%d)", state, rideBefore, c.ActiveCOS.CurrentHP)
		}
	}
}
