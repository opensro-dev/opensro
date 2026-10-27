package monster

// AllowsTargetStatus is the observer-dependent part of the reference
// server's target-actor validation, after entity validity and LIFE==1. The
// RTTI-bound getters inspect the actor's packed TID; they are not player/GM
// labels. The second getter is a subset of the first for these shared
// implementations. Keep the bit predicate intact.
//
// Target status comes from the character state owner's byte 3, not
// posture, mounted state, a synthetic protection mask, or the actor's flags.
// Meanings/producers of numeric states remain separately audited.
func AllowsTargetStatus(actorTID uint16, referenceFlags uint32, status uint8) bool {
	if actorTID&2 != 0 && actorTID&0x1c == 4 && actorTID&0x60 == 0x40 && actorTID&0x780 == 0x180 {
		return true
	}
	if status >= 2 && status <= 4 {
		return false
	}
	return (status != 6 && status != 7) || referenceFlags&0x200 != 0
}
