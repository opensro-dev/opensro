/*
===========================================================================

ground_scope_cache_test.go - the placed-drop cache stays the flat walk

The per-tick pass used to read a fresh registry copy per population and
walk every drop for every viewer. The cache (#1943) must deliver the
same spawn/despawn sets: the oracle below is that old flat walk, kept
verbatim, and the seeded run compares both paths over adds, removes and
viewer moves.

===========================================================================
*/
package action

import (
	"math/rand"
	"reflect"
	"slices"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/grounditem"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
referenceGroundScope

The pre-#1943 flat walk, kept verbatim as the golden test's oracle: the
shown set from the published gids, a linear scan over every placed drop,
the leftovers despawned in gid order.
================
*/
func referenceGroundScope(drops []placedDrop, viewer worldgeom.InterestBlock, published []uint32) ([]grounditem.Item, []uint32) {
	shown := make(map[uint32]struct{})
	for _, gid := range published {
		if gid > domain.GroundItemGIDBase && gid <= domain.GroundItemGIDLimit {
			shown[gid] = struct{}{}
		}
	}
	var spawn []grounditem.Item
	for _, drop := range drops {
		if !viewer.Adjacent(drop.block) {
			continue
		}
		if _, exists := shown[drop.item.Gid]; exists {
			delete(shown, drop.item.Gid)
			continue
		}
		spawn = append(spawn, *drop.item)
	}
	despawn := make([]uint32, 0, len(shown))
	for gid := range shown {
		despawn = append(despawn, gid)
	}
	slices.Sort(despawn)
	return spawn, despawn
}

/*
================
TestGroundScopeCacheMatchesTheFlatWalk

A seeded field of 300 drops across the main world and one instanced
population, six viewers per population, 40 ticks of seeded adds, removes
and viewer moves. Every viewer, every tick, the cached path must spawn
and despawn exactly what the flat walk does, in the same order.
================
*/
func TestGroundScopeCacheMatchesTheFlatWalk(t *testing.T) {
	const division = "golden"
	rng := rand.New(rand.NewSource(1943))
	rt := &Runtime{Ground: grounditem.NewRegistry()}
	populations := []grounditem.Population{{}, {World: 0x00030002, Generation: 1}}
	live := map[grounditem.Population][]uint32{}
	addDrop := func(population grounditem.Population) {
		item := rt.Ground.Add(division, grounditem.Item{
			RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Population: population,
			Position: grounditem.Point{RegionID: 0x6a9a,
				X: float32(rng.Intn(5000)), Z: float32(rng.Intn(5000))},
		})
		live[population] = append(live[population], item.Gid)
	}
	for i := 0; i < 300; i++ {
		addDrop(populations[rng.Intn(len(populations))])
	}
	type goldenViewer struct {
		population grounditem.Population
		x, z       float64
		published  []uint32
	}
	var viewers []goldenViewer
	for _, population := range populations {
		for v := 0; v < 6; v++ {
			viewers = append(viewers, goldenViewer{population: population,
				x: float64(rng.Intn(5000)), z: float64(rng.Intn(5000))})
		}
	}
	// One scratch set for every viewer of every pass, as the tick pass
	// runs it: the reused map must never leak one viewer's set into
	// the next.
	shown := make(map[uint32]struct{})
	for tick := 0; tick < 40; tick++ {
		for m := rng.Intn(9); m > 0; m-- {
			population := populations[rng.Intn(len(populations))]
			if gids := live[population]; len(gids) > 0 && rng.Intn(2) == 0 {
				i := rng.Intn(len(gids))
				rt.Ground.Remove(division, gids[i])
				live[population] = slices.Delete(gids, i, i+1)
			} else {
				addDrop(population)
			}
		}
		for vi := range viewers {
			viewer := &viewers[vi]
			if tick%7 == 3 {
				viewer.x += float64(rng.Intn(641) - 320)
				viewer.z += float64(rng.Intn(641) - 320)
			}
			block := viewerBlock(simulation.Spawn{RegionID: 0x6a9a, X: viewer.x, Z: viewer.z})
			drops := rt.groundDropBuckets(division, viewer.population)
			spawn, despawn := groundScope(drops, block, viewer.published, shown)
			wantSpawn, wantDespawn := referenceGroundScope(
				placeDrops(rt.Ground.AllInPopulation(division, viewer.population)), block, viewer.published)
			if !slices.EqualFunc(spawn, wantSpawn, func(a, b grounditem.Item) bool { return a.Gid == b.Gid }) ||
				!slices.Equal(despawn, wantDespawn) {
				t.Fatalf("tick %d viewer %d: cached scope = %d spawn / %d despawn, flat walk = %d / %d",
					tick, vi, len(spawn), len(despawn), len(wantSpawn), len(wantDespawn))
			}
			next := viewer.published[:0]
			gone := make(map[uint32]bool, len(despawn))
			for _, gid := range despawn {
				gone[gid] = true
			}
			for _, gid := range viewer.published {
				if !gone[gid] {
					next = append(next, gid)
				}
			}
			for _, item := range spawn {
				next = append(next, item.Gid)
			}
			viewer.published = next
		}
	}
}

/*
================
TestGroundScopeDungeonBlocksMatchTheFlatWalk

The dungeon wing of the golden: region 0x8F01 with a message grid
installed, so its drops sit in grid cells except one outside the grid,
whose block is the whole region; two more dungeons without grids and
one outdoor region. A gridded viewer must see the whole-region drop of
its own dungeon (the bucket walk's Whole scan), a whole-region viewer
must see every drop of its dungeon, and no viewer may see another
dungeon's drops. Every tick is also checked against the flat walk.
================
*/
func TestGroundScopeDungeonBlocksMatchTheFlatWalk(t *testing.T) {
	restore := worldgeom.InstallDungeonMessageGrids(map[uint16]worldgeom.DungeonMessageGrid{
		0x8F01: {Min: [3]float64{-320, -1000, -320}, Max: [3]float64{1600, 1000, 1600}},
	})
	defer restore()
	const division = "dungeon"
	rt := &Runtime{Ground: grounditem.NewRegistry()}
	add := func(region uint16, x, z float32) grounditem.Item {
		return rt.Ground.Add(division, grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02",
			Position: grounditem.Point{RegionID: region, X: x, Z: z}})
	}
	cell := add(0x8F01, 0, 0)          // the viewer's own grid cell
	farCell := add(0x8F01, 1500, 1500) // a far cell of the same dungeon
	whole := add(0x8F01, 5000, 5000)   // outside the grid: the whole region
	second := add(0x8F02, 0, 0)        // a second dungeon, no grid installed
	third := add(0x8F03, 0, 0)         // a third
	outdoor := add(0x6a9a, 0, 0)

	viewers := []struct {
		name  string
		spawn simulation.Spawn
		want  []uint32 // tick 0's spawns, ascending
	}{
		{"gridded", simulation.Spawn{RegionID: 0x8F01, X: 0, Z: 0},
			[]uint32{cell.Gid, whole.Gid}},
		{"whole-region", simulation.Spawn{RegionID: 0x8F01, X: 5000, Z: 5000},
			[]uint32{cell.Gid, farCell.Gid, whole.Gid}},
		{"second dungeon", simulation.Spawn{RegionID: 0x8F02, X: 0, Z: 0},
			[]uint32{second.Gid}},
		{"third dungeon", simulation.Spawn{RegionID: 0x8F03, X: 0, Z: 0},
			[]uint32{third.Gid}},
		{"outdoor", simulation.Spawn{RegionID: 0x6a9a, X: 0, Z: 0},
			[]uint32{outdoor.Gid}},
	}
	published := make([][]uint32, len(viewers))
	shown := make(map[uint32]struct{})
	for tick := 0; tick < 3; tick++ {
		if tick == 1 {
			rt.Ground.Remove(division, whole.Gid)
		}
		buckets := rt.groundDropBuckets(division, grounditem.Population{})
		for vi, viewer := range viewers {
			block := viewerBlock(viewer.spawn)
			spawn, despawn := groundScope(buckets, block, published[vi], shown)
			wantSpawn, wantDespawn := referenceGroundScope(
				placeDrops(rt.Ground.AllInPopulation(division, grounditem.Population{})), block, published[vi])
			gotGids := make([]uint32, 0, len(spawn))
			for _, item := range spawn {
				gotGids = append(gotGids, item.Gid)
			}
			wantGids := make([]uint32, 0, len(wantSpawn))
			for _, item := range wantSpawn {
				wantGids = append(wantGids, item.Gid)
			}
			if !slices.Equal(gotGids, wantGids) || !slices.Equal(despawn, wantDespawn) {
				t.Fatalf("tick %d viewer %s: cached scope = %v spawn / %v despawn, flat walk = %v / %v",
					tick, viewer.name, gotGids, despawn, wantGids, wantDespawn)
			}
			if tick == 0 && !slices.Equal(gotGids, viewer.want) {
				t.Fatalf("viewer %s spawned %v, want %v", viewer.name, gotGids, viewer.want)
			}
			next := published[vi][:0]
			gone := make(map[uint32]bool, len(despawn))
			for _, gid := range despawn {
				gone[gid] = true
			}
			for _, gid := range published[vi] {
				if !gone[gid] {
					next = append(next, gid)
				}
			}
			for _, item := range spawn {
				next = append(next, item.Gid)
			}
			published[vi] = next
		}
	}
}

