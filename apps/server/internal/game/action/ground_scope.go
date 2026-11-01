/*
===========================================================================

ground_scope.go - which ground drops a viewer is shown

Native publishes a ground item like every other world object: to viewers
whose 320-unit message block neighbours its own (worldgeom.InterestVisible).
The bootstrap, game-ready and a per-tick pass share groundScope, so a drop
enters a scene when its viewer comes into range and leaves it when the
viewer walks away (#617). Before, the bootstrap shipped every drop of the
population and a drop made out of sight never appeared on approach.

A drop's own creation and removal still route through the observers of its
actor (register.go); this owner only settles what those routes miss.

===========================================================================
*/
package action

import (
	"sort"
	"sync"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
================
placedDrop

A drop with its message block, computed once per registry revision and
shared by every viewer of its population. item points into the slice
the drop was placed from (the division's cached copy), so a bucket row
is a pointer and a block, not a copy of the item (#2031). Readers never
mutate either.
================
*/
type placedDrop struct {
	item  *grounditem.Item
	block worldgeom.InterestBlock
}

/*
================
dropBlock
================
*/
func dropBlock(item *grounditem.Item) worldgeom.InterestBlock {
	return worldgeom.InterestBlockAt(worldgeom.RegionXZ{
		RegionID: item.Position.RegionID, X: float64(item.Position.X), Z: float64(item.Position.Z),
	})
}

/*
================
placeDrops
================
*/
func placeDrops(items []grounditem.Item) []placedDrop {
	out := make([]placedDrop, len(items))
	for i := range items {
		out[i] = placedDrop{item: &items[i], block: dropBlock(&items[i])}
	}
	return out
}

/*
================
groundDivisionDrops, groundDropCache

One division's drops as of one registry revision: the deep copy and, per
population, its placement. The registry deep-copies on every read, so the
per-tick pass paid that copy per population per tick (#1943); the cache
pays it once per revision. Revision is one counter across divisions, so
an entry is rebuilt whenever the counter has moved, even by another
division's edit - correct, at worst an extra copy on a changed tick.

The stamp is read BEFORE the copy: a mutation racing the copy raises the
revision past the stamp, so the next pass rebuilds. The stamp is never
newer than the content, so stale content is never served as current.
================
*/
type groundDivisionDrops struct {
	revision uint64
	items    []grounditem.Item
	buckets  map[grounditem.Population]map[worldgeom.InterestBlock][]placedDrop
}

type groundDropCache struct {
	mu        sync.Mutex
	divisions map[string]*groundDivisionDrops
}

/*
================
groundDivisionLocked

The division's cached drops, rebuilt under the cache lock when the
registry revision moved. The caller holds groundDrops.mu.
================
*/
func (rt *Runtime) groundDivisionLocked(division string, revision uint64) *groundDivisionDrops {
	entry := rt.groundDrops.divisions[division]
	if entry == nil || entry.revision != revision {
		entry = &groundDivisionDrops{revision: revision, items: rt.Ground.All(division)}
		if len(entry.items) == 0 {
			// A division with no drops retains no entry, so a closed
			// instance division leaves the cache with its last drop.
			// The empty copy is cheap to rebuild on demand.
			delete(rt.groundDrops.divisions, division)
			return entry
		}
		if rt.groundDrops.divisions == nil {
			rt.groundDrops.divisions = make(map[string]*groundDivisionDrops)
		}
		rt.groundDrops.divisions[division] = entry
	}
	return entry
}

/*
================
groundDivisionSnapshot

The division's cached drops. The returned entry is shared; callers read
it and never mutate it.
================
*/
func (rt *Runtime) groundDivisionSnapshot(division string) *groundDivisionDrops {
	revision := rt.Ground.Revision()
	rt.groundDrops.mu.Lock()
	defer rt.groundDrops.mu.Unlock()
	return rt.groundDivisionLocked(division, revision)
}

/*
================
bucketDrops

The kept drops grouped by message block, so a viewer scans only its
own block, its eight neighbours and its region's block-less drops
instead of the whole population. keep nil keeps every drop.

Counted first, then laid into one backing array: each bucket is a
window of it, so the rows cost one allocation, not an append growth
per bucket, and keep filters in place of a filtered copy (#2031).
Within a bucket the rows keep items' order.
================
*/
func bucketDrops(items []grounditem.Item, keep func(*grounditem.Item) bool) map[worldgeom.InterestBlock][]placedDrop {
	counts := make(map[worldgeom.InterestBlock]int)
	total := 0
	for i := range items {
		if keep == nil || keep(&items[i]) {
			counts[dropBlock(&items[i])]++
			total++
		}
	}
	rows := make([]placedDrop, total)
	buckets := make(map[worldgeom.InterestBlock][]placedDrop, len(counts))
	next := 0
	for block, n := range counts {
		buckets[block] = rows[next : next : next+n]
		next += n
	}
	for i := range items {
		if keep == nil || keep(&items[i]) {
			block := dropBlock(&items[i])
			buckets[block] = append(buckets[block], placedDrop{item: &items[i], block: block})
		}
	}
	return buckets
}

/*
================
groundDropBuckets

The population's drops by message block, from the division's cached
copy: bucketed once per revision and shared by every viewer of the
population.
================
*/
func (rt *Runtime) groundDropBuckets(division string, population grounditem.Population) map[worldgeom.InterestBlock][]placedDrop {
	revision := rt.Ground.Revision()
	rt.groundDrops.mu.Lock()
	defer rt.groundDrops.mu.Unlock()
	entry := rt.groundDivisionLocked(division, revision)
	if buckets, ok := entry.buckets[population]; ok {
		return buckets
	}
	buckets := bucketDrops(entry.items, func(item *grounditem.Item) bool { return item.Population == population })
	if entry.buckets == nil {
		entry.buckets = make(map[grounditem.Population]map[worldgeom.InterestBlock][]placedDrop)
	}
	entry.buckets[population] = buckets
	return buckets
}

/*
================
groundScope

The drops a viewer in block viewer should be shown but is not, and the
published drops it should no longer be shown: out of range, or gone from
the population.

buckets are the population's shared cache rows, read-only here. shown
is the caller's scratch set, cleared on entry, so one map serves every
viewer of a pass. Spawn keeps the old flat walk's order - gid ascending,
as the registry reads - so the bucket visit order never reaches the
wire.
================
*/
func groundScope(buckets map[worldgeom.InterestBlock][]placedDrop, viewer worldgeom.InterestBlock, published []uint32,
	shown map[uint32]struct{},
) ([]grounditem.Item, []uint32) {
	clear(shown)
	for _, gid := range published {
		if gid > domain.GroundItemGIDBase && gid <= domain.GroundItemGIDLimit {
			shown[gid] = struct{}{}
		}
	}
	var spawn []grounditem.Item
	scan := func(drops []placedDrop) {
		for _, drop := range drops {
			if _, exists := shown[drop.item.Gid]; exists {
				delete(shown, drop.item.Gid)
				continue
			}
			spawn = append(spawn, *drop.item)
		}
	}
	if viewer.Whole {
		// A block-less dungeon viewer neighbours every block of its region.
		for block, drops := range buckets {
			if block.Dungeon == viewer.Dungeon {
				scan(drops)
			}
		}
	} else {
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				scan(buckets[worldgeom.InterestBlock{Dungeon: viewer.Dungeon, X: viewer.X + dx, Z: viewer.Z + dz}])
			}
		}
		scan(buckets[worldgeom.InterestBlock{Dungeon: viewer.Dungeon, Whole: true}])
	}
	sort.Slice(spawn, func(i, j int) bool { return spawn[i].Gid < spawn[j].Gid })
	despawn := make([]uint32, 0, len(shown))
	for gid := range shown {
		despawn = append(despawn, gid)
	}
	sort.Slice(despawn, func(i, j int) bool { return despawn[i] < despawn[j] })
	return spawn, despawn
}

/*
================
viewerBlock
================
*/
func viewerBlock(at simulation.Spawn) worldgeom.InterestBlock {
	return worldgeom.InterestBlockAt(worldgeom.RegionXZ{RegionID: at.RegionID, X: at.X, Z: at.Z})
}

/*
================
groundEntryFrame

A drop already lying on the ground, as a viewer coming into range meets it:
the 0x30D7 form with its appear byte clear, so it does not replay the
drop-in. NPCs entering range use the same byte (npcinterest.go).
================
*/
func groundEntryFrame(item grounditem.Item) wire.Frame {
	row := item.SpawnRow(true)
	row.AppearFlag = 0
	return wire.Frame{Opcode: wire.OpSingleObjectSpawn, Payload: row.Encode(),
		Scope: []domain.ObjectScopeChange{{GID: item.Gid, Visible: true}}}
}

/*
================
publishGroundScope

Each change is admitted under the queue lock against the scene's current
scope, not the caller's snapshot: a drop another producer introduced first
is not spawned twice, and one it already removed is not despawned twice.
The caller holds the division's publication gate, so no pickup or drop
commits between the registry read and these sends.
================
*/
func (rt *Runtime) publishGroundScope(s *transport.Session, revision uint64, spawn []grounditem.Item, despawn []uint32) {
	for _, gid := range despawn {
		frames := []wire.Frame{{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: gid}.Encode(),
			Scope: []domain.ObjectScopeChange{{GID: gid}}}}
		if err := s.SendObservedBatch(revision, gid, transportFrames(frames)); err != nil {
			log.Debugf("action: ground despawn for session %d failed: %v", s.ID, err)
			return
		}
	}
	if len(spawn) == 0 {
		return
	}
	// A viewer can only parse a drop whose reference it holds.
	if references := rt.groundReferences(spawn); len(references) != 0 {
		if err := s.SendSceneBatch(revision, transportFrames(references)); err != nil {
			log.Debugf("action: ground references for session %d failed: %v", s.ID, err)
			return
		}
	}
	for _, item := range spawn {
		if err := s.SendUnobservedBatch(revision, item.Gid, transportFrames([]wire.Frame{groundEntryFrame(item)})); err != nil {
			log.Debugf("action: ground spawn for session %d failed: %v", s.ID, err)
			return
		}
	}
}

