/*
===========================================================================

skillwall.go - the Chinese Force walls (Crystal Wall, Fire Wall)

A wall row is exactly onff + pw. pw (+0x2B4) is {mask, pool, defense,
parry}:

	mask     the server normalizes 1/2/3 to |0x0C and 4/8/12 to |0x03;
	         bit 4 covers the physical lane, bit 8 the magical lane
	pool     the server copies it into the context's remaining counter
	         (+0x5C +8); the absorb record's +1C carries it as the maximum
	defense  the damage formulas subtract it from the attack point
	parry    the same formulas divide the attack point by (1 + parry/100)

onff keeps it standing: every onff period the caster pays onff word 1 MP
or the wall retires, and the server retires it once the counter is 0.

===========================================================================
*/

package enterworld

// SkillWall is one admitted pw block, mask already normalized.
type SkillWall struct {
	Pinned               bool
	Mask                 uint32
	Pool, Defense, Parry uint32
}

// Covers reports the wall absorbing the lane selected by an attack flag
// (4 physical, 8 magical).
func (w SkillWall) Covers(flag uint32) bool { return w.Mask&flag != 0 }

// normalizeLaneMask is the lane-mask rule the server's parameter index applies to pw
// and br: 4/8/12 gain both attack kinds
// (|3), 1/2/3 gain both lanes (|12).
func normalizeLaneMask(mask uint32) uint32 {
	switch mask {
	case 4, 8, 12:
		return mask | 3
	case 1, 2, 3:
		return mask | 12
	}
	return mask
}

func parseSkillWall(fields []string, row *SkillRow) {
	if len(fields) != 118 || fields[0] != "1" || row.ChainNext != 0 || !row.Consumption.Pinned ||
		!row.TimingPinned || row.Consumption.HP != 0 || row.Consumption.HPPercent != 0 {
		return
	}
	// A wall is cast on the caster alone, with no target or weapon.
	for column := 21; column <= 33; column++ {
		if fields[column] != "0" {
			return
		}
	}
	if fields[50] != "255" || fields[51] != "255" {
		return
	}
	program, err := CompileSkillProgram(fields)
	if err != nil || program.Len() != 2 {
		return
	}
	var wall SkillWall
	onff := false
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		switch op.Tag {
		case 0x6f6e6666: // onff: period, MP
			onff = op.Count == 2 && op.Arguments[0] != 0
		case 0x7077: // pw
			a := op.Arguments
			if op.Count != 4 || a[1] == 0 || a[1] > 0xffff || a[2] > 0x7fffffff || a[3] > 0x7fffffff {
				return
			}
			wall = SkillWall{Mask: normalizeLaneMask(a[0]), Pool: a[1], Defense: a[2], Parry: a[3]}
		}
	}
	if !onff || wall.Mask&12 == 0 || row.Aura.PulseMs == 0 {
		return
	}
	wall.Pinned = true
	row.Wall = wall
}
