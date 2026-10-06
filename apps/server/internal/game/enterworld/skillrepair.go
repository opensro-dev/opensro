/*
===========================================================================

skillrepair.go - complete item-owned fortress repair programs

The item handler (49CC80) invokes the ordinary persistent skill lifecycle
5830B0. Its linked recipient receives percentage healing on each puls.

===========================================================================
*/
package enterworld

/*
================
SkillStructureRepair
================
*/
type SkillStructureRepair struct {
	Pinned                          bool
	DurationMs, PeriodMs, HPPercent uint32
}

/*
================
compileStructureRepair

Admit the complete shipped program, including link capacity, cancellation,
activity and rpkt. A partial heal block must not authorize a repair kit.
================
*/
func compileStructureRepair(fields []string, row SkillRow) SkillStructureRepair {
	var out SkillStructureRepair
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "1" || fields[68] != "3" ||
		!row.ReplacementPinned || !row.TimingPinned || row.ChainNext != 0 ||
		row.ActionCastingTimeMs != 0 || !row.CastGate.Rpkt {
		return out
	}
	program, err := CompileSkillProgram(fields)
	if err != nil || program.Len() != 7 {
		return out
	}
	tags := [...]uint32{0x64757261, 0x70756c73, 0x6865616c, 0x6c6e6b73, 0x736b63, 0x616f, 0x72706b74}
	for i, tag := range tags {
		op := program.Instruction(i)
		if op.Tag != tag {
			return SkillStructureRepair{}
		}
		switch i {
		case 0:
			out.DurationMs = op.Arguments[0]
		case 1:
			out.PeriodMs = op.Arguments[0]
		case 2:
			if op.Arguments[0] != 0 || op.Arguments[2] != 0 || op.Arguments[3] != 0 {
				return SkillStructureRepair{}
			}
			out.HPPercent = op.Arguments[1]
		case 3:
			if op.Arguments[0] != 0 || op.Arguments[1] != 0 || op.Arguments[2] != 1 || op.Arguments[3] != 1 {
				return SkillStructureRepair{}
			}
		case 4:
			if op.Arguments[0] != 15 || op.Arguments[1] != 8 || op.Arguments[2] != 0 {
				return SkillStructureRepair{}
			}
		}
	}
	out.Pinned = out.DurationMs != 0 && out.PeriodMs != 0 && out.HPPercent > 0 && out.HPPercent <= 100
	return out
}

/*
================
ItemUseNativeFields

CIFDelayInfoRow_InitCountdown mode 4 resolves the first item-owned skill
and reads its authored duration. Publish that resolved reference on both
bootstrap and incremental item-reference paths.
================
*/
func ItemUseNativeFields(ref *ItemRef, skills SkillDataSource) NativeFields {
	fields := ref.NativeFields
	if ref.TypeIDs != [4]int64{3, 3, 1, 10} {
		return fields
	}
	source, ok := skills.(interface{ SkillByCodename(string) (SkillRow, bool) })
	if !ok {
		return fields
	}
	skill, ok := source.SkillByCodename(ref.AssociatedSkillCodename)
	if !ok || !skill.StructureRepair.Pinned {
		return fields
	}
	return fields.With("useSkillId", float64(skill.ID)).With("useSkillDurationMs", float64(skill.StructureRepair.DurationMs))
}
