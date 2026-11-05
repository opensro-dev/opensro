/*
===========================================================================

skillpulse.go - linked attack result notifications without cast ownership

The v1.150 client reads B3C6 mode 2 at 7757CB. The reference server sends
the same payload under B0BC. Each pulse owns only its result batch.
Mode 1 carries deferred damage ticks instead of a result batch.

===========================================================================
*/

package wire

const OpSkillPulse uint16 = 0xb3c6

const (
	skillDeferredTicks   = 1
	skillPulseResults    = 2
	skillTrapResults     = 3
	maxSkillPulseTargets = 255
	maxDeferredTick      = 0xffffff
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
	return skillResultBatch(NewWriter(11).U8(skillPulseResults).U32(caster).U32(skill), targets)
}

/*
================
SkillDamageTickFrame

One character's deferred damage ticks: fence and Pain Quota shares and
other damage the server applies outside a cast result. The client shows
each tick as a victim label only - no cast, no hit reaction, no HP
change - so the taker's HP must travel on the vitals refresh instead.
The death byte mirrors the life publication; the client takes the death
itself from there.
================
*/
func SkillDamageTickFrame(source uint32, fatal bool, gid uint32, ticks []uint32) Frame {
	if gid == 0 || len(ticks) == 0 || len(ticks) > maxSkillPulseTargets {
		panic("wire: invalid deferred tick target or tick set")
	}
	w := NewWriter(11 + 3*len(ticks)).U8(skillDeferredTicks).U32(source)
	if fatal {
		w.U8(0x80)
	} else {
		w.U8(0)
	}
	w.U32(gid).U8(uint8(len(ticks)))
	for _, tick := range ticks {
		tick = min(tick, maxDeferredTick)
		w.U8(uint8(tick)).U8(uint8(tick >> 8)).U8(uint8(tick >> 16))
	}
	return Frame{Opcode: OpSkillPulse, Payload: w.Payload()}
}

/*
================
SkillTrapResultsFrame

The reference server's trap-result writer writes mode 3 and the trap GID.
The receiver resolves the skill and
explicit effect position from that object's previously published spawn.
================
*/
func SkillTrapResultsFrame(trap uint32, targets []SkillAreaTarget) Frame {
	if trap == 0 || len(targets) == 0 || len(targets) > maxSkillPulseTargets {
		panic("wire: invalid trap result identity or target set")
	}
	return skillResultBatch(NewWriter(7).U8(skillTrapResults).U32(trap), targets)
}

/*
================
skillResultBatch

Both independent result envelopes use the same reference-server target
grammar.
================
*/
func skillResultBatch(w *Writer, targets []SkillAreaTarget) Frame {
	impacts := len(targets[0].Impacts)
	if impacts == 0 || impacts > maxSkillPulseTargets {
		panic("wire: invalid skill pulse impact count")
	}
	w.U8(uint8(impacts)).U8(uint8(len(targets)))
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
