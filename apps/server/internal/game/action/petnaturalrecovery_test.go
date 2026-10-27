/*
===========================================================================

petnaturalrecovery_test.go - the companion natural-recovery pulses

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
petRecoveryRuntime

One summoned attack pet at 100/1000 health and 10/100 mana, its session
bound at the fixture clock (1_000_000), so its first recovery pulse is
due at 1_004_000.
================
*/
func petRecoveryRuntime(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.CharacterCOS, petOwnerKey) {
	t.Helper()
	c := testCharacter()
	rt, _ := newTestRuntime(c, nil)
	ref := equipCombatTestPet(t, rt, c, 3)
	ref.MaxHP, ref.MaxMP = 1000, 100
	pet := c.ActiveCOS
	pet.CurrentHP, pet.CurrentMP = 100, 10
	pet.Satiety = 3000
	rt.BindPetSession(testDivision, c, 1)
	for key := range rt.petSessions {
		if key.gid == pet.GID {
			return rt, c, pet, key
		}
	}
	t.Fatal("pet session was not bound")
	return nil, nil, nil, petOwnerKey{}
}

/*
================
TestPetNaturalRecoveryAmount
================
*/
func TestPetNaturalRecoveryAmount(t *testing.T) {
	for maximum, want := range map[int64]int64{6858: 109, 1000: 16, 100: 1, 1: 0, 0: 0, -5: 0} {
		if got := petNaturalRecoveryAmount(maximum); got != want {
			t.Errorf("amount(%d) = %d, want %d", maximum, got, want)
		}
	}
}

/*
================
TestPetNaturalRecoveryPulses
================
*/
func TestPetNaturalRecoveryPulses(t *testing.T) {
	rt, _, pet, _ := petRecoveryRuntime(t)
	vitals := func(nowMs int64) (count int) {
		for _, batch := range rt.advancePets(nowMs) {
			for _, frame := range batch.Frames {
				if frame.Opcode == simulation.OpVitalsUpdate {
					count++
				}
			}
		}
		return count
	}
	if got := vitals(1_003_999); pet.CurrentHP != 100 || pet.CurrentMP != 10 || got != 0 {
		t.Fatalf("pulse fired early: %d/%d, %d frames", pet.CurrentHP, pet.CurrentMP, got)
	}
	if got := vitals(1_004_000); pet.CurrentHP != 116 || pet.CurrentMP != 11 || got == 0 {
		t.Fatalf("first pulse wrong: %d/%d, %d frames", pet.CurrentHP, pet.CurrentMP, got)
	}
	rt.advancePets(1_008_000)
	if pet.CurrentHP != 132 || pet.CurrentMP != 12 {
		t.Fatalf("second pulse wrong: %d/%d", pet.CurrentHP, pet.CurrentMP)
	}
}

/*
================
TestPetNaturalRecoveryDeadPetSpendsPulse
================
*/
func TestPetNaturalRecoveryDeadPetSpendsPulse(t *testing.T) {
	rt, _, pet, key := petRecoveryRuntime(t)
	pet.CurrentHP = 0
	if frames := rt.advancePetNaturalRecovery(key, 1_004_000); len(frames) != 0 || pet.CurrentHP != 0 {
		t.Fatalf("dead pet recovered: %d, %v", pet.CurrentHP, frames)
	}
	// The refused pulse is spent: a revived pet gets one pulse at the
	// next period, never a catch-up burst.
	pet.CurrentHP = 500
	rt.advancePetNaturalRecovery(key, 1_006_000)
	if pet.CurrentHP != 500 {
		t.Fatalf("mid-period probe healed: the refused pulse was queued, at %d", pet.CurrentHP)
	}
	rt.advancePetNaturalRecovery(key, 1_008_000)
	if pet.CurrentHP != 516 {
		t.Fatalf("revived pet must gain exactly one pulse, at %d", pet.CurrentHP)
	}
}

/*
================
TestPetNaturalRecoveryZombieGate
================
*/
func TestPetNaturalRecoveryZombieGate(t *testing.T) {
	rt, c, pet, key := petRecoveryRuntime(t)
	rt.storeCosAbnormal(testDivision, c.Name, pet.GID, &abnormal.Block{Mask: abnormal.Zombie.Bit()})
	if frames := rt.advancePetNaturalRecovery(key, 1_004_000); len(frames) != 0 || pet.CurrentHP != 100 {
		t.Fatalf("zombie pet recovered: %d, %v", pet.CurrentHP, frames)
	}
}

/*
================
TestPetNaturalRecoveryMotionGate
================
*/
func TestPetNaturalRecoveryMotionGate(t *testing.T) {
	rt, _, pet, key := petRecoveryRuntime(t)
	state := rt.petSessions[key]
	from := state.follower.Position(1_002_000)
	goal := simulation.Spawn{RegionID: from.RegionID, X: from.X + 500, Z: from.Z}
	state.follower.Advance(goal, 80, 1_002_000, func(_, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
		return to, nil
	})
	if frames := rt.advancePetNaturalRecovery(key, 1_004_000); len(frames) != 0 || pet.CurrentHP != 100 {
		t.Fatalf("moving pet recovered: %d, %v", pet.CurrentHP, frames)
	}
	state.follower.Stop(1_004_500)
	rt.advancePetNaturalRecovery(key, 1_008_000)
	if pet.CurrentHP != 116 {
		t.Fatalf("standing pet must recover, at %d", pet.CurrentHP)
	}
}

/*
================
TestPetNaturalRecoveryFullVitals
================
*/
func TestPetNaturalRecoveryFullVitals(t *testing.T) {
	rt, _, pet, key := petRecoveryRuntime(t)
	pet.CurrentHP, pet.CurrentMP = 1000, 100
	if frames := rt.advancePetNaturalRecovery(key, 1_004_000); len(frames) != 0 {
		t.Fatalf("full pet published %v", frames)
	}
	if pet.CurrentHP != 1000 || pet.CurrentMP != 100 {
		t.Fatalf("full pet changed: %d/%d", pet.CurrentHP, pet.CurrentMP)
	}
}
