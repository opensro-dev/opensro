package domain

// BodyStatusTransition is applied only while the character mutation door is
// held. Owner identifies a live effect application, not its reusable wire token.
// Zero denotes an explicit status setter (including a GM toggle).
type BodyStatusTransition struct {
	Value       uint8
	Owner       uint64
	RetireOwner uint64
}

// TransitionBodyStatus keeps retirement conditional on ownership. Native effect
// retirement and a type-4 timed restoration are different operations: this is
// the former and must not be used to coalesce the native restoration timers.
func (c *Character) TransitionBodyStatus(t BodyStatusTransition) bool {
	if c == nil || t.Value > 7 || t.Value == 1 || (t.RetireOwner != 0 && c.BodyStatusOwner != t.RetireOwner) {
		return false
	}
	changed := c.NativeBodyStatus != t.Value
	c.NativeBodyStatus, c.BodyStatusOwner = t.Value, t.Owner
	return changed
}

// GMToggleBodyStatus follows the server's GM body-status toggle. Values are
// named only at this producer: /INVISIBLE requests 4 and /INVINCIBLE
// requests 3.
func GMToggleBodyStatus(current, requested uint8) (uint8, bool) {
	if requested != 3 && requested != 4 {
		return current, false
	}
	if current == requested {
		return 0, true
	}
	switch current {
	case 1, 2, 5, 6:
		return current, false
	}
	return requested, true
}

// InitialCOSBodyStatus follows the server's owned-actor admission:
// only owner states 3 and 4 are inherited by the new actor.
func InitialCOSBodyStatus(owner uint8) uint8 {
	if owner == 3 || owner == 4 {
		return owner
	}
	return 0
}
