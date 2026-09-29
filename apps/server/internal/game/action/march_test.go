/*
===========================================================================

march_test.go - Bard March ranks and shared party-buff lifecycle

Exercise the real command, effect and movement owners with shipped skill rows.
Party selection boundaries also cover Heal Shield and party invisibility.

===========================================================================
*/

package action

import (
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	movingMarchFirstID = 9734
	swingMarchLastID   = 9742
	musicRangeFirstID  = 9637
	bardManaFirstID    = 9645
	marchDuration      = 600000
	marchRadius        = 300
	marchHarpKind      = 14
	marchSpeedOpcode   = 0x376f
)

/*
================
marchFixture

Keep the shipped equipment requirement and costs. The fixture remains level
one; its supplied MP pool allows every rank without altering combat tables.
================
*/
func marchFixture(t *testing.T, id uint32) (*Runtime, *fakeClock, *enterworld.Character, enterworld.SkillRow) {
	t.Helper()
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	row, ok := shippedSkills(t).SkillByID(id)
	if !ok {
		t.Fatalf("missing March rank %d", id)
	}
	rt.deps.SkillData().(staticSkillSource)[id] = row
	c.Skills = append(c.Skills, id)
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(enterworld.DerivedMaxMP(c))
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = marchHarpKind
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	return rt, clock, c, row
}

/*
================
TestMarchEveryShippedRankLifecycle

Nine ranks share one producer. Verify real speed publication, independent
recipient identities, one-time membership, cancellation and strict expiry.
================
*/
func TestMarchEveryShippedRankLifecycle(t *testing.T) {
	for id := uint32(movingMarchFirstID); id <= swingMarchLastID; id++ {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			rt, clock, c, row := marchFixture(t, id)
			if !row.TimedEffect.Pinned || !row.MovementModifier.Supported || row.TimedJobExecutable() ||
				row.EffectDurationMs != marchDuration || row.TimedEffect.Area.Radius != marchRadius {
				t.Fatalf("incomplete March admission: %+v, %+v", row.TimedEffect, row.MovementModifier)
			}
			mate := nearbyCharacter(rt, c, 12, "march-mate", marchRadius)
			outside := nearbyCharacter(rt, c, 13, "march-outside", marchRadius+1)
			stranger := nearbyCharacter(rt, c, 14, "march-stranger", 1)
			rt.RewardParties = func(string) []RewardParty {
				return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(mate), enterworld.ObjectIDForCharacter(outside)}}}
			}
			beforeMP := *c.CurrentMP
			result := castSelf(rt, c, id)
			if result.DiagnosticRefusal != "" || !hasOpcode(result.Frames, wire.OpAttachedEffect) ||
				!hasOpcode(result.Frames, marchSpeedOpcode) {
				t.Fatalf("March did not publish its effect and speed: %+v", result)
			}
			if *c.CurrentMP != beforeMP-int64(row.Consumption.MP) || len(c.TimedSkillJobs) != 0 {
				t.Fatalf("wrong charge or persistence: MP %d, jobs %+v", *c.CurrentMP, c.TimedSkillJobs)
			}
			for name, want := range map[string]bool{c.Name: true, mate.Name: true, outside.Name: false, stranger.Name: false} {
				if hasSkillEffect(rt, name, id) != want {
					t.Errorf("%s received March = %v, want %v", name, !want, want)
				}
			}
			selfEffect := rt.effects.Snapshot(testDivision, c.Name)[0]
			mateEffect := rt.effects.Snapshot(testDivision, mate.Name)[0]
			if selfEffect.InstanceToken == mateEffect.InstanceToken || selfEffect.Phase != 1 || mateEffect.Phase != 2 {
				t.Fatal("recipient ownership was collapsed")
			}
			wantRun := float32(simulation.RunSpeed) * (1 + float32(row.MovementModifier.Percent)/100)
			if _, speed := worldSpeeds(rt, mate); speed != wantRun {
				t.Fatalf("party speed %v, want %v", speed, wantRun)
			}
			// These are independent timed buffs: leaving the party or the
			// caster cancelling its own instance cannot end a recipient's.
			rt.RewardParties = nil
			rt.HandleTargetInteract(testDivision, c, wire.CancelActiveEffectRequest{EffectID: id, InstanceToken: selfEffect.InstanceToken}.Encode())
			rt.drainStoppedCharacterEffects()
			if hasSkillEffect(rt, c.Name, id) || !hasSkillEffect(rt, mate.Name, id) {
				t.Fatal("caster cancellation altered recipient ownership")
			}
			clock.Advance(marchDuration * time.Millisecond)
			rt.effects.Expire(clock.NowMs())
			rt.drainStoppedCharacterEffects()
			if !hasSkillEffect(rt, mate.Name, id) {
				t.Fatal("buff expired at equality")
			}
			clock.Advance(time.Millisecond)
			rt.effects.Expire(clock.NowMs())
			rt.drainStoppedCharacterEffects()
			if _, speed := worldSpeeds(rt, mate); speed != simulation.RunSpeed || hasSkillEffect(rt, mate.Name, id) {
				t.Fatalf("expiry left speed %v or a live effect", speed)
			}
		})
	}
}

