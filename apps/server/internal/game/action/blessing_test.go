/*
===========================================================================

blessing_test.go - Cleric blessings and Heal Shield

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	blessPhysicalA1 = 10415 // SKILL_EU_CLERIC_BLESSA_PHYSICAL_A_01: defp 23 0 100, getv HLBP
	healShieldA1    = 10196 // SKILL_EU_CLERIC_RECOVERYA_HEALSHIELD_A_01: efr 1 1 300 8 0 5, defp 115 183 0
	praiseHLBP      = 90001 // a synthetic passive holding HLBP 9
)

/*
==================
TestBlessingAddsTheCastersHLBP

Natively the ally's physical defense rises by defp's 23 plus the
caster's HLBP (9 from a learned passive); with no target the blessing
lands on the caster.
==================
*/
func TestBlessingAddsTheCastersHLBP(t *testing.T) {
	for _, capped := range []bool{true, false} {
		blessAlly(t, capped)
	}
	rt2, _, c2 := concealmentFixture(t, blessPhysicalA1)
	if r := rt2.HandleTargetInteract(testDivision, c2, wire.SkillAction{ActionId: blessPhysicalA1}.Encode()); !hasSkillEffect(rt2, c2.Name, blessPhysicalA1) {
		t.Fatalf("untargeted blessing did not land on the caster: %+v", r)
	}
}

// blessAlly casts the shipped blessing, or a copy without its 100 % cap,
// from a caster holding HLBP 9 onto an ally.
func blessAlly(t *testing.T, capped bool) {
	t.Helper()
	rt, _, c := concealmentFixture(t, blessPhysicalA1)
	table := rt.deps.SkillData().(staticSkillSource)
	if !capped {
		row := table[blessPhysicalA1]
		row.TimedEffect.CapPercent = 0
		table[blessPhysicalA1] = row
	}
	var passive enterworld.SkillRow
	passive.ID, passive.Group, passive.Level = praiseHLBP, 90001, 1
	passive.PassiveParameters.Pinned = true
	passive.PassiveParameters.Mask = 1 << enterworld.ParameterBlessPhysical
	passive.PassiveParameters.Values[enterworld.ParameterBlessPhysical] = 9
	table[praiseHLBP] = passive
	c.Skills = append(c.Skills, praiseHLBP)
	ally := nearbyCharacter(rt, c, 21, "ally", 1)
	ally.Skills = nil

	// The shipped cap (100 %) holds the gain to the ally's current defense.
	before, _ := rt.PlayerBaseStats(testDivision, ally)
	bless := func() {
		rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: blessPhysicalA1, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(ally)}.Encode())
	}
	bless()
	after, _ := rt.PlayerBaseStats(testDivision, ally)
	want := before.PhysicalDefense + 23 + 9
	if capped {
		want = 2 * before.PhysicalDefense
	}
	if !hasSkillEffect(rt, ally.Name, blessPhysicalA1) || after.PhysicalDefense != want {
		t.Fatalf("capped %v: defense %v -> %v, want %v", capped, before.PhysicalDefense, after.PhysicalDefense, want)
	}
}

/*
==================
TestHealShieldCoversCasterAndParty

efr select 5: the caster and its party within 300 receive the defense;
a stranger does not.
==================
*/
func TestHealShieldCoversCasterAndParty(t *testing.T) {
	rt, clock, c := concealmentFixture(t, healShieldA1)
	// 214 MP is beyond a level-1 caster; the cost is not under test.
	row := rt.deps.SkillData().(staticSkillSource)[healShieldA1]
	row.Consumption.MP = 10
	rt.deps.SkillData().(staticSkillSource)[healShieldA1] = row
	mate := nearbyCharacter(rt, c, 12, "mate", 50)
	stranger := nearbyCharacter(rt, c, 11, "stranger", 50)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(mate)}}}
	}
	if r := castSelf(rt, c, healShieldA1); r.DiagnosticRefusal != "" || len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
		t.Fatalf("heal shield cast: %+v", r)
	}
	clock.Advance(2 * time.Second) // the 1168 ms preparation
	releasePreparedSkillForTest(t, rt, clock.NowMs())
	for name, want := range map[string]bool{c.Name: true, mate.Name: true, stranger.Name: false} {
		if got := hasSkillEffect(rt, name, healShieldA1); got != want {
			t.Errorf("%s shielded %v, want %v", name, got, want)
		}
	}
}
