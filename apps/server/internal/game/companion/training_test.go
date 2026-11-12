/*
===========================================================================

training_test.go - the attack-pet trees and gear follow the design rules

Assertions read the embedded design, so a balance change in the workbook
does not break them; a rule change does.

===========================================================================
*/
package companion

import (
	"encoding/json"
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
skillNamed
================
*/
func skillNamed(t *testing.T, name string) SkillDef {
	t.Helper()
	for _, s := range D.Skills {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no skill %q in the design", name)
	return SkillDef{}
}

/*
================
TestDesignGivesEveryRowALiveSkill

Each pet can use all five rows: every row offers at least one learnable
skill, and every pet kind maps to a native codename family.
================
*/
func TestDesignGivesEveryRowALiveSkill(t *testing.T) {
	for _, pet := range D.Pets {
		for row := range RowCount {
			live := 0
			for _, s := range PetSkills(pet.Name) {
				if s.Row == row && s.Live() {
					live++
				}
			}
			if live == 0 {
				t.Fatalf("%s row %d has no implemented skill", pet.Name, row)
			}
		}
		if PetKind(pet.CodenamePrefix+"_017") != pet.Name {
			t.Fatalf("%s codename family unresolved", pet.Name)
		}
	}
	if PetKind("COS_T_HORSE1") != "" {
		t.Fatal("a transport is an attack pet")
	}
}

/*
================
TestPointsLevelsAndSlots
================
*/
func TestPointsLevelsAndSlots(t *testing.T) {
	p := domain.CharacterCOS{Codename: "COS_P_WOLF_001", Level: 1}
	if TrainingPoints(&p) != 1 {
		t.Fatal("level 1 earns one point")
	}
	for _, tc := range []struct{ level, points uint8 }{{2, 2}, {3, 2}, {4, 3}, {40, 21}, {90, 46}} {
		p.Level = tc.level
		if got := TrainingPoints(&p); got != int(tc.points) {
			t.Fatalf("level %d: got %d points, want %d", tc.level, got, tc.points)
		}
	}
	p.Level = uint8(D.Rules.PointsPerPetLevels) * 4
	if TrainingPoints(&p) != 5 {
		t.Fatalf("points %d", TrainingPoints(&p))
	}
	bite := skillNamed(t, "Rending Bite")
	p.Level = bite.UnlockLevel - 1
	if Learn(&p, bite.ID) == "" {
		t.Fatal("learned below the unlock level")
	}
	p.Level = bite.UnlockLevel
	if refusal := Learn(&p, bite.ID); refusal != "" || Level(&p, bite) != 1 {
		t.Fatalf("Rending Bite refused at its unlock level: %s", refusal)
	}
	if Learn(&p, bite.ID) == "" {
		t.Fatal("second level learned without its pet level")
	}
	if Learn(&p, skillNamed(t, "Alpha's Lure").ID) == "" {
		t.Fatal("a planned skill was learned")
	}
	if Learn(&p, skillNamed(t, "Ground Slam").ID) == "" {
		t.Fatal("a wolf learned a bear skill")
	}
	p.Level, p.Training.BonusPoints = 140, 20
	execute, bloodlust := skillNamed(t, "Execute"), skillNamed(t, "Bloodlust")
	Learn(&p, execute.ID)
	Learn(&p, bloodlust.ID)
	if s, ok := SlottedPassive(&p, execute.Row); !ok || s.ID != execute.ID {
		t.Fatal("the first passive of a row is not slotted")
	}
	if Slot(&p, bloodlust.ID) != "" {
		t.Fatal("slot refused")
	}
	if s, _ := SlottedPassive(&p, execute.Row); s.ID != bloodlust.ID {
		t.Fatal("slotting did not replace the row's passive")
	}
	if Queue(&p, execute.ID, 0) == "" || Queue(&p, bite.ID, 0) != "" || p.Training.Queued != bite.ID {
		t.Fatal("only a learned action is queued")
	}
	if Queue(&p, bite.ID, 0) != "" || p.Training.Queued != 0 {
		t.Fatal("asking again does not cancel the queue")
	}
	StartRow(&p.Training, bite.Row, bite, 1, 0, Modifiers{})
	if Queue(&p, bite.ID, 0) == "" {
		t.Fatal("an action on cooldown was queued")
	}
	encoded, _ := json.Marshal(p)
	var restored domain.CharacterCOS
	if json.Unmarshal(encoded, &restored) != nil || !reflect.DeepEqual(restored.Training, p.Training) {
		t.Fatal("training does not persist")
	}
	Refund(&p)
	if TrainingPoints(&p) != 1+140/int(D.Rules.PointsPerPetLevels)+20 {
		t.Fatal("refund did not return every point")
	}
}

/*
================
TestRowCooldownsShareAndCount
================
*/
func TestRowCooldownsShareAndCount(t *testing.T) {
	var training domain.PetTraining
	timer := skillNamed(t, "Rending Bite")
	StartRow(&training, 1, timer, 1, 1000, Modifiers{CooldownPct: 50})
	want := int64(timer.CooldownAt(1) * 500)
	if training.Rows[1].ReadyAtMs != 1000+want || RowReady(&training, 1, 1000+want-1) || !RowReady(&training, 1, 1000+want) {
		t.Fatalf("timer row %+v, want %d", training.Rows[1], 1000+want)
	}
	counter := skillNamed(t, "Hamstring")
	StartRow(&training, 0, counter, 1, 0, Modifiers{})
	left := training.Rows[0].Left
	if left == 0 || training.Rows[0].Kind != TriggerAttacks {
		t.Fatalf("counter row %+v", training.Rows[0])
	}
	CountEvent(&training, TriggerKills)
	CountEvent(&training, TriggerAttacks)
	if training.Rows[0].Left != left-1 || RowReady(&training, 0, 0) {
		t.Fatal("an attack did not count, or a kill did")
	}
}

/*
================
fixedRoller
================
*/
type fixedRoller []float64

func (f *fixedRoller) Float() float64 {
	if len(*f) == 0 {
		return 0
	}
	v := (*f)[0]
	*f = (*f)[1:]
	return v
}

/*
================
TestGearRollsFollowDiablo2Rules

A normal roll has no blue; a rare roll has 3-6 blues of distinct groups,
only implemented, for the slot, within the item's degree; options decode
back and add to the modifiers.
================
*/
func TestGearRollsFollowDiablo2Rules(t *testing.T) {
	d := D.Gear[len(D.Gear)-1]
	normal := fixedRoller{0.999}
	if got := RollGear(d, 0, 0, &normal); len(got) != 0 {
		t.Fatalf("normal roll has blues %v", got)
	}
	for slot := range d.Items {
		for seed := range 20 {
			values := make(fixedRoller, 0, 64)
			values = append(values, 0, 0.99)
			for i := range 60 {
				values = append(values, float64((seed*7+i*13)%100)/100)
			}
			options := RollGear(d, slot, 0, &values)
			if len(options) < 3 || len(options) > D.Rules.MaxBlues {
				t.Fatalf("rare roll has %d blues", len(options))
			}
			groups := map[string]bool{}
			for _, o := range options {
				a, v, ok := DecodeAffix(o)
				if !ok || a.Status != "Implemented" || groups[a.Group] || a.MinDegree > d.Degree || !hasSlot(a, d.Items[slot].Slot) ||
					float64(v) < a.Min || float64(v) > a.Max {
					t.Fatalf("bad blue %+v value %d", a, v)
				}
				groups[a.Group] = true
			}
		}
	}
	first := D.Gear[0]
	low := fixedRoller{0, 0}
	for _, o := range RollGear(first, 0, 0, &low) {
		if a, _, _ := DecodeAffix(o); a.MinDegree > 1 {
			t.Fatal("a degree 1 item rolled a higher tier")
		}
	}
}

/*
================
TestModifiersSumGearAndPassives
================
*/
func TestModifiersSumGearAndPassives(t *testing.T) {
	d := D.Gear[0]
	var phys AffixDef
	for _, a := range D.Affixes {
		if a.Group == "DmgPhys" && a.Tier == 1 {
			phys = a
		}
	}
	p := domain.CharacterCOS{Codename: "COS_P_WOLF_030", Level: 30}
	p.Training.Gear[0] = domain.PetGear{RefObjID: d.Items[0].ID, Options: [6]uint64{EncodeAffix(phys.Code, 7)}}
	p.Training.Gear[1] = domain.PetGear{RefObjID: d.Items[1].ID}
	predator := skillNamed(t, "Predator")
	p.Training.Levels[predator.Index()] = 2
	p.Training.Slotted[predator.Row-ActiveRowCount] = uint8(predator.Option)
	m := ModifiersFor(&p)
	physical, magical := AttackScales(p.Codename, m)
	withoutBlue := m
	withoutBlue.PhysicalPct = 0
	basePhysical, baseMagical := AttackScales(p.Codename, withoutBlue)
	if physical <= basePhysical || magical != baseMagical {
		t.Fatal("physical blue did not improve physical damage only")
	}
	if m.AttackPct != predator.ValueAt(2) || m.DefensePct != 0 || m.PhysicalPct != 7 || m.GearStats.PhysicalAttack != d.Items[0].BaseStats.PhysicalAttack || m.GearStats.PhysicalDefense != d.Items[1].BaseStats.PhysicalDefense {
		t.Fatalf("modifiers %+v", m)
	}
	p.Training.Gear[0].RefObjID = d.Items[1].ID
	if ModifiersFor(&p).AttackPct != predator.ValueAt(2) {
		t.Fatal("a harness worn in the collar slot counted")
	}
}

/*
================
TestTrainingFlagDefaultsOff
================
*/
func TestTrainingFlagDefaultsOff(t *testing.T) {
	for _, value := range []string{"", "off", "true", "1", "unexpected"} {
		t.Setenv(EnvAttackPetExpansion, value)
		if AttackPetExpansionEnabled() {
			t.Fatalf("%q enabled the expansion", value)
		}
	}
	t.Setenv(EnvAttackPetExpansion, "on")
	if !AttackPetExpansionEnabled() {
		t.Fatal("explicit opt-in ignored")
	}
}

/*
================
TestRoarRadiusMeters
================
*/
func TestRoarRadiusMeters(t *testing.T) {
	for _, row := range []struct {
		tier  int
		level uint8
		want  float64
	}{{1, 1, 10}, {1, 10, 19}, {2, 1, 12}, {3, 1, 30}, {3, 3, 32}, {3, 10, 39}} {
		if got := (SkillDef{Option: row.tier}).RoarRadiusMeters(row.level); got != row.want {
			t.Fatalf("tier%d level%d radius=%v want%v", row.tier, row.level, got, row.want)
		}
	}
}
