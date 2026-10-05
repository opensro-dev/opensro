package combat

import "opensro.online/server/internal/game/enterworld"

/*
==================
AccumulateThreat

5903EC..5904D3, once per impact. Each impact updates the SAME action/target
accumulator, so two hits are not equivalent to multiplying the summed
damage once. The percent is applied on the FPU: fild word 1 (unsigned),
fdiv 100.0, fadd 1.0, fmul the accumulator (unsigned), FISTP under
truncation keeping the low dword; then the flat word is added.
(percent / 100 + 1) * acc is not acc * (100 + percent) / 100 when
percent / 100 is inexact.
==================
*/
func AccumulateThreat(previous, damage uint32, modifier enterworld.SkillThreat) uint32 {
	previous += damage
	if !modifier.Present {
		return previous
	}
	return fistpLow((float64(modifier.Percent)/100+1)*float64(previous)) + modifier.Flat
}

/*
==================
SplitLinkedThreat

5A042D..5A0452: fild lkag word 0 (unsigned), fdiv 100.0, fimul the
incoming aggression (signed), CRT_ftol truncating toward zero; the member
keeps the rest, low dword. lkag word 1 is never read. Damage never enters
this transfer.
==================
*/
func SplitLinkedThreat(aggression, percent uint32) (remaining, transferred uint32) {
	transferred = fistpLow(float64(percent) / 100 * float64(int32(aggression)))
	return aggression - transferred, transferred
}
