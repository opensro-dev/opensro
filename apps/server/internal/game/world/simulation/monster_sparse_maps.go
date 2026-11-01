package simulation

import "time"

// Deleting entries does not release a map's peak backing table. Rebuild only
// after a large sustained contraction, with at most 4096 surviving entries.
// Peak tracking and a 4:1 threshold avoid churn at ordinary region boundaries.
func compactSparseMap[V any](m map[uint32]V, peak *int, allow bool) map[uint32]V {
	n := len(m)
	if n > *peak {
		*peak = n
	}
	if !allow || *peak < 8192 || n > 4096 || n*4 > *peak {
		return m
	}
	next := make(map[uint32]V, n)
	for k, v := range m {
		next[k] = v
	}
	*peak = n
	return next
}

// Called by the population owner. Archive work has priority. The time check
// bounds admission, not an individual map copy or GC pause.
func (state *divisionMonsterState) compactDormantIndexes(deadline time.Time) {
	allow := len(state.archiveQueue) == 0
	state.instances.shrinkHotExcess(&state.sparseMapPeaks[0], allow && time.Now().Before(deadline))
	state.aiTimers = compactSparseMap(state.aiTimers, &state.sparseMapPeaks[1], allow && time.Now().Before(deadline))
	state.behavior.byGID = compactSparseMap(state.behavior.byGID, &state.sparseMapPeaks[2], allow && time.Now().Before(deadline))
	if allow && time.Now().Before(deadline) {
		state.behavior.shrinkExcess()
	}
}
