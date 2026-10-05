/*
===========================================================================

skillthreat.go - aggression modifiers and complete damage-free taunt programs

A taunt produces a target result without dealing HP damage. Keep its route
separate from att so neither an attack roll nor a minimum damage is invented.

===========================================================================
*/

package enterworld

const (
	tagThreat               = 0x746e7432
	tagPhysicalWeaponThreat = 0x70777474
)

/*
================
SkillThreat

tnt2 modifies the result accumulator; pwtt adds the physical weapon term.
Only marks a complete taunt, not the presence of either instruction alone.
================
*/
type SkillThreat struct {
	Present       bool
	Flat, Percent uint32
	Only          bool
	WeaponPercent uint32
	Area          SkillOffensiveArea
	// Decrease is a hostility-lowering program (Discord Wave,
	// skillthreatdecrease.go): dtnt's flat and percent words and mwdt's
	// weapon term, applied to the monsters Area selects around the target.
	// Present stays false: nothing here adds aggression.
	Decrease                      bool
	DecreaseFlat, DecreasePercent uint32
	DecreaseWeaponPercent         uint32
}

/*
================
compileSkillTaunt

58E5F0 admits tnt2 without att and emits a successful zero-damage record.
EFR selects monsters, either around the caster or a required hostile target.
================
*/
func compileSkillTaunt(fields []string, row SkillRow) SkillThreat {
	var out SkillThreat
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || fields[68] != "0" ||
		!row.TimingPinned || !row.Consumption.Pinned || !row.ActionRangePinned ||
		row.ChainSub || row.ChainNext != 0 || row.ActionCastingTimeMs != 0 || row.ActionDurationMs == 0 {
		return out
	}
	for _, column := range []int{15, 16, 17, 19, 20, 24, 25, 26, 27, 28, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return out
		}
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return out
	}
	seen := make(map[uint32]bool)
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if seen[op.Tag] && op.Tag != tagReqi {
			return SkillThreat{}
		}
		seen[op.Tag] = true
		switch op.Tag {
		case tagThreat:
			out.Present, out.Flat, out.Percent = true, op.Arguments[0], op.Arguments[1]
		case tagPhysicalWeaponThreat:
			out.WeaponPercent = op.Arguments[0]
		case tagReqi:
		case tagEfr:
			a := op.Arguments
			if a[0] != 1 || (a[1] != 1 && a[1] != 2) || a[2] == 0 || a[2] > 65535 ||
				a[3] == 0 || a[3] > 255 || a[4] != 0 || a[5] != 16 {
				return SkillThreat{}
			}
			out.Area = SkillOffensiveArea{Shape: uint8(a[1]), Radius: a[2], MaxTargets: uint8(a[3]), Select: 16}
		default:
			return SkillThreat{}
		}
	}
	if !out.Present || !seen[tagPhysicalWeaponThreat] || out.Area.Radius == 0 {
		return SkillThreat{}
	}
	targeted := out.Area.Shape == 2
	if targeted != row.TargetRequired || targeted && row.ActionRange == 0 {
		return SkillThreat{}
	}
	for _, column := range []int{22, 23, 29, 30} {
		want := "0"
		if targeted {
			want = "1"
		}
		if fields[column] != want {
			return SkillThreat{}
		}
	}
	if !targeted && fields[21] != "0" {
		return SkillThreat{}
	}
	out.Only = true
	return out
}
