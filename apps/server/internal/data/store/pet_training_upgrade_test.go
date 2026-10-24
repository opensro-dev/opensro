/*
===========================================================================

pet_training_upgrade_test.go - preserve pet saves across merged schema histories

===========================================================================
*/
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestStoredPetCastsSurviveReopen
================
*/
func TestStoredPetCastsSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := seededCharacter()
	want := domain.PetRowCooldown{Capacity: 3, Charges: 1, RechargeMs: 300000, ReadyAtMs: 600000, Kind: "Charges"}
	c.MissionInventory[0].Summon = &domain.CharacterCOS{Codename: "COS_P_RAVEN_040", Level: 90}
	c.MissionInventory[0].Summon.Training.Rows[1] = want
	if err := s.CreateCharacter(testDivision, "account", c); err != nil {
		t.Fatal(err)
	}
	s.Close()
	reopened := openTest(t, dir, newTestClock())
	var raw string
	if err := reopened.db.QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var got domain.Character
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.MissionInventory[0].Summon.Training.Rows[1] != want {
		t.Fatal("stored casts changed across restart")
	}
}

/*
================
TestPetTrainingUpgradeRewritesNothing

Legacy lab schemas 23-25 and main schema 26 preserve learned pet ranks and
equipment byte-for-byte while moving to the current schema; a repeated upgrade is idle.
================
*/
func TestPetTrainingUpgradeRewritesNothing(t *testing.T) {
	for _, sourceVersion := range []int{prePetTrainingVersion, preSequentialPetTreeVersion, preGuildFoundedVersion, preAcademyHonorVersion, preMergedPetVersion, preStoredPetCastsVersion} {
		t.Run(fmt.Sprint(sourceVersion), func(t *testing.T) {
			dir := t.TempDir()
			s := openTest(t, dir, newTestClock())
			c := seededCharacter()
			c.MissionInventory[0].Summon = &domain.CharacterCOS{Codename: "COS_P_BEAR_090", Level: 90,
				Training: domain.PetTraining{TreeVersion: 2, Levels: [15]uint8{10, 10, 1},
					Gear: [3]domain.PetGear{{RefObjID: 30209, Options: [6]uint64{214748397645}}}}}
			if err := s.CreateCharacter(testDivision, "account", c); err != nil {
				t.Fatal(err)
			}
			var before string
			if err := s.db.QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			s.Close()
			if sourceVersion <= preGuildFoundedVersion {
				downgradeToLayout9(t, dir)
			}
			rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, sourceVersion)
			if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
				t.Fatalf("upgrade %q: %v", backup, err)
			}
			reopened := openTest(t, dir, newTestClock())
			var after string
			if err := reopened.db.QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatal("the upgrade rewrote the character")
			}
			reopened.Close()
			if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
				t.Fatalf("second upgrade = %v, want ErrAuthorityCurrent", err)
			}

		})
	}
}
