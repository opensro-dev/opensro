/*
===========================================================================

placement_test.go - a plan refusal names its own cause

The rehearsal's "cannot place" error must carry the scheduler's reason:
the exhausted dimension and the resources asked for, in a stable order.

===========================================================================
*/

package main

import (
	"testing"

	nomad "github.com/hashicorp/nomad/api"
)

/*
================
TestPlacementFailuresNamesTheExhaustedDimension

A canary that does not fit beside the live allocation: one node
evaluated and exhausted on cpu, the task asking 6000 MHz.
================
*/
func TestPlacementFailuresNamesTheExhaustedDimension(t *testing.T) {
	cpu, memory := 6000, 768
	failed := map[string]*nomad.AllocationMetric{
		"gameworld": {
			NodesEvaluated:     1,
			NodesExhausted:     1,
			DimensionExhausted: map[string]int{"cpu": 1},
			ResourcesExhausted: map[string]*nomad.Resources{
				"gameworld": {CPU: &cpu, MemoryMB: &memory},
			},
		},
	}
	got := placementFailures(failed)
	want := "gameworld (1 node(s) evaluated, 1 exhausted, dimension exhausted: cpu=1, " +
		"short of gameworld cpu=6000 MHz memory=768 MB)"
	if got != want {
		t.Fatalf("placementFailures:\n got %q\nwant %q", got, want)
	}
}

/*
================
TestPlacementFailuresIsStableAndNamesBareGroups

Groups sort by name, map keys inside a reason sort too, and a group
the plan gave no metric for is still named.
================
*/
func TestPlacementFailuresIsStableAndNamesBareGroups(t *testing.T) {
	failed := map[string]*nomad.AllocationMetric{
		"zeta": nil,
		"alpha": {
			NodesEvaluated:     2,
			NodesFiltered:      2,
			ConstraintFiltered: map[string]int{"${meta.sro_shards} set_contains global": 1, "${attr.kernel.name} = linux": 1},
			QuotaExhausted:     []string{"sro"},
		},
	}
	got := placementFailures(failed)
	want := "alpha (2 node(s) evaluated, 2 filtered, constraint filtered: " +
		"${attr.kernel.name} = linux=1, ${meta.sro_shards} set_contains global=1, quota exhausted: sro); zeta"
	if got != want {
		t.Fatalf("placementFailures:\n got %q\nwant %q", got, want)
	}
}
