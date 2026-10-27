/*
===========================================================================

barrier_probe_test.go - the cheap probes the action tick asks before a barrier

The action runtime takes its global maintenance barrier only when these
probes say there is work (#1820). Each probe must agree with the call it
guards and must not change that call's result.

===========================================================================
*/
package statuseffect

import (
	"reflect"
	"testing"
)

const (
	probeJobDurationMs = 1000000
	probeStepMs        = 1000
	probeHorizonMs     = 905000
	probeExpiredMs     = 2000
)

/*
================
probeJobRegistry

Two persistent jobs installed a second apart, so their 300-second
checkpoints land on different ticks.
================
*/
func probeJobRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	for i, start := range []int64{0, probeStepMs} {
		if !r.Apply(Effect{DivisionID: "d", CharacterName: "c", SkillID: uint32(i + 1), SkillGroup: uint32(i + 1),
			InstanceToken: uint32(i + 1), Persistent: true, StartedAtMs: start, ExpiresAtMs: start + probeJobDurationMs}) {
			t.Fatal("apply")
		}
	}
	return r
}

/*
================
TestAnyJobCheckpointDueAgreesWithTakeAndLeavesItUnchanged

Over three checkpoint periods, the probe is true on exactly the ticks where
TakeJobCheckpoints returns a batch, and probing first (even twice) yields
the same batches as taking alone.
================
*/
func TestAnyJobCheckpointDueAgreesWithTakeAndLeavesItUnchanged(t *testing.T) {
	probed, alone := probeJobRegistry(t), probeJobRegistry(t)
	dueTicks := 0
	for now := int64(0); now <= probeHorizonMs; now += probeStepMs {
		due := probed.AnyJobCheckpointDue(now)
		if again := probed.AnyJobCheckpointDue(now); again != due {
			t.Fatalf("t=%d: a second probe changed the answer", now)
		}
		got, want := probed.TakeJobCheckpoints(now), alone.TakeJobCheckpoints(now)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("t=%d: probing changed the batches: %v vs %v", now, got, want)
		}
		if due != (len(want) > 0) {
			t.Fatalf("t=%d: probe said %v, take returned %d batches", now, due, len(want))
		}
		if due {
			dueTicks++
		}
	}
	if dueTicks == 0 {
		t.Fatal("no checkpoint came due; the probe was never exercised")
	}
}

/*
================
TestHasStopRequestedTracksTheDrainQueue

The drain probe is false on an empty registry, true once an expiry queues
an owner, and false again after the drain.
================
*/
func TestHasStopRequestedTracksTheDrainQueue(t *testing.T) {
	var nilRegistry *Registry
	if nilRegistry.HasStopRequested() {
		t.Fatal("a nil registry reported pending stops")
	}
	r := NewRegistry()
	if r.HasStopRequested() {
		t.Fatal("an empty registry reported pending stops")
	}
	if !r.Apply(Effect{DivisionID: "d", CharacterName: "c", SkillID: 1, SkillGroup: 1, InstanceToken: 1,
		State: StateActive, DurationPresent: true, StartedAtMs: 0, ExpiresAtMs: probeStepMs}) {
		t.Fatal("apply")
	}
	r.Expire(probeExpiredMs)
	if !r.HasStopRequested() {
		t.Fatal("an expired effect queued nothing for the drain")
	}
	if ended := r.DrainStopRequested(); len(ended) != 1 {
		t.Fatalf("drained %d owners, want 1", len(ended))
	}
	if r.HasStopRequested() {
		t.Fatal("the drain left the queue non-empty")
	}
}
