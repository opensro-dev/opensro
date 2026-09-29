/*
===========================================================================

skillpulse.go - linked attack result notifications without cast ownership

The v1.150 client reads B3C6 mode 2 at 7757CB. The v1.188 server sends the
same payload under B0BC from 59B220. Each pulse owns only its result batch.

===========================================================================
*/

package wire

const OpSkillPulse uint16 = 0xb3c6

const (
	skillPulseResults    = 2
	maxSkillPulseTargets = 255
)

/*
================
SkillPulseFrame

Serialize committed results directly. A pulse has no cast token, target header
or steering byte, and must not reopen the caster's action bracket.
================
*/
func SkillPulseFrame(caster, skill uint32, targets []SkillAreaTarget) Frame {
	if caster == 0 || skill == 0 || len(targets) == 0 || len(targets) > maxSkillPulseTargets {
		panic("wire: invalid skill pulse identity or target set")
	}
	impacts := len(targets[0].Impacts)
	if impacts == 0 || impacts > maxSkillPulseTargets {
		panic("wire: invalid skill pulse impact count")
	}
	w := NewWriter(11).U8(skillPulseResults).U32(caster).U32(skill).U8(uint8(impacts)).U8(uint8(len(targets)))
	for _, target := range targets {
		if target.GID == 0 || len(target.Impacts) != impacts {
			panic("wire: inconsistent skill pulse target")
		}
		w.U32(target.GID)
		for _, impact := range target.Impacts {
			impact.writeTo(w)
		}
	}
	return Frame{Opcode: OpSkillPulse, Payload: w.Payload()}
}