/*
================
GroundObjectRows

The bootstrap's ground rows: the population's drops in range of the entry
position, by the same predicate as the live pass.
================
*/
func (rt *Runtime) GroundObjectRows(division string, c *enterworld.Character, entry *enterworld.LocalPlayerEntry) []enterworld.Packet {
	if c == nil || entry == nil {
		return nil
	}
	viewer := worldgeom.InterestBlockAt(worldgeom.RegionXZ{
		RegionID: uint16(entry.StartProfile.RegionID), X: entry.StartProfile.X, Z: entry.StartProfile.Z,
	})
	var items []grounditem.Item
	for _, drop := range placeDrops(rt.CharacterGroundItems(division, c)) {
		if viewer.Adjacent(drop.block) {
			items = append(items, *drop.item)
		}
	}
	return enterworld.GroundObjectListRows(items)
}

/*
================
sessionGroundPopulation

The ground population a tick session views. The main world is the zero
population; any other needs a live lease for the session's world.
================
*/
func sessionGroundPopulation(session simulation.SessionView) (grounditem.Population, bool) {
	if session.WorldInstance == domain.DefaultWorldInstance {
		return grounditem.Population{}, true
	}
	if uint32(session.Population.ID) != session.WorldInstance || session.Population.Generation == 0 {
		return grounditem.Population{}, false
	}
	return grounditem.Population{World: session.WorldInstance, Generation: session.Population.Generation}, true
}