/*
================
TestMarchBardPassivesAndWeaponAdmission

BDMD reduces the prepared cost. MUER is a kind-two aura addend, so its learned
value does not expand March's kind-one selection. A sword must still fail.
================
*/
func TestMarchBardPassivesAndWeaponAdmission(t *testing.T) {
	rt, _, c, row := marchFixture(t, movingMarchFirstID)
	learnShipped(t, rt, c, bardManaFirstID)
	learnShipped(t, rt, c, musicRangeFirstID)
	c.CurrentMP = testInt64(enterworld.DerivedMaxMP(c))
	outside := nearbyCharacter(rt, c, 12, "outside-authored-radius", marchRadius+1)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(outside)}}}
	}
	before := *c.CurrentMP
	result := castSelf(rt, c, row.ID)
	const reducedFirstRankMP = 27
	if result.DiagnosticRefusal != "" || *c.CurrentMP != before-reducedFirstRankMP || hasSkillEffect(rt, outside.Name, row.ID) {
		t.Fatalf("Bard modifiers: result %+v, cost %d", result, before-*c.CurrentMP)
	}
	rt, _, c, row = marchFixture(t, movingMarchFirstID)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 2
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	before = *c.CurrentMP
	result = castSelf(rt, c, row.ID)
	if hasSkillEffect(rt, c.Name, row.ID) || *c.CurrentMP != before || len(result.Frames) != 1 || result.Frames[0].Payload[0] != 2 {
		t.Fatalf("non-harp March admitted: %+v", result)
	}
}

/*
================
TestSharedPartySelectionNativeBoundary

March, Heal Shield and party invisibility all dispatch select 5 to 58BEF0.
Test the selector contract directly, including its lack of a target-cap read.
================
*/
func TestSharedPartySelectionNativeBoundary(t *testing.T) {
	for _, id := range []uint32{movingMarchFirstID, healShieldA1, wizardInvisibleBID} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			rt, clock, c := concealmentFixture(t, id)
			row := rt.deps.SkillData().(staticSkillSource)[id]
			area := row.TimedEffect.Area
			if row.Concealment.Area.Present {
				area = row.Concealment.Area
			}
			at := nearbyCharacter(rt, c, 12, "at-boundary", float64(area.Radius))
			outside := nearbyCharacter(rt, c, 13, "past-boundary", float64(area.Radius)+0.01)
			inside := nearbyCharacter(rt, c, 14, "inside-boundary", 1)
			rt.RewardParties = func(string) []RewardParty {
				return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(at), enterworld.ObjectIDForCharacter(outside), enterworld.ObjectIDForCharacter(inside)}}}
			}
			area.MaxTargets = 1
			got := rt.concealmentRecipients(testDivision, c, area, clock.NowMs())
			if len(got) != 2 || got[0].Name != at.Name || got[1].Name != inside.Name {
				t.Fatalf("party selector expanded radius or applied cap: %+v", got)
			}
		})
	}
}
