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

/*
================
TestUpgradeGatesEachBookOnThePreviousBooksCap

An old-tree row with ranks to spare, at every pet level: a book that
receives ranks follows a book at its own MaxLevel (10 / 10 / 1), never one
that only reached the next book's cap.
================
*/
func TestUpgradeGatesEachBookOnThePreviousBooksCap(t *testing.T) {
	const maxPetLevel = 120
	for _, species := range D.Pets {
		for level := uint8(1); level <= maxPetLevel; level++ {
			p := &domain.CharacterCOS{Codename: species.CodenamePrefix + "_090", Level: level}
			for i := range p.Training.Levels {
				p.Training.Levels[i] = 10
			}
			UpgradeTree(p)
			var previous SkillDef
			for _, s := range SkillsForPet(p) {
				if s.Option > 1 && previous.Row == s.Row && p.Training.Levels[s.Index()] > 0 && p.Training.Levels[previous.Index()] < previous.MaxLevel {
					t.Fatalf("%s level %d: %s holds %d ranks after %s at %d of %d", species.CodenamePrefix, level, s.Name, p.Training.Levels[s.Index()], previous.Name, p.Training.Levels[previous.Index()], previous.MaxLevel)
				}
				previous = s
			}
		}
	}
}

/*
================
TestSequentialPassivesAlwaysUseHighestLearnedBook
================
*/
func TestSequentialPassivesAlwaysUseHighestLearnedBook(t *testing.T) {
	for _, kind := range []string{"COS_P_BEAR_090", "COS_P_WOLF_090", "COS_P_PENGUIN_090", "COS_P_RAVEN_090"} {
		p := &domain.CharacterCOS{Codename: kind, Level: 140, Training: domain.PetTraining{TreeVersion: 2}}
		for row := ActiveRowCount; row < RowCount; row++ {
			for tier := 1; tier <= 3; tier++ {
				var learned SkillDef
				for _, s := range SkillsForPet(p) {
					if s.Row == row && s.Option == tier {
						p.Training.Levels[s.Index()] = 1
						learned = s
					}
				}
				if learned.ID == 0 {
					t.Fatalf("missing %s row %d tier %d", kind, row, tier)
				}
				for _, stale := range []uint8{0, 1, 2, 3, 255} {
					p.Training.Slotted[row-ActiveRowCount] = stale
					got, ok := SlottedPassive(p, row)
					if !ok || got.ID != learned.ID {
						t.Fatalf("%s row%d: stale slot %d selected %d instead of %d", kind, row, stale, got.ID, learned.ID)
					}
					if Slot(p, learned.ID) == "" {
						t.Fatal("sequential passive still manually selectable")
					}
				}
			}
		}
		Refund(p)
		for row := ActiveRowCount; row < RowCount; row++ {
			if _, ok := SlottedPassive(p, row); ok {
				t.Fatal("refund retained passive")
			}
		}
	}
}

/*
================
TestEveryThirdBookCanMaxAtLevel90

Port-only: the final book spends points at the level cap, without requiring 126.
================
*/
func TestEveryThirdBookCanMaxAtLevel90(t *testing.T) {
	for _, species := range D.Pets {
		for row := range RowCount {
			p := &domain.CharacterCOS{Codename: species.CodenamePrefix + "_090", Level: 90, Training: domain.PetTraining{TreeVersion: 2, BonusPoints: 150}}
			for _, s := range SkillsForPet(p) {
				if s.Row != row {
					continue
				}
				for rank := uint8(0); rank < s.MaxLevel; rank++ {
					if s.Option == 3 && NextLevelRequirement(s, rank) != 90 {
						t.Fatalf("%s rank %d exceeds cap", s.Name, rank+1)
					}
					if msg := Learn(p, s.ID); msg != "" {
						t.Fatalf("%s rank %d: %s", s.Name, rank+1, msg)
					}
				}
			}
		}
	}
}

/*
================
TestBooksCapAtTenTenOne

Port-only: first and second books reach 10, the third stops at 1 with its
former rank-10 values; a third book saved above 1 reads as 1 and refunds
the excess points.
================
*/
func TestBooksCapAtTenTenOne(t *testing.T) {
	want := map[int]uint8{1: 10, 2: 10, 3: 1}
	for _, species := range D.Pets {
		p := &domain.CharacterCOS{Codename: species.CodenamePrefix + "_090", Level: 90, Training: domain.PetTraining{TreeVersion: 2, BonusPoints: 150}}
		for _, s := range SkillsForPet(p) {
			if s.MaxLevel != want[s.Option] {
				t.Fatalf("%s max %d, want %d", s.Name, s.MaxLevel, want[s.Option])
			}
			if s.Option != 3 {
				continue
			}
			if previous, _ := PetSkill(p, s.ID-1); s.ValueAt(1) < previous.ValueAt(10) {
				t.Fatalf("%s rank 1 (%v) is weaker than %s rank 10 (%v)", s.Name, s.ValueAt(1), previous.Name, previous.ValueAt(10))
			}
			before := TrainingPoints(p)
			p.Training.Levels[s.Index()] = 10 // saved while third books had 10 ranks
			if Level(p, s) != 1 || TrainingPoints(p) != before-1 || LearnRefusal(p, s.ID) == "" {
				t.Fatalf("%s: saved rank 10 reads %d, points %d->%d", s.Name, Level(p, s), before, TrainingPoints(p))
			}
		}
	}
}
