/*
===========================================================================

retirement.go - reading an allocated layer's evacuation state

Retirement begins while residents remain. The final release request is a
later transition, so consumers must read the layer's state rather than infer
it from the manager's release inbox. A lease pins the allocated lifetime.

===========================================================================
*/

package instance

/*
================
Retiring

Whether this exact lifetime is evacuating. The second result distinguishes
a missing or replaced lifetime from an allocated layer that is still open.
================
*/
func (r *Registry) Retiring(lease Lease) (bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, layer := r.resolve(lease)
	if layer == nil {
		return false, false
	}
	return layer.retirementMode != 0, true
}
