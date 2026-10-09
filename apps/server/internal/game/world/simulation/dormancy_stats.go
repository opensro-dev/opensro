/*
===========================================================================

dormancy_stats.go - simulation dormancy stats ownership

===========================================================================
*/

package simulation

// DormancyStats reads counters only; it never expands the complete population.
/*
================
DormancyStats
================
*/
func (s *MonsterState) DormancyStats(division string) map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int{}
	for _, key := range s.populationKeys() {
		if key.division != division {
			continue
		}
		d := s.populationForLease(key.division, key.lease)
		if d == nil {
			continue
		}
		out["resident"] += d.instances.len()
		out["archived"] += len(d.instances.cold)
		out["archivePending"] += len(d.archiveQueue)
		out["sleeping"] += len(d.dormant)
		out["scheduled"] += d.behavior.Len()
		out["liveAITimers"] += len(d.aiTimers)
		out["storedAITimers"] += len(d.storedAITimers)
		for _, r := range d.movers.records() {
			if r.pending != nil {
				out["compactPending"]++
			} else {
				out["liveMovers"]++
			}
		}
	}
	return out
}
