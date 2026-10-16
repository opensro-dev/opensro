/*
===========================================================================

tiers_test.go - sequential book admission and lossless point conversion

===========================================================================
*/
package companion

import (
	"opensro.online/server/internal/domain"
	"testing"
)

/*
================
TestSequentialBooksRequirePetLevelAndPreviousMaximum
================
*/
func TestSequentialBooksRequirePetLevelAndPreviousMaximum(t *testing.T) {
	p := &domain.CharacterCOS{Codename: "COS_P_BEAR_040", Level: 40, Training: domain.PetTraining{TreeVersion: 2, BonusPoints: 60}}
	first, _ := PetSkill(p, 16)
	second, _ := PetSkill(p, 17)
	if NextLevelRequirement(first, 0) != 10 || NextLevelRequirement(first, 9) != 40 || NextLevelRequirement(second, 0) != 40 || NextLevelRequirement(second, 9) != 90 {
		t.Fatal("book endpoints differ from 10/40/90")
	}
	if LearnRefusal(p, 17) == "" {
		t.Fatal("second book skipped its predecessor")
	}
	for range 10 {
		if msg := Learn(p, 16); msg != "" {
			t.Fatal(msg)
		}
	}
	if msg := Learn(p, 17); msg != "" {
		t.Fatal(msg)
	}
	if LearnRefusal(p, 17) == "" {
		t.Fatal("second book advanced without the next pet level")
	}
	p.Level = 90
	for range 9 {
		if msg := Learn(p, 17); msg != "" {
			t.Fatal(msg)
		}
	}
	if msg := Learn(p, 18); msg != "" {
		t.Fatal(msg)
	}
}

/*
================
TestUpgradeRetainsPointBudgetGearAndCooldown
================
*/
func TestUpgradeRetainsPointBudgetGearAndCooldown(t *testing.T) {
	p := &domain.CharacterCOS{Codename: "COS_P_WOLF_040", Level: 40, Training: domain.PetTraining{BonusPoints: 30}}
	p.Training.Levels[0], p.Training.Levels[1], p.Training.Levels[2] = 3, 4, 5
	p.Training.Gear[0].RefObjID = 123
	p.Training.Rows[0].Left = 7
	budget := TrainingPoints(p) + 12
	UpgradeTree(p)
	spent := 0
	for _, n := range p.Training.Levels {
		spent += int(n)
	}
	if TrainingPoints(p)+spent != budget || p.Training.Levels[0] != 10 || p.Training.Levels[1] != 1 || p.Training.Levels[2] != 0 || p.Training.Gear[0].RefObjID != 123 || p.Training.Rows[0].Left != 7 {
		t.Fatal("migration changed budget, prerequisites, gear or cooldown")
	}
	copy := p.Training
	UpgradeTree(p)
	if copy != p.Training {
		t.Fatal("migration was not idempotent")
	}
}
