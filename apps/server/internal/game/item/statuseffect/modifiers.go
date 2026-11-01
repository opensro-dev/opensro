package statuseffect

import (
	"fmt"
	"math"

	"opensro.online/server/internal/game/paramkeeper"
)

// Modifiers is an immutable installation-time program. Its private storage
// makes Effect copies safe across every registry boundary. Source identities
// are assigned by the registry, never borrowed from wire instance tokens.
type Modifiers struct{ data *modifierData }

// HasWrites identifies applications that own parameter contributions, including
// zero-valued entries: native removal still notifies when such an entry exists.
func (m Modifiers) HasWrites() bool { return m.data != nil && len(m.data.writes) != 0 }

type modifierData struct {
	writes []paramkeeper.Write
	source uint32
}

// NewModifiers freezes authored contributions. Source must be zero on input;
// the final combat graph additionally validates its supported parameter set.
// Preserve write order, including repeated writes to the same parameter.
func NewModifiers(writes []paramkeeper.Write) (Modifiers, error) {
	if len(writes) == 0 {
		return Modifiers{}, nil
	}
	for _, w := range writes {
		if w.Source != 0 || w.Parameter >= 512 || w.Channel > paramkeeper.FactorProduct || math.IsNaN(float64(w.Value)) || math.IsInf(float64(w.Value), 0) {
			return Modifiers{}, fmt.Errorf("statuseffect: invalid modifier program")
		}
	}
	return Modifiers{&modifierData{writes: append([]paramkeeper.Write(nil), writes...)}}, nil
}

// bindModifiersLocked gives every installation a fresh identity. Keys are
// deterministic port ownership, not native allocator-address ordering. Never
// wrap and reuse an identity: an exhausted registry refuses installation.
func (r *Registry) bindModifiersLocked(e *Effect) bool {
	if e.Modifiers.data == nil {
		return true
	}
	if r.nextModifierSource == 0 {
		r.nextModifierSource = 0x80000000
	}
	if r.nextModifierSource > math.MaxUint32 {
		return false
	}
	e.Modifiers = Modifiers{&modifierData{writes: e.Modifiers.data.writes, source: uint32(r.nextModifierSource)}}
	r.nextModifierSource++
	return true
}

// ModifierWrites returns detached installed contributions. A stop request or
// elapsed timer does not itself erase native parameter entries: retirement
// owns removal (the retirement callback and the parameter-source removal
// callback). Consequently
// this query deliberately does not filter StopRequested or read a clock.
func (r *Registry) ModifierWrites(division, name string) []paramkeeper.Write {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []paramkeeper.Write
	for _, e := range r.byOwner[ownerKey(division, name)] {
		if e.Modifiers.data == nil {
			continue
		}
		for _, w := range e.Modifiers.data.writes {
			w.Source = e.Modifiers.data.source
			out = append(out, w)
		}
	}
	return out
}
