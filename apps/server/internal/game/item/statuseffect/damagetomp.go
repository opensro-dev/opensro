/*
===========================================================================
damagetomp.go - dgmp contribution read through the effect lifecycle owner
The installed descriptor is immutable; hit processing allocates no snapshots.
===========================================================================
*/
package statuseffect

/*
================
DamageToMPPercent

594D28 installs the recipient instance; 582ABA clears it at retirement.
Stop requests do not erase an installed contribution before that phase.
The authored family excludes concurrent ranks through its casting states;
percentages never add. A later admitted installation owns the contribution.
================
*/
func (r *Registry) DamageToMPPercent(division, name string) uint32 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	effects := r.byOwner[ownerKey(division, name)]
	for i := len(effects) - 1; i >= 0; i-- {
		if effects[i].DamageToMP {
			return effects[i].DamageToMPPercent
		}
	}
	return 0
}
