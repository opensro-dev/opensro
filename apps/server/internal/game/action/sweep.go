package action

import (
	"opensro.online/server/internal/domain"
	"time"

	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// SweepExpired removes every drop whose fixture lifetime elapsed and returns
// the per-division 0x36AB despawn broadcasts, ready for the simulation tick.
//
// The hook rides the shared simulation tick but the sweep honours the declared
// grounditem.SweepInterval cadence (Node's 5s missionGroundItemSweepIntervalMs):
// a drop still expires against its own DroppedAt deadline, the despawn
// broadcast just lands on the first sweep at or after it. The expiry peek
// uses only the registry lock, so an idle visit never takes the maintenance
// barrier; the barrier and commit door engage only when something expired.
func (rt *Runtime) SweepExpired(nowMs int64) []simulation.DivisionFrames {
	next := rt.nextSweepAtMs.Load()
	if nowMs < next {
		return nil
	}
	if !rt.nextSweepAtMs.CompareAndSwap(next, nowMs+grounditem.SweepInterval.Milliseconds()) {
		// A racing sweep claimed this slot (tests drive the hook directly).
		return nil
	}

	now := time.UnixMilli(nowMs)
	if !rt.hasExpired(now) {
		return nil
	}

	rt.maintenance.Lock()
	defer rt.maintenance.Unlock()

	var out []simulation.DivisionFrames
	// The whole sweep is ONE character-less commit (ADR-1 "ttl-sweep"; the
	// registry's own write-through hook retires with the door cutover).
	rt.deps.MutateDeferred(nil, "ttl-sweep", func() {
		for _, divisionID := range rt.Ground.DivisionIDs() {
			expired := rt.Ground.ExpireItems(divisionID, now, grounditem.FixtureLifetime)
			if len(expired) == 0 {
				continue
			}

			for _, item := range expired {
				out = append(out, simulation.DivisionFrames{DivisionID: divisionID, SourceGID: item.Gid, Frames: []simulation.Frame{{
					Opcode:  wire.OpObjectDespawn,
					Scope:   []domain.ObjectScopeChange{{GID: item.Gid}},
					Payload: wire.ObjectDespawn{Gid: item.Gid}.Encode(),
				}}})
			}
		}
	})
	return out
}

// hasExpired is the read-only twin of the sweep's expiry predicate
// (registry ExpireItems: DroppedAt set and now-DroppedAt >= lifetime).
// It holds only the ground registry's own lock. A false negative only delays
// the sweep one interval; a false positive (an expired drop picked up between
// the peek and the sweep's lock) only costs one no-op commit - both self-heal.
func (rt *Runtime) hasExpired(now time.Time) bool {
	for _, divisionID := range rt.Ground.DivisionIDs() {
		if rt.Ground.HasExpired(divisionID, now, grounditem.FixtureLifetime) {
			return true
		}
	}
	return false
}
