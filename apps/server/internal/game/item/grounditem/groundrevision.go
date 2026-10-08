/*
===========================================================================

groundrevision.go - bind pickup approaches to their authoritative movement

A delayed collision result can retire only the approach that caused it.
The next command may already occupy the same per-character slot.

===========================================================================
*/
package grounditem

/*
================
ArmGround
================
*/
func (t *PendingTracker) ArmGround(pending Pending) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending[pending.Key] = pending
}

/*
================
ClearGroundRevision
================
*/
func (t *PendingTracker) ClearGroundRevision(key string, revision uint64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if pending, ok := t.pending[key]; ok && revision != 0 && pending.MovementRevision == revision {
		delete(t.pending, key)
		return true
	}
	return false
}
