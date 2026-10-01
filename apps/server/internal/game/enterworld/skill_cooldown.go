/*
===========================================================================

skill_cooldown.go - native reuse timing after action-speed modifiers

The action portion is scaled; the remaining authored reuse delay is not.
Casting handlers retain their authored release boundary independently.

===========================================================================
*/

package enterworld

/*
================
CooldownDurationMs

64C744..64C7E8: only type-two actions with positive duration use keeper 8C.
Preserve the uint32 addition and the three float32 stores before truncation.
The cooldown may exceed the authored value even at 100% when action time
already exceeds reuse time. Neither effect duration nor projectile flight
belongs to this calculation.
================
*/
func (r SkillRow) CooldownDurationMs(percent float32) uint32 {
	if r.ActionKind != 2 || r.ActionDurationMs == 0 {
		return r.CoolTimeMs
	}
	action := float32(r.ActionCastingTimeMs + r.ActionDurationMs)
	remainder := max(float32(0), float32(float64(r.CoolTimeMs)-float64(action)))
	scaled := float32(float64(action) * float64(percent) / 100)
	return uint32(int64(float64(scaled) + float64(remainder)))
}

/*
================
ActionRecoveryDurationMs

64C7F0..64C859 stores the scaled action portion in the common reuse manager.
64C1A0 checks it only for eligible unchained actions in phase mask 80.
================
*/
func (r SkillRow) ActionRecoveryDurationMs(percent float32) uint32 {
	if r.ActionKind != 2 || r.ActionDurationMs == 0 {
		return 0
	}
	action := float32(r.ActionCastingTimeMs + r.ActionDurationMs)
	return uint32(int64(float32(float64(action) * float64(percent) / 100)))
}
