/*
===========================================================================

pprof_test.go - the profiling listener reaches one shard, on loopback

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/transport"
)

/*
================
TestPprofServesOnlyTheFirstShardOnItsOwnLoopbackListener

The first shard turns profiling on at its own loopback address; another
shard keeps it off. The job always receives a loopback pprof_addr, so a
shard that serves none can never be handed a public one.
================
*/
func TestPprofServesOnlyTheFirstShardOnItsOwnLoopbackListener(t *testing.T) {
	shards := []shardDeployment{
		{Definition: shard.Definition{ID: "global-official"}, PprofAddr: "127.0.0.1:8792"},
		{Definition: shard.Definition{ID: "test"}},
	}
	d := deployment{Shards: shards}
	first, second := d.gameVariables(shards[0]), d.gameVariables(shards[1])
	if first["transport_pprof"] != "1" || first["pprof_addr"] != "127.0.0.1:8792" {
		t.Fatalf("first shard pprof = %v at %v", first["transport_pprof"], first["pprof_addr"])
	}
	if second["transport_pprof"] != "0" || second["pprof_addr"] != transport.DefaultPprofAddr {
		t.Fatalf("second shard pprof = %v at %v", second["transport_pprof"], second["pprof_addr"])
	}
}
