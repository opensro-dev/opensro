/*
===========================================================================

skilloffense_test.go - direct offense and projectile admission

Coverage of the shipped direct offense skills and the complete shapes their
admission requires.

===========================================================================
*/
package enterworld

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestShippedDirectOffenseCoverage
================
*/
func TestShippedDirectOffenseCoverage(t *testing.T) {
	dir := licensed.RetailTextdataDir(t)
	if _, err := os.Stat(filepath.Join(dir, "skilldata.txt")); err != nil {
		t.Skip("shipped media unavailable")
	}
	source := NewTextdataSkills(dir)
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	admitted, attackRows, comboRoots, comboStages := 0, 0, 0, 0
	for _, row := range source.rows.values() {
		if (!strings.HasPrefix(row.Codename, "SKILL_CH_") && !strings.HasPrefix(row.Codename, "SKILL_EU_")) || row.ChainSub {
			continue
		}
		if row.Attack.Present {
			attackRows++
		}
		if row.DirectOffensePinned {
			admitted++
			// A status cast, a pdmg hit and an lfst hit are the complete
			// offenses without att.
			if !row.Consumption.Pinned || row.ChainNext != 0 || !row.CombatPinned && !row.StatusCast && !row.FixedDamage.Present && !row.LifeSteal.Present {
				t.Fatalf("incomplete admitted skill %s", row.Codename)
			}
		}
		if row.ChainNext != 0 {
			if stages, ok := OffensiveSequence(source, row.ID); ok {
				comboRoots++
				comboStages += len(stages)
			}
		}
	}
	if admitted == 0 {
		t.Fatal("no shipped offensive roots admitted")
	}
	t.Logf("Supported direct-damage player rows (including base attacks): %d; player attack roots: %d", admitted, attackRows)
	if comboRoots == 0 {
		t.Fatal("no complete shipped combo graphs admitted")
	}
	t.Logf("Complete supported player combo roots: %d; linked stages: %d", comboRoots, comboStages)
}

/*
================
TestDirectOffenseRequiresCompleteSupportedShape
================
*/
func TestDirectOffenseRequiresCompleteSupportedShape(t *testing.T) {
	for _, mode := range []string{"direct", "unknown-tail", "duplicate-attack", "chain", "hp-cost", "malformed-cost", "negative-mp", "shared-reuse", "trailing-data"} {
		t.Run(mode, func(t *testing.T) {
			fields := make([]string, 118)
			for i := range fields {
				fields[i] = "0"
			}
			fields[0], fields[53] = "1", "19"
			fields[69] = strconv.FormatInt(skillAttackTag, 10)
			fields[70], fields[71], fields[72], fields[73], fields[74] = "5", "143", "15", "18", "143"
			row := SkillRow{CombatPinned: true, TimingPinned: true, ActionRangePinned: true, TargetRequired: true}
			switch mode {
			case "unknown-tail":
				fields[75] = "999999"
			case "duplicate-attack":
				copy(fields[75:81], fields[69:75])
			case "chain":
				row.ChainNext = 123
			case "hp-cost":
				fields[52] = "1"
			case "malformed-cost":
				fields[53] = "19x"
			case "negative-mp":
				fields[53] = "-1"
			case "shared-reuse":
				fields[15] = "1"
			case "trailing-data":
				fields[117] = "1"
			}
			parseSkillOffense(fields, &row)
			// An HP cost is admitted and carried to the charge (58E1B6 /
			// 58312C; action/skillcost.go).
			if row.DirectOffensePinned != (mode == "direct" || mode == "hp-cost") {
				t.Fatalf("unexpected admission: %+v", row)
			}
			if mode == "hp-cost" && row.Consumption.HP != 1 {
				t.Fatalf("HP cost %d, want 1", row.Consumption.HP)
			}
		})
	}
}

/*
================
TestProjectileAdmissionRequiresCompleteSingleArrowShape
================
*/
func TestProjectileAdmissionRequiresCompleteSingleArrowShape(t *testing.T) {
	for _, branch := range []string{"arrow", "zero-count", "multi-arrow", "wrong-family", "zero-speed", "bad-speed", "overflow-speed", "combo", "area", "multi-impact", "missing-cnsm", "thrown-blade", "thrown-blade-with-bow", "extra-effect"} {
		t.Run(branch, func(t *testing.T) {
			fields := make([]string, 118)
			for i := range fields {
				fields[i] = "0"
			}
			fields[0], fields[16] = "1", "400"
			copy(fields[69:], []string{"6386804", "6", "150", "13", "18", "150", "1668182893", "4", "1", "1"})
			row := SkillRow{CombatPinned: true, TimingPinned: true, ActionRangePinned: true, TargetRequired: true, ActionCastingTimeMs: 300, RequiredWeaponKinds: [2]uint8{6, 255}, Attack: SkillAttack{Present: true, ImpactCount: 1}}
			switch branch {
			case "zero-count":
				fields[78] = "0"
			case "multi-arrow":
				fields[78] = "2"
			case "wrong-family":
				fields[77] = "2"
			case "zero-speed":
				fields[16] = "0"
			case "bad-speed":
				fields[16] = "-1"
			case "overflow-speed":
				fields[16] = "4294967296"
			case "combo":
				row.ChainNext = 2
			case "area":
				row.OffensiveArea = SkillOffensiveArea{Radius: 20}
			case "multi-impact":
				row.Attack.ImpactCount = 2
			case "missing-cnsm":
				for i := 75; i < 79; i++ {
					fields[i] = "0"
				}
			case "thrown-blade", "thrown-blade-with-bow":
				// SKILL_CH_SWORD_SPECIAL_*: no cnsm, sword or blade.
				for i := 75; i < 79; i++ {
					fields[i] = "0"
				}
				row.RequiredWeaponKinds = [2]uint8{2, 3}
				if branch == "thrown-blade-with-bow" {
					row.RequiredWeaponKinds[1] = 6
				}
			case "extra-effect":
				fields[79] = "99999"
			}
			row.ProjectileSpeed = textdataU32(fields[16]) // same projection as the production loader
			parseSkillOffense(fields, &row)
			// An area no longer disqualifies an arrow: the bow's pierce and
			// special shots resolve their area at release (action/skillarea.go);
			// the area's own shape is the efr parser's to validate.
			// Several impacts (BOW_CHAIN) resolve at release and spend
			// count x impacts arrows (action/ammunition.go).
			if row.DirectOffensePinned != (branch == "arrow" || branch == "area" || branch == "thrown-blade" || branch == "multi-impact") {
				t.Fatalf("%s admission %+v", branch, row)
			}
		})
	}
}
