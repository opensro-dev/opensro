/*
===========================================================================

skillimbue.go - complete weapon-coating programs and their impact riders

Chinese forces add elemental damage; Rogue coatings carry only poison.
Both use the persistent imbue owner and the shared abnormal-status resolver.

===========================================================================
*/
package enterworld

// SkillBurn is the bu body: duration power, proc chance, damage-table level.
// 590B12 uses value 2 for probability; 590B7C uses value 1 for duration;
// 590BF2 uses value 3 to index C63C94. These are not interchangeable.
/*
================
SkillBurn
================
*/
type SkillBurn struct{ Power, Chance, Level uint32 }

/*
================
SkillImbue
================
*/
type SkillImbue struct {
	Pinned bool
	Poison bool
	Attack SkillAttack
	Burn   SkillBurn
	// Area is the Lightning Force's efr: an imbue-eligible attack with no
	// area of its own selects its victims with it (586DA8 / 586E1B).
	Area SkillOffensiveArea
}

// Complete dura/att/getv MAAT program with its status rider: the Fire
// Force's bu, the Cold Force's fz and fb, or the Lightning Force's es with
// its efr spread. The imbue hit rolls that rider from row.Abnormal
// (590680); the words are checked here only so a malformed row is refused.
// Admission covers every shipped rank without codename exceptions.
/*
================
parseSkillImbue
================
*/
func parseSkillImbue(fields []string, row *SkillRow) {
	if coating := compilePoisonCoating(fields, *row); coating.Pinned {
		row.Imbue = coating
		return
	}
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "1" || fields[68] != "3" || fields[18] != "1" || row.ChainNext != 0 || !row.Consumption.Pinned || !row.TimingPinned || row.Consumption.HP != 0 || row.Consumption.HPPercent != 0 {
		return
	}
	for _, column := range []int{9, 12, 13, 15, 16, 17, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return
		}
	}
	if fields[50] != "255" || fields[51] != "255" {
		return
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return
	}
	var imbue SkillImbue
	seen := map[uint32]bool{}
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if seen[op.Tag] {
			return
		}
		seen[op.Tag] = true
		a := op.Arguments
		for _, w := range a[:op.Count] {
			if w > 0x7fffffff {
				return
			}
		}
		switch op.Tag {
		case 0x64757261: // dura
			if a[0] == 0 {
				return
			}
		case uint32(skillAttackTag): // att 8 100 min max 100
			if a[0] != 8 || a[1] != 100 || a[2] > a[3] || a[4] != 100 {
				return
			}
			imbue.Attack = SkillAttack{Present: true, Flags: 11, Percent: 100, Min: int64(a[2]), Max: int64(a[3]), Value5: 100, ImpactCount: 1}
		case 0x6275: // bu: power, chance, level
			if a[0] == 0 || a[0] > 65535 || a[1] > 100 || a[2] == 0 || a[2] > 140 {
				return
			}
			imbue.Burn = SkillBurn{a[0], a[1], a[2]}
		case 0x667a, 0x6662, 0x6573: // fz, fb, es: power, chance[, es word]
			if a[0] == 0 || a[1] > 100 {
				return
			}
		case tagEfr: // kind 1 shape radius targets reduction select
			shape := a[1] >= 1 && a[1] <= 4 || a[1] == 6
			if a[0] != 1 || !shape || a[2] == 0 || a[2] > 0xffff || a[1] == 6 && a[2] > 450 ||
				a[3] == 0 || a[3] > 255 || a[4] > 100 || a[5] != 24 {
				return
			}
			imbue.Area = SkillOffensiveArea{Radius: a[2], MaxTargets: uint8(a[3]), ReductionPercent: uint8(a[4]), Shape: uint8(a[1]), Select: uint8(a[5])}
		case tagGetv:
			if a[0] != 0x4d414154 { // MAAT
				return
			}
		default:
			return
		}
	}
	cold := seen[0x667a] || seen[0x6662]
	lightning := seen[0x6573] && seen[tagEfr]
	riders := 0
	for _, r := range []bool{seen[0x6275], cold, lightning} {
		if r {
			riders++
		}
	}
	rider := riders == 1 && seen[0x6573] == seen[tagEfr]
	if !seen[0x64757261] || !imbue.Attack.Present || !seen[tagGetv] || !rider {
		return
	}
	imbue.Pinned = true
	row.Imbue = imbue
}