/*
================
placedGidsOf

Every gid in a bucketed placement, ascending.
================
*/
func placedGidsOf(buckets map[worldgeom.InterestBlock][]placedDrop) []uint32 {
	var gids []uint32
	for _, drops := range buckets {
		for _, drop := range drops {
			gids = append(gids, drop.item.Gid)
		}
	}
	slices.Sort(gids)
	return gids
}

/*
================
TestGroundDropBucketsFollowRegistryWrites

The pin for the cache: a write after a read must appear in the next
read. A cache that stops watching the registry revision - the mutation
this guards - keeps serving the first read and fails here.
================
*/
func TestGroundDropBucketsFollowRegistryWrites(t *testing.T) {
	rt := &Runtime{Ground: grounditem.NewRegistry()}
	if gids := placedGidsOf(rt.groundDropBuckets("pin", grounditem.Population{})); len(gids) != 0 {
		t.Fatalf("empty registry placed %v", gids)
	}
	first := rt.Ground.Add("pin", grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02",
		Position: grounditem.Point{RegionID: 0x6a9a, X: 10, Z: 10}})
	if gids := placedGidsOf(rt.groundDropBuckets("pin", grounditem.Population{})); !slices.Equal(gids, []uint32{first.Gid}) {
		t.Fatalf("after the first add the cache placed %v, want gid %d", gids, first.Gid)
	}
	second := rt.Ground.Add("pin", grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02",
		Position: grounditem.Point{RegionID: 0x6a9a, X: 20, Z: 20}})
	if gids := placedGidsOf(rt.groundDropBuckets("pin", grounditem.Population{})); len(gids) != 2 {
		t.Fatalf("after the second add the cache placed %v, want 2 drops", gids)
	}
	if _, ok := rt.Ground.Remove("pin", first.Gid); !ok {
		t.Fatalf("registry lost gid %d", first.Gid)
	}
	if gids := placedGidsOf(rt.groundDropBuckets("pin", grounditem.Population{})); !slices.Equal(gids, []uint32{second.Gid}) {
		t.Fatalf("after the remove the cache placed %v, want only gid %d", gids, second.Gid)
	}
	instanced := grounditem.Population{World: 0x00030002, Generation: 1}
	rt.Ground.Add("pin", grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Population: instanced,
		Position: grounditem.Point{RegionID: 0x6a9a, X: 30, Z: 30}})
	if gids := placedGidsOf(rt.groundDropBuckets("pin", grounditem.Population{})); len(gids) != 1 {
		t.Fatalf("an instanced drop leaked into the main world: %v", gids)
	}
	if gids := placedGidsOf(rt.groundDropBuckets("pin", instanced)); len(gids) != 1 {
		t.Fatalf("the instanced population placed %v, want one drop", gids)
	}
}

