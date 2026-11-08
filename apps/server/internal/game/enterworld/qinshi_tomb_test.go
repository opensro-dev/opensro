/*
===========================================================================

qinshi_tomb_test.go - verification of Qin-Shi Emperor Tomb monster skills and summon references

Tests both SRO_QINSHI_TOMB off (native default) and on: 238 monster skills
mapped into the v1.150 skill ID gaps (10565..10738, 25047..25073,
30612..30647, 30686) and full foreign-key closure across all 79 Tomb monster
DefaultSkillIDs and summon references.

===========================================================================
*/

package enterworld

import (
	"strconv"
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/tier"
)

// generalAttack07 names the eight imported fourth skills (#2948).
var generalAttack07 = map[string]bool{
	"MSKILL_TQ_TOMBGENERAL1_ATTACK07":  true,
	"MSKILL_TQ_TOMBGENERAL2_ATTACK07":  true,
	"MSKILL_TQ_TOMBGENERAL3_ATTACK07":  true,
	"MSKILL_TQ_TOMBGENERAL4_ATTACK07":  true,
	"MSKILL_TQ_SNAKEGENERAL1_ATTACK07": true,
	"MSKILL_TQ_SNAKEGENERAL2_ATTACK07": true,
	"MSKILL_TQ_SNAKEGENERAL3_ATTACK07": true,
	"MSKILL_TQ_SNAKEGENERAL4_ATTACK07": true,
}

/*
================
TestQinShiTombEmbeddedSkills
================
*/
func TestQinShiTombEmbeddedSkills(t *testing.T) {
	rows := monster.SplitTabbedText(qinshiTombSkillDataTSV)
	if got, want := len(rows), 238; got != want {
		t.Fatalf("embedded tomb skill rows = %d, want %d", got, want)
	}
	// The generals' fourth skills (iSRO-R _RefSkill 31183..31190, #2948)
	// ride along as full 118-column rows under the gap IDs 10731..10738.
	// (Other tomb families have their own _ATTACK07 attacks; only the
	// generals' are the imported sumn rows.)
	attack07 := 0
	for _, fields := range rows {
		if len(fields) != 118 {
			t.Fatalf("tomb skill row %s has %d columns, want 118", fields[3], len(fields))
		}
		if generalAttack07[fields[3]] {
			attack07++
			if fields[69] != "1936945013" {
				t.Fatalf("%s encoded tail starts with %s, want the sumn tag 1936945013", fields[3], fields[69])
			}
		}
	}
	if attack07 != 8 {
		t.Fatalf("embedded ATTACK07 rows = %d, want 8", attack07)
	}
}

/*
================
TestQinShiTombSkillsAndMonsterDefaultSkills
================
*/
func TestQinShiTombSkillsAndMonsterDefaultSkills(t *testing.T) {
	tier.Long(t, "every Qin-Shi tomb skill and monster default skill resolves")
	clientdata.RequireGameData(t)
	dir := clientdata.RetailTextdataDir(t)

	t.Setenv(monster.EnvQinShiTomb, "off")
	baseSkills := NewTextdataSkills(dir)
	baseCount := baseSkills.Len()
	if baseCount == 0 {
		t.Fatal("expected non-empty base skill table")
	}
	if _, ok := baseSkills.SkillByID(10565); ok {
		t.Fatal("base v1.150 skill table must not contain Tomb skill 10565 when gate is off")
	}

	t.Setenv(monster.EnvQinShiTomb, "on")
	tombSkills := NewTextdataSkills(dir)
	if got, want := tombSkills.Len()-baseCount, 238; got != want {
		t.Fatalf("added tomb skills = %d, want %d", got, want)
	}

	baseMonsters := monster.LoadTemplate(dir)
	withTomb, err := baseMonsters.WithQinShiTomb(dir)
	if err != nil {
		t.Fatalf("WithQinShiTomb: %v", err)
	}
	withSummons, err := WithMonsterSummonReferences(withTomb, tombSkills)
	if err != nil {
		t.Fatalf("WithMonsterSummonReferences: %v", err)
	}

	// Every DefaultSkillID on every Tomb monster must resolve in tombSkills.
	for id, ref := range withSummons.Refs {
		if _, inBase := baseMonsters.Refs[id]; inBase {
			continue
		}
		for _, skillID := range ref.DefaultSkillIDs {
			if skillID == 0 {
				continue
			}
			if _, ok := tombSkills.SkillByID(skillID); !ok {
				t.Fatalf("tomb monster %d (%s) references missing skill %d", id, ref.Codename, skillID)
			}
		}
	}
}

