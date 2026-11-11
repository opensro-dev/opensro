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
	"cmp"
	"maps"
	"slices"
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

A drop with its message block, computed when the drop enters the
division's cache and shared by every viewer of its population. item
points into the slice the drop was placed from (the registry's copy or
its delta), so a bucket row is a pointer and a block, not a copy of the
item (#2031). Readers never mutate either.
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
groundDivisionDrops

One division's drops as of one registry revision, by population and
message block. It is immutable once published and shared by every
viewer of the division; a new revision is a new value, so its identity
pins the drop set (groundScopeMark relies on that).
================
*/
type groundDivisionDrops struct {
	revision uint64
	buckets  map[grounditem.Population]map[worldgeom.InterestBlock][]placedDrop
}

// dropPlace is where a cached drop lies: its population and block.
type dropPlace struct {
	population grounditem.Population
	block      worldgeom.InterestBlock
}

/*
================
groundDivisionState, groundDropCache

The cache's own record of one division: the published view and, per
gid, the place its row lies in. The index is the cache's working
state, read and written only under groundDropCache.mu; readers get
the view.

The registry deep-copies on every read, so the per-tick pass paid
that copy per population per tick (#1943), then once per revision of
its own division (#2030). A revision is one drop landing or one
pickup, and the copy was still of every drop: 6% of the live shard's
allocation (#3164). The cache now asks the registry what changed
since its revision (grounditem division_delta.go) and derives the
next view from the last: the touched blocks' rows are copied, every
other block's rows and every unchanged item are shared with the view
before. When the registry cannot say - the first read, a reader
further behind than its log, a Restore - the view is built whole,
as before.

Each read takes the revision and the content it belongs to under one
registry lock, so a view is never stamped newer than its content.
================
*/
type groundDivisionState struct {
	view  *groundDivisionDrops
	index map[uint32]dropPlace
}

type groundDropCache struct {
	mu        sync.Mutex
	divisions map[string]*groundDivisionState
}

/*
================
groundDivisionLocked

The division's current view: the cached one while the registry's
revision stands, the next one derived from it when the registry can
name what changed, a whole rebuild otherwise. A division with no
drops retains no state, so a closed instance division leaves the
cache with its last drop; its empty view is cheap to rebuild on
demand. The caller holds groundDrops.mu.
================
*/
func (rt *Runtime) groundDivisionLocked(division string) *groundDivisionDrops {
	state := rt.groundDrops.divisions[division]
	if state != nil {
		if delta, ok := rt.Ground.DivisionChangesSince(division, state.view.revision); ok {
			if delta.Revision == state.view.revision {
				return state.view
			}
			state.apply(delta)
			if len(state.index) == 0 {
				delete(rt.groundDrops.divisions, division)
			}
			return state.view
		}
	}
	items, revision := rt.Ground.DivisionSnapshot(division)
	state = newGroundDivisionState(items, revision)
	if len(items) == 0 {
		delete(rt.groundDrops.divisions, division)
		return state.view
	}
	if rt.groundDrops.divisions == nil {
		rt.groundDrops.divisions = make(map[string]*groundDivisionState)
	}
	rt.groundDrops.divisions[division] = state
	return state.view
}

/*
================
newGroundDivisionState

A division's state built whole from the registry's copy: every
population bucketed, every gid indexed.
================
*/
func newGroundDivisionState(items []grounditem.Item, revision uint64) *groundDivisionState {
	state := &groundDivisionState{
		view:  &groundDivisionDrops{revision: revision},
		index: make(map[uint32]dropPlace, len(items)),
	}
	populations := make(map[grounditem.Population]struct{})
	for i := range items {
		item := &items[i]
		state.index[item.Gid] = dropPlace{population: item.Population, block: dropBlock(item)}
		populations[item.Population] = struct{}{}
	}
	if len(populations) == 0 {
		return state
	}
	state.view.buckets = make(map[grounditem.Population]map[worldgeom.InterestBlock][]placedDrop, len(populations))
	for population := range populations {
		state.view.buckets[population] = bucketDrops(items, func(item *grounditem.Item) bool {
			return item.Population == population
		})
	}
	return state
}

/*
================
groundDivisionState.apply

Derive the next view from the current one and the registry's delta.
The outer maps and the rows of each touched block are copied once
(copy on write); everything else is shared with the view before,
which stays exactly as its readers hold it. Rows keep gid order
within a block, as a whole build lays them.
================
*/
func (state *groundDivisionState) apply(delta grounditem.DivisionDelta) {
	build := groundDeltaBuild{
		buckets: maps.Clone(state.view.buckets),
		blocks:  make(map[grounditem.Population]bool),
		rows:    make(map[dropPlace]bool),
	}
	if build.buckets == nil {
		build.buckets = make(map[grounditem.Population]map[worldgeom.InterestBlock][]placedDrop)
	}
	for _, gid := range delta.Removed {
		if place, held := state.index[gid]; held {
			build.remove(place, gid)
			delete(state.index, gid)
		}
	}
	for i := range delta.Changed {
		item := &delta.Changed[i]
		if place, held := state.index[item.Gid]; held {
			build.remove(place, item.Gid)
		}
		place := dropPlace{population: item.Population, block: dropBlock(item)}
		build.insert(place, item)
		state.index[item.Gid] = place
	}
	state.view = &groundDivisionDrops{revision: delta.Revision, buckets: build.buckets}
}

/*
================
groundDeltaBuild

The next view's buckets while a delta is applied. blocks and rows
say which block maps and which row slices this build already owns;
anything else still belongs to the view before and is copied before
its first write.
================
*/
type groundDeltaBuild struct {
	buckets map[grounditem.Population]map[worldgeom.InterestBlock][]placedDrop
	blocks  map[grounditem.Population]bool
	rows    map[dropPlace]bool
}

/*
================
groundDeltaBuild.own

The place's rows, owned by this build: the population's block map and
the block's rows are copied on their first write.
================
*/
func (b *groundDeltaBuild) own(place dropPlace) []placedDrop {
	blocks := b.buckets[place.population]
	if blocks == nil || !b.blocks[place.population] {
		blocks = maps.Clone(blocks)
		if blocks == nil {
			blocks = make(map[worldgeom.InterestBlock][]placedDrop)
		}
		b.buckets[place.population] = blocks
		b.blocks[place.population] = true
	}
	rows := blocks[place.block]
	if !b.rows[place] {
		rows = slices.Clone(rows)
		b.rows[place] = true
	}
	return rows
}

/*
================
groundDeltaBuild.put

Store the place's rows; a block left empty leaves its map, and a
population left empty leaves the view.
================
*/
func (b *groundDeltaBuild) put(place dropPlace, rows []placedDrop) {
	blocks := b.buckets[place.population]
	if len(rows) != 0 {
		blocks[place.block] = rows
		return
	}
	delete(blocks, place.block)
	if len(blocks) == 0 {
		delete(b.buckets, place.population)
	}
}

/*
================
groundDeltaBuild.remove, insert
================
*/
func (b *groundDeltaBuild) remove(place dropPlace, gid uint32) {
	rows := b.own(place)
	at, found := slices.BinarySearchFunc(rows, gid, func(row placedDrop, gid uint32) int {
		return cmp.Compare(row.item.Gid, gid)
	})
	if found {
		rows = slices.Delete(rows, at, at+1)
	}
	b.put(place, rows)
}

func (b *groundDeltaBuild) insert(place dropPlace, item *grounditem.Item) {
	rows := b.own(place)
	at, _ := slices.BinarySearchFunc(rows, item.Gid, func(row placedDrop, gid uint32) int {
		return cmp.Compare(row.item.Gid, gid)
	})
	b.put(place, slices.Insert(rows, at, placedDrop{item: item, block: place.block}))
}

/*
================
groundDivisionSnapshot

The division's current view. It is shared; callers read it and never
mutate it.
================
*/
func (rt *Runtime) groundDivisionSnapshot(division string) *groundDivisionDrops {
	rt.groundDrops.mu.Lock()
	defer rt.groundDrops.mu.Unlock()
	return rt.groundDivisionLocked(division)
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

The population's drops by message block, from the division's current
view: shared by every viewer of the population, read-only.
================
*/
func (rt *Runtime) groundDropBuckets(division string, population grounditem.Population) map[worldgeom.InterestBlock][]placedDrop {
	rt.groundDrops.mu.Lock()
	defer rt.groundDrops.mu.Unlock()
	return rt.groundDivisionLocked(division).buckets[population]
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
	rt.groundScopePrune(sessions)
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
		drops := rt.groundDivisionSnapshot(division)
		buckets := rt.groundDropBuckets(division, population)
		block := viewerBlock(session.World.LiveSpawnAt(nowMs))
		if rt.groundScopeSettled(session, division, population, drops, block) {
			continue
		}
		spawn, despawn := groundScope(buckets, block, session.PublishedObjects, shown)
		if len(spawn) == 0 && len(despawn) == 0 {
			rt.groundScopeSettle(session, division, population, drops, block)
			continue
		}
		// Changes are outstanding until the scene admits them into
		// the session's published set; the next pass recomputes
		// rather than trusting a mark over unadmitted scope.
		rt.groundScopeForget(session.SessionID)
		s, revision, live := scene(session.SessionID)
		if !live {
			continue
		}
		rt.publishGroundScope(s, revision, spawn, despawn)
	}
}

/*
================
groundScopeMark

One viewer's settled ground scope (#3101): the division's cached
drops entry it was computed from - the entry is rebuilt on every
registry revision, so its identity pins the drop set - the viewer's
division, identity, population and block, and the published set the
pass settled on. groundScope is a pure function of the entry's
buckets for the population, the block and the published set, so
while they all stand its spawn/despawn diff is empty and the
nine-block walk is skipped. The entry is read before the buckets:
a drop write racing the two reads leaves the mark naming the older
entry, and the next pass recomputes.
================
*/
type groundScopeMark struct {
	division    string
	characterID int64
	population  grounditem.Population
	drops       *groundDivisionDrops
	block       worldgeom.InterestBlock
	published   []uint32
}

/*
================
groundScopeSettled

Whether the viewer's last settled pass ran on exactly these inputs.
================
*/
func (rt *Runtime) groundScopeSettled(session simulation.SessionView, division string, population grounditem.Population,
	drops *groundDivisionDrops, block worldgeom.InterestBlock,
) bool {
	rt.groundScopeMu.Lock()
	defer rt.groundScopeMu.Unlock()
	mark, ok := rt.groundScopeMarks[session.SessionID]
	return ok && mark.division == division && mark.characterID == session.CharacterID &&
		mark.population == population && mark.drops == drops && mark.block == block &&
		slices.Equal(mark.published, session.PublishedObjects)
}

/*
================
groundScopeSettle

Record a pass whose diff was empty, keeping a copy of the published
set it settled on.
================
*/
func (rt *Runtime) groundScopeSettle(session simulation.SessionView, division string, population grounditem.Population,
	drops *groundDivisionDrops, block worldgeom.InterestBlock,
) {
	rt.groundScopeMu.Lock()
	defer rt.groundScopeMu.Unlock()
	if rt.groundScopeMarks == nil {
		rt.groundScopeMarks = map[string]groundScopeMark{}
	}
	mark := rt.groundScopeMarks[session.SessionID]
	mark.division, mark.characterID, mark.population = division, session.CharacterID, population
	mark.drops, mark.block = drops, block
	mark.published = append(mark.published[:0], session.PublishedObjects...)
	rt.groundScopeMarks[session.SessionID] = mark
}

/*
================
groundScopeForget
================
*/
func (rt *Runtime) groundScopeForget(sessionID string) {
	rt.groundScopeMu.Lock()
	defer rt.groundScopeMu.Unlock()
	delete(rt.groundScopeMarks, sessionID)
}

/*
================
groundScopePrune

Drop the marks of sessions this pass did not see, so the table
holds only live sessions.
================
*/
func (rt *Runtime) groundScopePrune(sessions []simulation.SessionView) {
	rt.groundScopeMu.Lock()
	defer rt.groundScopeMu.Unlock()
	if len(rt.groundScopeMarks) == 0 {
		return
	}
	seen := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		seen[session.SessionID] = true
	}
	for sessionID := range rt.groundScopeMarks {
		if !seen[sessionID] {
			delete(rt.groundScopeMarks, sessionID)
		}
	}
}