/*
================
TestGroundCacheIgnoresOtherDivisionsWrites

The stamp is the division's own counter: one division's drops churn
every tick of a live shard, and under the one global counter every
other division's cached copy rebuilt with it (#2030). A busy
division's write must leave a quiet division's entry in place -
the same entry, not a rebuilt twin - while the quiet division's own
write still replaces it.
================
*/
func TestGroundCacheIgnoresOtherDivisionsWrites(t *testing.T) {
	rt := &Runtime{Ground: grounditem.NewRegistry()}
	rt.Ground.Add("quiet", grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02",
		Position: grounditem.Point{RegionID: 0x6a9a, X: 10, Z: 10}})
	before := rt.groundDivisionSnapshot("quiet")

	rt.Ground.Add("busy", grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02",
		Position: grounditem.Point{RegionID: 0x6a9a, X: 20, Z: 20}})
	rt.Ground.Add("busy", grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02",
		Position: grounditem.Point{RegionID: 0x6a9a, X: 30, Z: 30}})
	if after := rt.groundDivisionSnapshot("quiet"); after != before {
		t.Fatal("another division's writes rebuilt the quiet division's cached drops")
	}

	rt.Ground.Add("quiet", grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02",
		Position: grounditem.Point{RegionID: 0x6a9a, X: 40, Z: 40}})
	after := rt.groundDivisionSnapshot("quiet")
	if after == before {
		t.Fatal("the quiet division's own write did not rebuild its cached drops")
	}
	if gids := placedGidsOf(after.buckets[grounditem.Population{}]); len(gids) != 2 {
		t.Fatalf("the next view places %v, want 2 drops", gids)
	}
}

