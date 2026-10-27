/*
===========================================================================

mercenary_clock.go - native penalty-keeper polling for soldier reuse

The absolute deadline survives logout. Job presence lasts until its ten-second
online poll retires it; merely reading an elapsed deadline cannot
permit another summon or guild-master transfer.

===========================================================================
*/
package action

const mercenaryPenaltyPollSeconds float32 = 10

/*
================
mercenaryPenaltyClock

The native accumulator is a float. It loses only one ten-second interval per
update, including an overdue update. Re-entry starts a fresh accumulator.
================
*/
type mercenaryPenaltyClock struct {
	lastMs  int64
	elapsed float32
}

/*
================
advanceMercenaryCooldowns

The existing owner session roster supplies only admitted online characters.
The division lock serializes the clock with summon and dismissal requests.
================
*/
func (rt *Runtime) advanceMercenaryCooldowns(now int64) {
	rt.petMu.Lock()
	keys := make([]petOwnerKey, 0)
	for key, state := range rt.petSessions {
		if key.gid == 0 && state.ready {
			keys = append(keys, key)
		}
	}
	rt.petMu.Unlock()
	for _, key := range keys {
		rt.advanceMercenaryCooldownInDivision(key, now)
	}
}

/*
================
advanceMercenaryCooldownInDivision
================
*/
func (rt *Runtime) advanceMercenaryCooldownInDivision(key petOwnerKey, now int64) {
	unlock := rt.lockDivision(key.division)
	defer unlock()
	rt.petMu.Lock()
	state := rt.petSessions[key]
	rt.petMu.Unlock()
	if state == nil {
		return
	}
	var deadline int64
	rt.deps.Read(key.division, func() { deadline = state.character.MercenarySummonUntilMs })
	clock := &state.mercenaryPenalty
	if deadline == 0 {
		*clock = mercenaryPenaltyClock{lastMs: now}
		return
	}
	if now <= clock.lastMs {
		return
	}
	clock.elapsed = float32(float64(clock.elapsed) + float64(float32(float64(now-clock.lastMs)/1000)))
	clock.lastMs = now
	if clock.elapsed < mercenaryPenaltyPollSeconds {
		return
	}
	clock.elapsed -= mercenaryPenaltyPollSeconds
	if deadline <= now {
		rt.deps.UpdateDeferred(state.character, "mercenary-cooldown-expire", func() bool {
			if state.character.MercenarySummonUntilMs != deadline {
				return false
			}
			state.character.MercenarySummonUntilMs = 0
			return true
		})
	}
}
