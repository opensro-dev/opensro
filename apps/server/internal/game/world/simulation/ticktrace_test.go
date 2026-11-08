/*
===========================================================================

ticktrace_test.go - the zone timeline's contract (#2301)

A slow tick's zones must nest per goroutine and render as
Perfetto JSON a viewer accepts, a fast tick must keep nothing,
and the disarmed path (no tracer, or a StepTimer with no zone
seam) must stay a no-op. Overhead is benchmarked, not asserted:
BenchmarkTickTraceZonesOff and BenchmarkTickTraceZonesArmed are
the numbers the PR quotes against the 0.1 ms budget.

===========================================================================
*/

package simulation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

/*
================
TestTickTraceSlowTickRendersNestedZones
================
*/
func TestTickTraceSlowTickRendersNestedZones(t *testing.T) {
	tracer := NewTickTracer()
	start := time.Now()
	tracer.beginTick(start)
	endPhase := tracer.zone("phase:hooks")
	endHook := tracer.zone("hook:action")
	endHook()
	endPhase()

	_, events := tracer.finish(tickTraceSlowThreshold)
	if events == nil {
		t.Fatal("a slow tick kept no trace")
	}
	trace := renderTickTrace(start, events, tickTraceSlowThreshold)
	if trace.Events != 4 {
		t.Fatalf("trace events = %d, want 4 (two zones, two edges each)", trace.Events)
	}
	var rendered struct {
		TraceEvents []perfettoEvent `json:"traceEvents"`
	}
	if err := json.Unmarshal(trace.JSON, &rendered); err != nil {
		t.Fatalf("rendered trace is not the viewer's JSON: %v", err)
	}
	var phases []string
	var names []string
	for _, event := range rendered.TraceEvents {
		if event.Ph == "M" {
			continue
		}
		phases = append(phases, event.Ph)
		names = append(names, event.Name)
		if event.Tid == 0 {
			t.Fatal("a zone edge has no goroutine track")
		}
		if event.Tid != rendered.TraceEvents[1].Tid {
			t.Fatal("zones from one goroutine split across tracks")
		}
	}
	if strings.Join(phases, ",") != "B,B,E,E" || strings.Join(names, ",") != "phase:hooks,hook:action,hook:action,phase:hooks" {
		t.Fatalf("zone edges = %v %v, want the hook nested in the phase", phases, names)
	}
}

/*
================
TestTickTraceFastTickKeepsNothing
================
*/
func TestTickTraceFastTickKeepsNothing(t *testing.T) {
	tracer := NewTickTracer()
	tracer.beginTick(time.Now())
	end := tracer.zone("phase:hooks")
	end()
	if _, events := tracer.finish(tickTraceSlowThreshold - time.Millisecond); events != nil {
		t.Fatalf("a fast tick kept %d events, want none", len(events))
	}
}

/*
================
TestTickTraceZonesCarryTheirGoroutine
================
*/
func TestTickTraceZonesCarryTheirGoroutine(t *testing.T) {
	tracer := NewTickTracer()
	tracer.beginTick(time.Now())
	done := make(chan struct{})
	go func() {
		defer close(done)
		end := tracer.zone("division:worker")
		end()
	}()
	<-done
	_, events := tracer.finish(tickTraceSlowThreshold)
	if len(events) != 2 {
		t.Fatalf("worker zones = %d events, want a begin/end pair", len(events))
	}
	main := currentGoroutineID()
	if events[0].gid == 0 || events[0].gid == main {
		t.Fatalf("worker zone carries goroutine %d, want its own (main is %d)", events[0].gid, main)
	}
}

/*
================
TestTickerTraceFinishSinksOnlySlowTicks
================
*/
func TestTickerTraceFinishSinksOnlySlowTicks(t *testing.T) {
	var sunk []TickTrace
	ticker := &Ticker{
		Trace: NewTickTracer(),
		TraceSink: func(trace TickTrace) {
			sunk = append(sunk, trace)
		},
	}
	ticker.Trace.beginTick(time.Now())
	ticker.zone(ZoneKindPhase, "hooks").End()
	ticker.traceFinish(TickTiming{Total: tickTraceSlowThreshold - time.Millisecond})
	if len(sunk) != 0 {
		t.Fatalf("a fast tick sank %d traces, want none", len(sunk))
	}
	ticker.Trace.beginTick(time.Now())
	ticker.zone(ZoneKindPhase, "hooks").End()
	ticker.traceFinish(TickTiming{Total: tickTraceSlowThreshold})
	if len(sunk) != 1 || sunk[0].Events != 2 {
		t.Fatalf("a slow tick sank %+v, want one trace of the zoned step", sunk)
	}
	// Disarmed: no tracer, no sink call, no panic.
	disarmed := &Ticker{TraceSink: func(trace TickTrace) { t.Fatal("disarmed ticker sank a trace") }}
	disarmed.zone(ZoneKindPhase, "hooks").End()
	disarmed.traceFinish(TickTiming{Total: tickTraceSlowThreshold})
}

/*
================
TestStepTimerZoneSeam
================
*/
func TestStepTimerZoneSeam(t *testing.T) {
	tracer := NewTickTracer()
	tracer.beginTick(time.Now())
	steps := StepTimer{Zone: tracer.zone}
	steps.Time("attackStep", func() {})
	_, events := tracer.finish(tickTraceSlowThreshold)
	if len(events) != 2 || events[0].name != "attackStep" || events[1].name != "attackStep" {
		t.Fatalf("step zones = %+v, want the step's begin/end pair", events)
	}
	// No seam installed: the step still runs, nothing records.
	plain := StepTimer{}
	ran := false
	plain.Time("attackStep", func() { ran = true })
	if !ran {
		t.Fatal("a seam-less StepTimer did not run its step")
	}
}

/*
================
BenchmarkTickTraceZonesOff

The disarmed budget's measuring stick: zone calls on a ticker
with no tracer must stay at the shared no-op.
================
*/
func BenchmarkTickTraceZonesOff(b *testing.B) {
	ticker := &Ticker{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ticker.zone(ZoneKindPhase, "hooks").End()
	}
}

/*
================
BenchmarkTickTraceZonesArmed

What turning the capture on costs per zone, before anyone enables
it live.
================
*/
func BenchmarkTickTraceZonesArmed(b *testing.B) {
	tracer := NewTickTracer()
	tracer.beginTick(time.Now())
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		end := tracer.zone("phase:hooks")
		end()
		if len(tracer.events) > 8*tickTraceEventPrealloc {
			tracer.events = tracer.events[:0]
		}
	}
}
