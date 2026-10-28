/*
===========================================================================

petadmit_test.go - per-tick pet checks copy the owner only when admitted

elementalCompanionEligibility and bondSource run per pet per tick (and
per bond plan). They copied the owner's whole character, inventory
included, before learning that the pet was the wrong kind: about
2.6 GB a minute on the live shard (#1943). Admission keeps every
verdict and skips the copy for a rejected pet.

===========================================================================
*/
package action

import (
	"testing"
)

/*
================
TestPetChecksKeepTheirVerdictWithoutCopyingRejectedOwners

A crow owner: the crow check still passes, the wolf and bear checks
still fail, and each failing check allocates less than one copy of the
owner would.
================
*/
func TestPetChecksKeepTheirVerdictWithoutCopyingRejectedOwners(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
	rt.AttackPetExpansion = true
	roarField(t, rt, c, clock)
	gid, now := c.ActiveCOS.GID, clock.NowMs()
	step, ok := rt.elementalCompanion(testDivision, c, gid, "Crow", now)
	if !ok {
		t.Fatal("crow owner lost crow eligibility")
	}
	if _, ok := rt.elementalCompanion(testDivision, c, gid, "Wolf", now); ok {
		t.Fatal("crow owner became wolf-eligible")
	}
	if _, _, _, _, ok := rt.bondSource(step.key, step.state); ok {
		t.Fatal("crow became a bond source")
	}

	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, c) })
	wolfAllocs := testing.AllocsPerRun(20, func() { rt.elementalCompanion(testDivision, c, gid, "Wolf", now) })
	bondAllocs := testing.AllocsPerRun(20, func() { rt.bondSource(step.key, step.state) })
	t.Logf("allocs: one owner copy %.0f, rejected wolf check %.0f, rejected bond check %.0f", copyAllocs, wolfAllocs, bondAllocs)
	if wolfAllocs >= copyAllocs || bondAllocs >= copyAllocs {
		t.Fatalf("rejected checks still copy the owner: wolf %.0f, bond %.0f allocs; one copy is %.0f",
			wolfAllocs, bondAllocs, copyAllocs)
	}
}
