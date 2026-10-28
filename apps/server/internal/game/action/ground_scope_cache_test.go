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
		spawn = append(spawn, drop.item)
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