/*
================
AdvanceGroundScope

The per-tick pass: every live scene gains the drops that came into its range
and loses those that left it. scene resolves a tick session id to its
transport session and scene revision. Each division's sessions are settled
under that division's publication gate.
================
*/
func (rt *Runtime) AdvanceGroundScope(nowMs int64, sessions []simulation.SessionView,
	scene func(sessionID string) (*transport.Session, uint64, bool),
) {
	byDivision := make(map[string][]simulation.SessionView)
	for _, session := range sessions {
		if session.PublishedObjects != nil {
			byDivision[session.DivisionID] = append(byDivision[session.DivisionID], session)
		}
	}
	divisions := make([]string, 0, len(byDivision))
	for division := range byDivision {
		divisions = append(divisions, division)
	}
	sort.Strings(divisions)
	for _, division := range divisions {
		rt.advanceDivisionGroundScope(division, byDivision[division], nowMs, scene)
	}
}

/*
================
advanceDivisionGroundScope
================
*/
func (rt *Runtime) advanceDivisionGroundScope(division string, sessions []simulation.SessionView, nowMs int64,
	scene func(sessionID string) (*transport.Session, uint64, bool),
) {
	unlock := rt.LockPublication(division, "groundScope")
	defer unlock()
	shown := make(map[uint32]struct{})
	for _, session := range sessions {
		population, ok := sessionGroundPopulation(session)
		if !ok {
			continue
		}
		spawn, despawn := groundScope(rt.groundDropBuckets(division, population),
			viewerBlock(session.World.LiveSpawnAt(nowMs)), session.PublishedObjects, shown)
		if len(spawn) == 0 && len(despawn) == 0 {
			continue
		}
		s, revision, live := scene(session.SessionID)
		if !live {
			continue
		}
		rt.publishGroundScope(s, revision, spawn, despawn)
	}
}
