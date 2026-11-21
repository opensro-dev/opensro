/*
===========================================================================

self_effect.go - buff instances a monster carries

===========================================================================
*/

package monster

/*
================
SelfEffect

Installation values, independent of later actor stats. Only complete
token-bearing, phase-free self programs enter this projection. UntilMs 0
is a persistent effect (the Roc's mom enrage): it lasts while the monster
lives, and Steps counts its growth (First the period, Second the value
per step).
================
*/
type SelfEffect struct {
	SkillID, Token, Tag, First, Second uint32
	StartedAtMs, UntilMs               int64
	Steps                              uint32
}
type SelfEffects [8]SelfEffect

// TargetEffects are buff instances another actor installed on the monster
// (Vital Spot's bbuf, enterworld/skillhostiledebuff.go): each keeps its own
// token and clock, as a self effect does, but no conditional slot owns it.
type TargetEffects [8]SelfEffect

/*
================
Active
================
*/
func (e SelfEffect) Active(now int64) bool {
	if e.Persistent() {
		return true
	}
	return e.Token != 0 && uint32(now-e.StartedAtMs) <= uint32(e.UntilMs-e.StartedAtMs)
}

/*
================
Persistent

A live effect with no end of its own: it retires with its monster.
================
*/
func (e SelfEffect) Persistent() bool {
	return e.Token != 0 && e.UntilMs == 0
}

/*
================
StepsAt

A persistent stepped effect's count at now: one when it lands and one
more each full period after. The v1.188 server's persistent pass adds a
step when a period has passed since the last one; on the tick clock that
is this count.
================
*/
func (e SelfEffect) StepsAt(now int64) uint32 {
	if !e.Persistent() || e.First == 0 || now < e.StartedAtMs {
		return 0
	}
	return 1 + uint32((now-e.StartedAtMs)/int64(e.First))
}
