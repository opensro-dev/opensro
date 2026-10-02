/*
===========================================================================

skillstatuscast_test.go - admission boundaries of damage-free status casts

The shipped Axis Quiver (tnt2 aggression, ContinueBasicAttack 1) and
Poison Field (two reqi pairs) rows compile to status casts, and the
synthetic programs pin what still has to be refused: two aggression
words, a column 19 that is not a flag, and a repeated non-reqi tag.

===========================================================================
*/

package enterworld

import "testing"

const (
	// testStatusStun is the st block tag (abnormal source 0x7374).
	testStatusStun = 0x7374
)

/*
================
TestShippedStatusCastTiers

Every Axis Quiver and Poison Field tier is a status cast with its authored
area, aggression and equipment pairs.
================
*/
func TestShippedStatusCastTiers(t *testing.T) {
	source := sharedShippedSkills(t)
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	quiverThreat := []uint32{242, 686, 1505, 2958}
	for tier, flat := range quiverThreat {
		code := "SKILL_EU_WARRIOR_DUALA_STUN_A_0" + string(rune('1'+tier))
		row, ok := source.SkillByCodename(code)
		if !ok || !row.StatusCast || !row.DirectOffensePinned || row.Attack.Present || !row.TargetRequired {
			t.Fatalf("%s not a targeted status cast: %q", code, row.OffenseRefusal)
		}
		if row.Threat.Flat != flat || row.Threat.Percent != 0 || !row.ContinueBasicAttack || !row.Abnormal.Stun() {
			t.Fatalf("%s threat %+v continue=%v", code, row.Threat, row.ContinueBasicAttack)
		}
		if a := row.OffensiveArea; a.Shape != 2 || a.Radius != 50 || int(a.MaxTargets) != 3+tier || a.Select != statusCastSelect {
			t.Fatalf("%s area %+v", code, a)
		}
	}
	for tier := 0; tier < 6; tier++ {
		code := "SKILL_EU_ROG_POISONA_ROUND_A_0" + string(rune('1'+tier))
		row, ok := source.SkillByCodename(code)
		if !ok || !row.StatusCast || row.TargetRequired || row.Threat.Present {
			t.Fatalf("%s not an untargeted status cast: %q", code, row.OffenseRefusal)
		}
		r := row.Reqi
		if !r.Present || r.All || r.Count != 2 || r.Pairs[0] != (SkillReqiPair{Kind: 6, Value: 12}) || r.Pairs[1] != (SkillReqiPair{Kind: 6, Value: 13}) {
			t.Fatalf("%s reqi %+v", code, r)
		}
		if a := row.OffensiveArea; a.Shape != 1 || a.Radius != 60 || a.MaxTargets != 5 || a.Select != statusCastSelect {
			t.Fatalf("%s area %+v", code, a)
		}
		if !row.Abnormal.PoisonDamageGetv || !row.Abnormal.PoisonDurationGetv {
			t.Fatalf("%s poison getv not read", code)
		}
	}
}

/*
================
TestStatusCastAdmissionBoundaries

A targeted stun program in the Axis Quiver shape, mutated one way per
case. Only one aggression word, a 0/1 column 19 and repeated reqi pass.
================
*/
func TestStatusCastAdmissionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		mode  string
		valid bool
	}{
		{"tnt2", true},
		{"tant", true},
		{"continue", true},
		{"reqi-twice", true},
		{"tant-and-tnt2", false},
		{"continue-two", false},
		{"duplicate-status", false},
		{"duplicate-tnt2", false},
		{"unknown", false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			tail := []uint32{tagEfr, 1, 2, 50, 3, 0, statusCastSelect, testStatusStun, 5000, 50, 2}
			switch tc.mode {
			case "tant":
				tail = append(tail, tagStatusThreat, 242, 0)
			case "tant-and-tnt2":
				tail = append(tail, tagStatusThreat, 242, 0, tagThreat, 242, 0)
			case "duplicate-tnt2":
				tail = append(tail, tagThreat, 242, 0, tagThreat, 242, 0)
			case "duplicate-status":
				tail = append(tail, testStatusStun, 5000, 50, 2, tagThreat, 242, 0)
			case "reqi-twice":
				tail = append(tail, tagThreat, 242, 0, tagReqi, 6, 12, tagReqi, 6, 13)
			case "unknown":
				tail = append(tail, tagThreat, 242, 0, 0xffffffff)
			default:
				tail = append(tail, tagThreat, 242, 0)
			}
			fields := marchProgramFields(tail)
			fields[68] = "0"
			fields[22], fields[23], fields[29], fields[30] = "1", "1", "1", "1"
			switch tc.mode {
			case "continue":
				fields[statusCastContinueColumn] = "1"
			case "continue-two":
				fields[statusCastContinueColumn] = "2"
			}
			row := SkillRow{TimingPinned: true, Consumption: SkillConsumption{Pinned: true}, ActionRangePinned: true,
				TargetRequired: true, Targets: skillTargetsFromColumns(fields), ActionDurationMs: 1072,
				Abnormal: encodedAbnormalParams(fields)}
			threat, ok := compileSkillStatusCast(fields, row)
			if ok != tc.valid {
				t.Fatalf("admission=%v, want %v", ok, tc.valid)
			}
			if ok && (threat.Flat != 242 || threat.Percent != 0 || threat.Area.MaxTargets != 3) {
				t.Fatalf("compiled %+v", threat)
			}
		})
	}
}