/*
================
TestGroundCacheDropsTheEmptyDivision

A division whose last drop leaves keeps no cache entry, so a closed
instance division does not accumulate in the cache.
================
*/
func TestGroundCacheDropsTheEmptyDivision(t *testing.T) {
	rt := &Runtime{Ground: grounditem.NewRegistry()}
	item := rt.Ground.Add("gone", grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02",
		Position: grounditem.Point{RegionID: 0x6a9a, X: 10, Z: 10}})
	if gids := placedGidsOf(rt.groundDropBuckets("gone", grounditem.Population{})); len(gids) != 1 {
		t.Fatalf("the division placed %v, want one drop", gids)
	}
	if _, ok := rt.Ground.Remove("gone", item.Gid); !ok {
		t.Fatalf("registry lost gid %d", item.Gid)
	}
	if gids := placedGidsOf(rt.groundDropBuckets("gone", grounditem.Population{})); len(gids) != 0 {
		t.Fatalf("after the last remove the cache placed %v", gids)
	}
	if _, kept := rt.groundDrops.divisions["gone"]; kept {
		t.Fatal("the empty division kept its cache entry")
	}
}

/*
================
TestHasExpiredOwnerReadsTheCachedDrops

The ownership sweep reads the same cached copies: an expired owner
added after an empty read must still be seen.
================
*/
func TestHasExpiredOwnerReadsTheCachedDrops(t *testing.T) {
	now := time.Unix(4_000_000, 0)
	rt := &Runtime{Ground: grounditem.NewRegistry()}
	if rt.hasExpiredOwner(now) {
		t.Fatal("an empty registry has an expired owner")
	}
	// The tick pass snapshots the division every tick; force that empty
	// snapshot so the sweep must notice the add that follows.
	rt.groundDropBuckets("pin", grounditem.Population{})
	rt.Ground.Add("pin", grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", OwnerJID: 7,
		DroppedAt: now.Add(-grounditem.OwnerLifetime), Position: grounditem.Point{RegionID: 0x6a9a, X: 10, Z: 10}})
	if !rt.hasExpiredOwner(now) {
		t.Fatal("the cached sweep missed the expired owner")
	}
}

/*
================
viewRows

A view flattened for comparison: per population and block, the rows'
items by value, in the view's own order.
================
*/
func viewRows(buckets map[grounditem.Population]map[worldgeom.InterestBlock][]placedDrop) map[dropPlace][]grounditem.Item {
	out := make(map[dropPlace][]grounditem.Item)
	for population, blocks := range buckets {
		for block, rows := range blocks {
			place := dropPlace{population: population, block: block}
			if len(rows) == 0 {
				out[place] = nil
			}
			for _, row := range rows {
				if row.block != block {
					out[dropPlace{population: population, block: row.block}] = nil
				}
				out[place] = append(out[place], *row.item)
			}
		}
	}
	return out
}

