/*
===========================================================================

timing_test.go - request and tick phase histograms land in their buckets

===========================================================================
*/

package transport

import (
	"testing"
	"time"
)

/*
================
TestTimingHistogramsBucketHandlersPhasesAndSlowHooks
================
*/
func TestTimingHistogramsBucketHandlersPhasesAndSlowHooks(t *testing.T) {
	hub := newHub(testCfg())
	hub.metrics.timing.recordHandler(0x7738, 5*time.Millisecond)
	hub.metrics.timing.recordHandler(0x7738, 120*time.Millisecond)
	hub.metrics.timing.recordHandler(0x7738, 2*time.Second)
	hub.RecordTickPhases(TickPhases{
		BeforeHooks: 2 * time.Millisecond, Divisions: 30 * time.Millisecond, Hooks: 300 * time.Millisecond,
		Total:     332 * time.Millisecond,
		SlowHooks: []SlowHook{{Name: "action.(*Runtime).TickHook.func1", Elapsed: 300 * time.Millisecond}},
	})
	m := hub.Metrics()
	move := m.HandlerMs["0x7738"]
	if move.Buckets != [6]uint64{1, 0, 0, 1, 0, 1} || move.MaxMs != 2000 {
		t.Fatalf("handler histogram = %+v", move)
	}
	if m.TickPhaseMs["divisions"].Buckets[1] != 1 || m.TickPhaseMs["hooks"].Buckets[4] != 1 || m.TickPhaseMs["total"].MaxMs != 332 {
		t.Fatalf("tick phases = %+v", m.TickPhaseMs)
	}
	if slow := m.SlowHooks["action.(*Runtime).TickHook.func1"]; slow.Count != 1 || slow.MaxMs != 300 {
		t.Fatalf("slow hooks = %+v", m.SlowHooks)
	}
}
