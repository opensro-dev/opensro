/*
===========================================================================

placementguard.go - report a player placed inside an object no walk can enter

BR-261014-1425-CE1E: a player stood inside a closed tree trunk (region
0x5F6A, asset 1160) and every move was refused. A walk can never enter such
an object (movement/placementguard.go), so some other code path placed
them there. Every non-walk position write (a teleport, a rebirth, a ride,
a knockback) reports itself here after its write-back; a placement inside
a closed object logs a throttled Warn naming the path and counts in the
metrics' placement_inside_object block. It changes no position.

===========================================================================
*/
package action

import (
	"strings"
	"sync"
	"sync/atomic"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

// placementWarnQuietMs is the per character and path Warn interval.
const placementWarnQuietMs = 60000

/*
================
placementGuard

The placement counters by path and the Warn throttle.
================
*/
type placementGuard struct {
	mu     sync.Mutex
	counts map[string]*atomic.Uint64
	last   map[string]int64
}

/*
================
Runtime.notePlacement

Called after a non-walk write-back of c's position by the path named in
reason. Reads the persisted pose; nil InsideClosedObject disables it.
================
*/
func (rt *Runtime) notePlacement(c *enterworld.Character, reason string) {
	if rt.InsideClosedObject == nil || c == nil || c.World == nil || c.World.Spawn == nil {
		return
	}
	spawn := c.World.Spawn
	if spawn.RegionID == nil || spawn.X == nil || spawn.Y == nil || spawn.Z == nil {
		return
	}
	pose := simulation.Spawn{RegionID: uint16(*spawn.RegionID), X: *spawn.X, Y: *spawn.Y, Z: *spawn.Z}
	asset, inside := rt.InsideClosedObject(pose)
	if !inside {
		return
	}
	rt.placements.mu.Lock()
	if rt.placements.counts == nil {
		rt.placements.counts = make(map[string]*atomic.Uint64)
		rt.placements.last = make(map[string]int64)
	}
	counter, ok := rt.placements.counts[reason]
	if !ok {
		counter = new(atomic.Uint64)
		rt.placements.counts[reason] = counter
	}
	counter.Add(1)
	now := rt.Now().UnixMilli()
	key := strings.ToLower(c.Name) + "/" + reason
	warn := now-rt.placements.last[key] >= placementWarnQuietMs
	if warn {
		rt.placements.last[key] = now
	}
	rt.placements.mu.Unlock()
	if warn {
		log.WithFields(log.Fields{"character": c.Name, "path": reason, "asset": asset,
			"region": pose.RegionID, "x": pose.X, "y": pose.Y, "z": pose.Z}).
			Warn("action: player placed inside a closed object")
	}
}

/*
================
Runtime.PlacementCounters

The metrics' placement_inside_object block: placements inside a closed
object since start, by the path that made them.
================
*/
func (rt *Runtime) PlacementCounters() map[string]uint64 {
	rt.placements.mu.Lock()
	defer rt.placements.mu.Unlock()
	out := make(map[string]uint64, len(rt.placements.counts))
	for reason, counter := range rt.placements.counts {
		out[reason] = counter.Load()
	}
	return out
}
