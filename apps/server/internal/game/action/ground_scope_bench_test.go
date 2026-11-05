/*
===========================================================================

ground_scope_bench_test.go - the per-tick ground pass's cost (#1943)

One division holding 1,600 main-world drops and 64 viewers spread over
the drop field, advanced with no live scenes: the measured work is the
registry copy, the placement and the per-viewer scope walk, the chain
the production profile attributes to this pass.

===========================================================================
*/
package action

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

const (
	benchGroundDrops   = 1600
	benchGroundViewers = 64
	benchGroundRegion  = 0x6a9a
)

/*
================
BenchmarkGroundScopeTick

One AdvanceGroundScope pass over the bench field. The scene lookup
answers "not live", so nothing is published; the copy and scope work
runs in full.
================
*/
func BenchmarkGroundScopeTick(b *testing.B) {
	registry := grounditem.NewRegistry()
	rt := &Runtime{Ground: registry}
	var gids []uint32
	for i := 0; i < benchGroundDrops; i++ {
		item := registry.Add("bench", grounditem.Item{
			RefObjID:     62,
			Codename:     "ITEM_ETC_GOLD_02",
			MagicOptions: []uint64{0x10001, 0x20002},
			Position: grounditem.Point{RegionID: benchGroundRegion,
				X: float32(100 + (i%40)*160), Z: float32(100 + (i/40)*160)},
		})
		gids = append(gids, item.Gid)
	}
	sessions := make([]simulation.SessionView, benchGroundViewers)
	for v := range sessions {
		published := make([]uint32, 0, benchGroundDrops/2)
		for i := 0; i < benchGroundDrops; i += 2 {
			published = append(published, gids[i])
		}
		sessions[v] = simulation.SessionView{
			SessionID:        fmt.Sprintf("viewer-%d", v),
			DivisionID:       "bench",
			WorldInstance:    domain.DefaultWorldInstance,
			PublishedObjects: published,
			World: simulation.WorldState{Spawn: simulation.Spawn{RegionID: benchGroundRegion,
				X: float64(200 + (v%8)*700), Z: float64(200 + (v/8)*700)}},
		}
	}
	scene := func(string) (*transport.Session, uint64, bool) { return nil, 0, false }
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rt.AdvanceGroundScope(1000, sessions, scene)
	}
}
