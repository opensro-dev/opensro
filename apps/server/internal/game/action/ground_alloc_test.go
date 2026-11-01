/*
===========================================================================

ground_alloc_test.go - ground drops on a churning shard copy no items (#2031)

Every drop, pickup and expiry moves the registry revision, so on a busy
shard the ground pass rebuckets its division nearly every tick. The
buckets copied every item twice more (a population filter and the
placement) and grew per bucket; every new drop rebuilt the published
static reference list to look one codename up. Live that was about
0.9 GB a minute. These pins hold the rebuild to pointers and the lookup
to a cached set.

===========================================================================
*/
package action

import (
	"testing"
	"unsafe"

	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/loot"
)

// churnDrops is a busy division's ground: a few hundred drops over
// a handful of message blocks.
const churnDrops = 400

/*
================
churnGround
================
*/
func churnGround() []grounditem.Item {
	items := make([]grounditem.Item, churnDrops)
	for i := range items {
		items[i] = grounditem.Item{Gid: uint32(i + 1), RefObjID: 62, Codename: "ITEM_ETC_GOLD_02",
			Position: grounditem.Point{RegionID: 0x6a9a, X: float32(i%8) * 300, Z: float32(i/8%4) * 300}}
	}
	return items
}

/*
================
TestRebucketingCopiesNoItems

A rebuild lays pointers into the division's cached copy: the rows it
allocates are far smaller than one copy of the items. A rebuild that
filters or places by value - the mutation this guards - allocates at
least one item copy per drop and fails. The rows still read back as
the items, in order, with only the kept population.
================
*/
func TestRebucketingCopiesNoItems(t *testing.T) {
	items := churnGround()
	items[0].Population = grounditem.Population{World: 7, Generation: 1}
	keepMain := func(item *grounditem.Item) bool { return item.Population == grounditem.Population{} }
	buckets := bucketDrops(items, keepMain)
	placed := 0
	for block, rows := range buckets {
		for i, row := range rows {
			if row.item.Population != (grounditem.Population{}) || row.block != block || row.block != dropBlock(row.item) {
				t.Fatalf("bucket %v row %d is %+v", block, i, row)
			}
			if i > 0 && rows[i-1].item.Gid >= row.item.Gid {
				t.Fatalf("bucket %v lost the items' order at row %d", block, i)
			}
			placed++
		}
	}
	if placed != churnDrops-1 {
		t.Fatalf("placed %d drops, want %d", placed, churnDrops-1)
	}

	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			bucketDrops(items, keepMain)
		}
	})
	itemCopy := int64(churnDrops) * int64(unsafe.Sizeof(grounditem.Item{}))
	t.Logf("rebucket: %d B/op, %d allocs/op; one item copy is %d B", result.AllocedBytesPerOp(), result.AllocsPerOp(), itemCopy)
	if result.AllocedBytesPerOp() >= itemCopy/2 {
		t.Fatalf("rebucketing allocates %d B, at least half an item copy (%d B)", result.AllocedBytesPerOp(), itemCopy)
	}
}

/*
================
TestStaticReferencesAreLookedUpNotRebuilt

A drop of a published static type needs no reference delta, and asking
costs a set lookup, not a rebuild of the published list. Replacing a
catalog rebuilds the set.
================
*/
func TestStaticReferencesAreLookedUpNotRebuilt(t *testing.T) {
	rt := &Runtime{}
	static := loot.MonsterDropRefItemCodenames()
	if len(static) == 0 {
		t.Fatal("no static drop codenames")
	}
	drop := []grounditem.Item{{Gid: 1, RefObjID: 9001, Codename: static[0]}}
	if frames := rt.groundReferences(drop); len(frames) != 0 {
		t.Fatalf("a static drop sent references: %v", frames)
	}
	allocs := testing.AllocsPerRun(50, func() { rt.groundReferences(drop) })
	list := testing.AllocsPerRun(50, func() { rt.StaticRefItemCodenames() })
	t.Logf("allocs: static drop lookup %.0f, one list rebuild %.0f", allocs, list)
	if allocs >= list {
		t.Fatalf("a static lookup allocates %.0f, as much as a rebuild (%.0f)", allocs, list)
	}
	rt.Alchemy = &alchemy.Catalog{}
	if rt.staticRefSet(); rt.staticRefs.alchemy != rt.Alchemy {
		t.Fatal("a replaced alchemy catalog kept the old set")
	}
}

/*
================
BenchmarkGroundChurn

The live shape: one drop lands and one is picked up, then the ground
pass rebuckets the division for its viewers.
================
*/
func BenchmarkGroundChurn(b *testing.B) {
	rt := &Runtime{Ground: grounditem.NewRegistry()}
	for _, item := range churnGround() {
		item.Gid = 0
		rt.Ground.Add("churn", item)
	}
	b.ReportAllocs()
	for range b.N {
		drop := rt.Ground.Add("churn", grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02",
			Position: grounditem.Point{RegionID: 0x6a9a, X: 40, Z: 40}})
		rt.Ground.Remove("churn", drop.Gid)
		rt.groundDropBuckets("churn", grounditem.Population{})
	}
}