/*
================
TestGroundCacheDeltasMatchAWholeRebuild

The cache derives each view from the one before and the registry's
delta (#3164). After every one of 1,500 random registry writes, in two
populations, the derived view must equal a view built whole from the
registry: the same populations, the same blocks, the same rows in the
same order. A view handed out earlier must still read as it did when
it was handed out, because viewers hold it across later writes.
================
*/
func TestGroundCacheDeltasMatchAWholeRebuild(t *testing.T) {
	random := rand.New(rand.NewSource(3164))
	rt := &Runtime{Ground: grounditem.NewRegistry()}
	instance := grounditem.Population{World: 9, Generation: 2}
	clock := time.Unix(1_800_000_000, 0)
	var live []uint32
	var held *groundDivisionDrops
	var heldRows map[dropPlace][]grounditem.Item
	wholeBuilds := 0
	for step := 0; step < 1500; step++ {
		clock = clock.Add(time.Second)
		switch choice := random.Intn(100); {
		case choice < 45 || len(live) == 0:
			item := grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", StackCount: 1, DroppedAt: clock,
				MagicOptions: []uint64{uint64(step)},
				Position:     grounditem.Point{RegionID: 0x6a9a, X: float32(random.Intn(1900)), Z: float32(random.Intn(1900))}}
			if random.Intn(3) == 0 {
				item.Population = instance
			}
			if random.Intn(3) == 0 {
				item.OwnerJID = uint32(1 + random.Intn(9))
			}
			live = append(live, rt.Ground.Add("main", item).Gid)
		case choice < 78:
			at := random.Intn(len(live))
			rt.Ground.Remove("main", live[at])
			live = slices.Delete(live, at, at+1)
		case choice < 90:
			rt.Ground.SetStackCount("main", live[random.Intn(len(live))], uint16(1+random.Intn(900)))
		case choice < 95:
			rt.Ground.ReleaseExpiredOwners("main", clock, 30*time.Second)
		case choice < 98:
			for _, gone := range rt.Ground.ExpireItems("main", clock, 90*time.Second) {
				live = slices.DeleteFunc(live, func(gid uint32) bool { return gid == gone.Gid })
			}
		default:
			for _, gone := range rt.Ground.RemovePopulation("main", instance) {
				live = slices.DeleteFunc(live, func(gid uint32) bool { return gid == gone.Gid })
			}
		}
		// Some steps are not read at all, so a delta spans several writes.
		if random.Intn(4) == 0 {
			continue
		}
		view := rt.groundDivisionSnapshot("main")
		items, revision := rt.Ground.DivisionSnapshot("main")
		whole := newGroundDivisionState(items, revision).view
		if view.revision != whole.revision {
			t.Fatalf("step %d: the view is at revision %d, the registry at %d", step, view.revision, whole.revision)
		}
		if got, want := viewRows(view.buckets), viewRows(whole.buckets); !reflect.DeepEqual(got, want) {
			t.Fatalf("step %d: the derived view differs from a whole rebuild (%d places against %d)", step, len(got), len(want))
		}
		if _, cached := rt.groundDrops.divisions["main"]; !cached {
			wholeBuilds++
		}
		if held != nil && !reflect.DeepEqual(viewRows(held.buckets), heldRows) {
			t.Fatalf("step %d: a view handed out earlier changed under its readers", step)
		}
		if step%50 == 0 {
			held, heldRows = view, viewRows(view.buckets)
		}
	}
	if len(live) == 0 {
		t.Fatal("the run ended on an empty division; it proves less than it should")
	}
	t.Logf("%d drops live at the end; %d reads found the division empty", len(live), wholeBuilds)
}

/*
================
TestGroundCacheFallsBackWhenTheRegistryCannotSay

A reader further behind than the registry's log, or one that read
before a Restore, takes the whole copy again and is right.
================
*/
func TestGroundCacheFallsBackWhenTheRegistryCannotSay(t *testing.T) {
	rt := &Runtime{Ground: grounditem.NewRegistry()}
	first := rt.Ground.Add("main", grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02",
		Position: grounditem.Point{RegionID: 0x6a9a, X: 10, Z: 10}})
	rt.groundDivisionSnapshot("main")
	// More writes than the log keeps, none of them read.
	for i := 0; i < 700; i++ {
		rt.Ground.Add("main", grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02",
			Position: grounditem.Point{RegionID: 0x6a9a, X: float32(20 + i), Z: 10}})
	}
	rt.Ground.Remove("main", first.Gid)
	if gids := placedGidsOf(rt.groundDropBuckets("main", grounditem.Population{})); len(gids) != 700 || slices.Contains(gids, first.Gid) {
		t.Fatalf("after a gap past the log the cache places %d drops, want the 700 that are live", len(gids))
	}
	rt.Ground.Restore(domain.GroundSnapshot{})
	if gids := placedGidsOf(rt.groundDropBuckets("main", grounditem.Population{})); len(gids) != 0 {
		t.Fatalf("after a Restore to an empty plane the cache still places %d drops", len(gids))
	}
}