/*
================
TestQinShiTombGeneralFourthSkills

The Tomb and Snake Generals' fourth default skill (#2948): iSRO-R
assigns each variant MSKILL_TQ_*_ATTACK07 in DefaultSkill_4 where
v1.188 assigns three skills. All sixteen general refs (base, clones
and the MOB_QT_ quest copies) must hold exactly four nonzero default
skills, the fourth resolving to that variant's ATTACK07 row, and every
species the ATTACK07 sumn tuples name must exist as a Tomb ref.
================
*/
func TestQinShiTombGeneralFourthSkills(t *testing.T) {
	tier.Long(t, "Qin-Shi generals carry their iSRO-R fourth skill")
	clientdata.RequireGameData(t)
	dir := clientdata.RetailTextdataDir(t)

	t.Setenv(monster.EnvQinShiTomb, "on")
	tombSkills := NewTextdataSkills(dir)
	baseMonsters := monster.LoadTemplate(dir)
	withTomb, err := baseMonsters.WithQinShiTomb(dir)
	if err != nil {
		t.Fatalf("WithQinShiTomb: %v", err)
	}

	wantFourth := map[string]string{
		"MOB_TQ_TOMBGENERAL":        "MSKILL_TQ_TOMBGENERAL1_ATTACK07",
		"MOB_TQ_TOMBGENERAL_CLON":   "MSKILL_TQ_TOMBGENERAL2_ATTACK07",
		"MOB_TQ_TOMBGENERAL_CLON2":  "MSKILL_TQ_TOMBGENERAL3_ATTACK07",
		"MOB_TQ_TOMBGENERAL_CLON3":  "MSKILL_TQ_TOMBGENERAL4_ATTACK07",
		"MOB_TQ_SNAKEGENERAL":       "MSKILL_TQ_SNAKEGENERAL1_ATTACK07",
		"MOB_TQ_SNAKEGENERAL_CLON":  "MSKILL_TQ_SNAKEGENERAL2_ATTACK07",
		"MOB_TQ_SNAKEGENERAL_CLON2": "MSKILL_TQ_SNAKEGENERAL3_ATTACK07",
		"MOB_TQ_SNAKEGENERAL_CLON3": "MSKILL_TQ_SNAKEGENERAL4_ATTACK07",
		"MOB_QT_TQ_TOMBGENERAL1":    "MSKILL_TQ_TOMBGENERAL1_ATTACK07",
		"MOB_QT_TQ_TOMBGENERAL2":    "MSKILL_TQ_TOMBGENERAL2_ATTACK07",
		"MOB_QT_TQ_TOMBGENERAL3":    "MSKILL_TQ_TOMBGENERAL3_ATTACK07",
		"MOB_QT_TQ_TOMBGENERAL4":    "MSKILL_TQ_TOMBGENERAL4_ATTACK07",
		"MOB_QT_TQ_SNAKEGENERAL1":   "MSKILL_TQ_SNAKEGENERAL1_ATTACK07",
		"MOB_QT_TQ_SNAKEGENERAL2":   "MSKILL_TQ_SNAKEGENERAL2_ATTACK07",
		"MOB_QT_TQ_SNAKEGENERAL3":   "MSKILL_TQ_SNAKEGENERAL3_ATTACK07",
		"MOB_QT_TQ_SNAKEGENERAL4":   "MSKILL_TQ_SNAKEGENERAL4_ATTACK07",
	}
	checked := 0
	for _, ref := range withTomb.Refs {
		want, general := wantFourth[ref.Codename]
		if !general {
			continue
		}
		checked++
		nonzero := 0
		for _, skillID := range ref.DefaultSkillIDs {
			if skillID != 0 {
				nonzero++
			}
		}
		if nonzero != 4 {
			t.Fatalf("%s holds %d default skills, want 4", ref.Codename, nonzero)
		}
		fourth, ok := tombSkills.SkillByID(ref.DefaultSkillIDs[3])
		if !ok {
			t.Fatalf("%s fourth skill %d does not resolve", ref.Codename, ref.DefaultSkillIDs[3])
		}
		if fourth.Codename != want {
			t.Fatalf("%s fourth skill is %s, want %s", ref.Codename, fourth.Codename, want)
		}
	}
	if checked != len(wantFourth) {
		t.Fatalf("checked %d general refs, want %d", checked, len(wantFourth))
	}

	// Every species an ATTACK07 sumn tuple names must be a Tomb ref.
	for _, fields := range monster.SplitTabbedText(qinshiTombSkillDataTSV) {
		if !generalAttack07[fields[3]] {
			continue
		}
		for tuple := 0; tuple < 9; tuple++ {
			refID, err := strconv.ParseUint(fields[70+tuple*4], 10, 32)
			if err != nil {
				t.Fatalf("%s sumn tuple %d ref %q: %v", fields[3], tuple, fields[70+tuple*4], err)
			}
			if refID == 0 {
				continue
			}
			if _, ok := withTomb.Refs[uint32(refID)]; !ok {
				t.Fatalf("%s sumn tuple %d names missing Tomb ref %d", fields[3], tuple, refID)
			}
		}
	}
}
