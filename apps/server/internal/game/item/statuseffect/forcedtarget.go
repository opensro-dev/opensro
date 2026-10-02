/*
===========================================================================

forcedtarget.go - the active hitm constraint owned by the effect registry

The target GID is part of its effect, so expiry, cancellation, death and
replacement cannot leave a second target-lock cache behind.

===========================================================================
*/
package statuseffect

/*
================
ForcedTarget
================
*/
func (r *Registry) ForcedTarget(division, name string, now int64) uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, effect := range r.byOwner[ownerKey(division, name)] {
		if effect.State == StateActive && effect.ForcedTargetGID != 0 && !effect.StopRequested && !effect.Expired(now) {
			return effect.ForcedTargetGID
		}
	}
	return 0
}

/*
================
ForcedTargets

Return value snapshots; the runtime checks source presence outside this lock.
================
*/
func (r *Registry) ForcedTargets() []Effect {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Effect
	for _, rows := range r.byOwner {
		for _, effect := range rows {
			if effect.ForcedTargetGID != 0 && !effect.StopRequested {
				out = append(out, effect)
			}
		}
	}
	return out
}

/*
================
StopForcedTarget

A stale presence check cannot retire a replacement with a different token.
================
*/
func (r *Registry) StopForcedTarget(effect Effect) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := ownerKey(effect.DivisionID, effect.CharacterName)
	for i := range r.byOwner[key] {
		row := &r.byOwner[key][i]
		if row.InstanceToken == effect.InstanceToken && row.ForcedTargetGID == effect.ForcedTargetGID {
			r.requestReplacementStopLocked(key, i)
		}
	}
}

/*
================
replaceForcedTargetLocked

5936D5 replaces the single hitm owner even across different skill groups.
Queue the old instance's wire retirement as well as clearing its live flag.
================
*/
func (r *Registry) replaceForcedTargetLocked(key string, effect Effect) {
	if effect.ForcedTargetGID == 0 {
		return
	}
	for i, old := range r.byOwner[key] {
		if old.ForcedTargetGID != 0 && old.InstanceToken != effect.InstanceToken {
			r.requestReplacementStopLocked(key, i)
		}
	}
}
