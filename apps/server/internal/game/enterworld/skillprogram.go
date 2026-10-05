package enterworld

import "fmt"

// SkillInstruction is one completely decoded native parameter block. Arguments
// preserve the original 32-bit words, including signed values' bit patterns.
type SkillInstruction struct {
	Tag       uint32
	Column    uint8
	Count     uint8
	Arguments [6]uint32
}

// SkillProgram owns the validated stream. Callers receive values, never its
// backing storage. Decoding does not confer execution or lifecycle support.
type SkillProgram struct{ instructions []SkillInstruction }

func (p SkillProgram) Len() int                           { return len(p.instructions) }
func (p SkillProgram) Instruction(i int) SkillInstruction { return p.instructions[i] }

// CompileSkillProgram is shared by production admission and coverage export.
// Native indexers skip zero words; ssou stops indexing. Unknown instructions
// cannot be treated as zero-argument operations in an executable plan.
func CompileSkillProgram(fields []string) (SkillProgram, error) {
	var p SkillProgram
	if len(fields) != 118 {
		return p, fmt.Errorf("skill program: expected 118 fields, got %d", len(fields))
	}
	for col := skilldataColEncodedTail; col < len(fields); {
		n, ok := textdataInt(fields[col])
		if !ok || n < 0 || n > 0xffffffff {
			return SkillProgram{}, fmt.Errorf("skill program: invalid tag at %d", col)
		}
		if n == 0 {
			col++
			continue
		}
		arity, known := spawnParamSpec(uint32(n))
		if !known {
			return SkillProgram{}, fmt.Errorf("skill program: unknown instruction %x at %d", n, col)
		}
		if arity > 6 || col+arity >= len(fields) {
			return SkillProgram{}, fmt.Errorf("skill program: truncated instruction %x at %d", n, col)
		}
		i := SkillInstruction{Tag: uint32(n), Column: uint8(col), Count: uint8(arity)}
		for a := 0; a < arity; a++ {
			v, valid := textdataDword(fields[col+1+a])
			if !valid {
				return SkillProgram{}, fmt.Errorf("skill program: invalid argument at %d", col+1+a)
			}
			i.Arguments[a] = v
		}
		p.instructions = append(p.instructions, i)
		if i.Tag == 0x73736f75 {
			break
		}
		col += 1 + arity
	}
	return p, nil
}
