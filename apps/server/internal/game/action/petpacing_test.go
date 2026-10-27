/*
===========================================================================

petpacing_test.go - the port-only pet pacing setting and its native default

===========================================================================
*/
package action

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPetRunSpeedPolicyLeavesVehiclesAndWalkingAlone

Pacing slows only the run a grab or growth pet moves with; the native
parameter the follow and battle rules compare keeps its value.
================
*/
func TestPetRunSpeedPolicyLeavesVehiclesAndWalkingAlone(t *testing.T) {
	rt := &Runtime{PetPolicies: companion.Policies{Pacing: true}}
	for _, band := range []uint16{1, 2, 3, 4, 6} {
		ref := &enterworld.CharacterRef{TidWord: 0x1c6 | band<<11, RunSpeed: 100, WalkSpeed: 20}
		want := float32(100)
		if band == 3 || band == 4 {
			want = 80
		}
		walk, run := rt.cosMovementSpeeds(ref, nil, nil)
		if run != want || walk != 20 {
			t.Fatalf("band %d moves at %v/%v, want 20/%v", band, walk, run, want)
		}
		if got := cosParameter(ref, nil, nil, movementRunParameter); got != 100 {
			t.Fatalf("band %d native run parameter paced to %v", band, got)
		}
	}
}

/*
================
TestPacedFollowSpeedMatchesNativeAtFullFactor

A paced pet already at its native full speed installs nothing: the battle
reset compares the native parameter, not the paced one, so it
does not resend the speed every tick.
================
*/
func TestPacedFollowSpeedMatchesNativeAtFullFactor(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	rt.PetPolicies.Pacing = true
	ref := refs.characters["ATTACK"]
	pet := &enterworld.CharacterCOS{GID: 42, Codename: "ATTACK", RefObjID: ref.RefObjID, CurrentHP: 100, Summoned: true}
	run := cosParameter(ref, pet, nil, movementRunParameter)
	if run < ref.RunSpeed {
		t.Fatalf("native run %v below authored %v: the battle reset would fire", run, ref.RunSpeed)
	}
	state := &petSession{follower: simulation.NewPetFollower(pet.GID, simulation.Spawn{RegionID: 0x62aa}), character: c}
	step := petCombatStep{key: petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: pet.GID}, state: state,
		snapshot: c, pet: pet, ref: ref, run: run, nowMs: 1000}
	if frames := rt.setCompanionFollowSpeed(step, 100); len(frames) != 0 {
		t.Fatal("full factor resent the follow speed", frames)
	}
}

/*
================
TestPetPacingDefaultsToNative

SRO_PET_PACING unset leaves pets native; only an explicit value enables it.
================
*/
func TestPetPacingDefaultsToNative(t *testing.T) {
	t.Setenv(companion.EnvPetPacing, "")
	if policies := companion.PoliciesFromEnv(); policies != (companion.Policies{}) {
		t.Fatalf("unset environment enabled %+v", policies)
	}
	t.Setenv(companion.EnvPetPacing, "1")
	if policies := companion.PoliciesFromEnv(); policies != (companion.Policies{Pacing: true}) {
		t.Fatalf("set environment read as %+v", policies)
	}
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	if rt.PetPolicies != (companion.Policies{}) || rt.PetPolicies.RunSpeed(4, 100) != 100 {
		t.Fatal("a new runtime is not native")
	}
}
