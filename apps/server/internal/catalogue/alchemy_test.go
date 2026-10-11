/*
===========================================================================

alchemy_test.go - the reinforcement rules the planner reads

===========================================================================
*/
package catalogue

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/item/alchemy"
)

/*
================
TestAlchemyShardRatesFollowTheSetting

The export carries the shard's port-only table only while it is set, so
an original-rules shard publishes no shardRates.
================
*/
func TestAlchemyShardRatesFollowTheSetting(t *testing.T) {
	catalog := &alchemy.Catalog{Items: map[string]alchemy.Reference{}}
	source := AlchemySource{Catalog: catalog, ItemID: func(string) uint32 { return 0 }}
	if file := BuildAlchemy(Header{}, source); file.Rules.ShardRates != nil {
		t.Fatalf("off: shardRates = %v", file.Rules.ShardRates)
	}
	catalog.Rates, _ = alchemy.ParseReinforceRates("on")
	file := BuildAlchemy(Header{}, source)
	if !reflect.DeepEqual(file.Rules.ShardRates, []int(catalog.Rates)) {
		t.Fatalf("on: shardRates = %v, want %v", file.Rules.ShardRates, catalog.Rates)
	}
	if file.Rules.MinChance != alchemy.ReinforceMinChance {
		t.Fatalf("minChance = %d: the native rule must stay published", file.Rules.MinChance)
	}
}
