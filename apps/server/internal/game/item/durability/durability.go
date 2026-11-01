// Package durability implements the item mutation contract shared by skill
// equipment damage and repair. It does not send packets or commit a character.
package durability

import "errors"

var ErrNativeDomain = errors.New("durability: native signed assertion domain")
var ErrWriteDenied = errors.New("durability: item record mutation disabled")

func equipment(tid uint16) bool {
	return tid&2 == 0 && tid&0x1c == 0xc && tid&0x60 == 0x20
}

// Broken is the derived equipment +190 flag the server sets, distinct
// from the persistent quantity at instance +38. Exemptions are native TID
// predicates, not an assumption that a zero maximum means indestructible.
func Broken(tid uint16, requested bool) bool {
	family := (tid >> 7) & 15
	return requested && !(equipment(tid) && (family == 5 || family == 12 || family == 13 || family == 14))
}

// State represents just the mutation-owned fields. The owning inventory/store
// must persist Current and Flags and refresh equipment contributions on a zero
// crossing before publishing the build-specific notification.
type State struct {
	Current uint32
	Flags   uint32
	Broken  bool
}

// Offset follows the server's durability offset including low-word
// wrapping, exempt return1, derived flag before the write gate, and
// unchanged-value behavior. Invalid assertion
// domains return an explicit error; native crash-handler continuation is not
// claimed equivalent. Caller serialization/write authority are prerequisites.
func (s *State) Offset(tid uint16, maximum uint32, delta int32, writeAllowed bool) (uint32, error) {
	family := (tid >> 7) & 15
	if equipment(tid) && (family == 5 || family == 12) {
		return 1, nil
	}
	value := s.Current + uint32(delta)
	if int32(value) < 0 || int32(maximum) < 0 {
		return s.Current, ErrNativeDomain
	}
	if value > maximum {
		value = maximum
	}
	s.Broken = Broken(tid, value == 0)
	if value != s.Current {
		if !writeAllowed {
			return value, ErrWriteDenied
		}
		s.Flags |= 4
		s.Current = value
	}
	return value, nil
}
