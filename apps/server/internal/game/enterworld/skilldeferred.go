/*
===========================================================================

skilldeferred.go - the deferred vital instruction on a skill row

A pmhp block whose mode word is not 2 is not installed like a buff: the
server queues it on the caster when the skill engages and its deferred
tick applies it later (combat/deferredinstructions.go). Mode 2 belongs to
the timed-effect installer (skilltimedeffect.go); kind 1 (a negative
parameter write) has no carrier in the v1.150 data and stays unprojected.
The one v1.150 carrier of kind 0 is the anti-bot bomb's attack: a hit on
itself of Percent of its current HP plus Base, at 100 percent.

===========================================================================
*/

package enterworld

/*
================
SkillDeferredVital

One queued self instruction: after the swing that engaged it, the caster
takes Base + Percent percent of its current HP as a hit from nobody, and
its HP recovery latch stays down for DurationMs. The runtime owns the
queue and the latch; this is only the row's words.
================
*/
type SkillDeferredVital struct {
	Present    bool
	DurationMs uint32
	Base       uint32
	Percent    uint32
}

/*
================
parseSkillDeferredVital

Project the row's pmhp block when its mode word is 0 (the deferred
self hit). Descriptor pointers are last-write-wins, so a later block
replaces an earlier one.
================
*/
func parseSkillDeferredVital(fields []string, row *SkillRow) {
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return
	}
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		if op.Tag != tagTimedMaxHPPenalty || op.Count != 4 || op.Arguments[3] != 0 {
			continue
		}
		row.DeferredVital = SkillDeferredVital{Present: true,
			DurationMs: op.Arguments[0], Base: op.Arguments[1], Percent: op.Arguments[2]}
	}
}
