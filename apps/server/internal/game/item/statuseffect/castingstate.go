package statuseffect

// UpdateActive implements the server's casting-state update. Each nonzero
// low-three-byte
// index sets or clears one manager bit. Removal is unconditional, including
// repeated installations of the same index; this is not reference counting.
// 1D and 23 are stored normally even though the conflict reader ignores them.
// Lifecycle owners must call this at native installation/cleanup transitions,
// rather than reconstructing Active from presentation rows.
func (s *CastingConflictSnapshot) UpdateActive(packed uint32, remove bool) {
	for shift := uint(0); shift < 24; shift += 8 {
		state := uint8(packed >> shift)
		if state == 0 {
			continue
		}
		mask := uint64(1) << (state % 64)
		if remove {
			s.Active[state/64] &^= mask
		} else {
			s.Active[state/64] |= mask
		}
	}
}

func (r *Registry) changeEffectStatesLocked(key string, effect Effect, remove bool) {
	s := r.castingStates[key]
	words := effect.InstalledStates
	if remove {
		words = effect.RetirementStates
	}
	for _, packed := range words {
		s.UpdateActive(packed, remove)
	}
	if s == (CastingConflictSnapshot{}) {
		delete(r.castingStates, key)
	} else {
		r.castingStates[key] = s
	}
}

// UnlinkedStateOperations selects the ordinary no-link-context branches of
// the server's persistent-cast lifecycle and contribution retirement.
// Cleanup can clear
// a word that installation skipped (mode-2 dttp without ovl2). Link-context
// owners must select their separate source/area branches instead.
func UnlinkedStateOperations(d ReplacementDescriptor, mode uint8, durable bool) (install, retire [2]uint32) {
	install[0], retire[0] = d.PackedStates, d.PackedStates
	if d.Ovl2Present {
		install[1], retire[1] = d.Ovl2, d.Ovl2
	}
	if !durable && mode == 2 {
		if d.Ovl2Present {
			install[0], retire[0] = d.Ovl2, d.Ovl2
		} else if d.DttpPresent {
			install[0] = 0
		}
	}
	return
}

// CastingStates returns the lifecycle-owned active bits. Current-command words
// remain the action controller's responsibility; this registry does not invent
// a current cast from an attached-effect row.
func (r *Registry) CastingStates(division, name string) CastingConflictSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.castingStates[ownerKey(division, name)]
}
