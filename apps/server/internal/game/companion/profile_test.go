/*
===========================================================================

profile_test.go - species profiles carry attack and defense, never HP

A pet's MaxHP is the native per-level character row, with or without the
expansion (issue #2567): the design once carried a healthByLevel curve
that replaced it, and these tests keep the curve from coming back.
What the profile owns is the species' attack and defense identity.

===========================================================================
*/
package companion

import "testing"

/*
================
TestProfileScopesAttackAndDefenseOnly
================
*/
func TestProfileScopesAttackAndDefenseOnly(t *testing.T) {
	for _, pet := range D.Pets {
		p := Profile(pet.CodenamePrefix)
		if p.Name != pet.Name {
			t.Fatalf("profile %q does not resolve %q", pet.CodenamePrefix, pet.Name)
		}
		if p.PhysicalPercent == 0 || p.MagicalPercent == 0 || p.DefensePercent == 0 || p.MagicDefensePercent == 0 {
			t.Fatalf("%s: profile drops a combat percent: %+v", pet.Name, p)
		}
	}
	unclaimed := Profile("COS_P_JAGUAR")
	if unclaimed.PhysicalPercent != 100 || unclaimed.MagicalPercent != 100 || unclaimed.DefensePercent != 100 || unclaimed.MagicDefensePercent != 100 {
		t.Fatalf("unclaimed codename: profile %+v, want neutral 100s", unclaimed)
	}
}

/*
================
TestAttackScalesUseProfilePercents
================
*/
func TestAttackScalesUseProfilePercents(t *testing.T) {
	// Bear is the physical species (65 physical / 20 magical in the
	// design); a neutral modifier returns the profile's own percents.
	phys, magic := AttackScales("COS_P_BEAR_040", Modifiers{})
	if phys != 0.65 || magic != 0.20 {
		t.Fatalf("bear scales %v/%v, want 0.65/0.20", phys, magic)
	}
}
