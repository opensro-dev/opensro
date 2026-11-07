/*
===========================================================================

zone_ledger_test.go - the always-on zone totals (#2824)

The budget read (sro-tickbudget) rests on four properties pinned
here: every zone of a tick is counted on both tick paths, a child
zone never outlasts the zone that contains it, every action step is
totalled whether slow or not, and an armed ledger costs no
allocation per zone once its keys exist.

===========================================================================
*/

package simulation

import (
	"context"
	"testing"
	"time"
)

/*
================
zoneTotalsByKey
================
*/
func zoneTotalsByKey(ledger *ZoneLedger) map[string]ZoneTotal {
	out := make(map[string]ZoneTotal)
	for _, row := range ledger.Snapshot() {
		out[row.Kind+":"+row.Name] = row
	}
	return out
}

/*
================
zoneLedgerTestTicker

One session in division "A" and one named hook, with a ledger.
================
*/
func zoneLedgerTestTicker() *Ticker {
	source := &fakeSource{sessions: []SessionSnapshot{
		{SessionID: "a", DivisionID: "A", CharacterID: 1, NpcsEnabled: true, NpcAnchor: NpcShopSpawn()},
	}}
	ticker := newTestTicker(source, &fakePusher{})
	ticker.Hooks = []NamedTickHook{{Name: "probe", Hook: func(int64) []DivisionFrames { return nil }}}
	ticker.Zones = NewZoneLedger()
	return ticker
}

/*
================
TestZoneLedgerCountsEveryZoneOfATick
================
*/
func TestZoneLedgerCountsEveryZoneOfATick(t *testing.T) {
	ticker := zoneLedgerTestTicker()
	ticker.RunTick(100)
	ticker.RunTick(200)
	zones := zoneTotalsByKey(ticker.Zones)
	for _, key := range []string{
		"phase:total", "phase:prepare", "phase:before_hooks", "phase:divisions", "phase:hooks",
		"division:A", "sub:peerVisibility", "sub:peerCOSVisibility", "sub:playerMap",
		"sub:sessionLegs", "sub:monsterLeg", "sub:repush", "hook:probe",
	} {
		if zones[key].Count != 2 {
			t.Errorf("%s counted %d times over two ticks, want 2 (zones %v)", key, zones[key].Count, zones)
		}
	}
}

/*
================
TestZoneLedgerChildrenNeverOutlastTheirParent

The analyzer's "unattributed" row is parent minus children; it is
only honest when no child total can exceed its container's.
================
*/
func TestZoneLedgerChildrenNeverOutlastTheirParent(t *testing.T) {
	ticker := zoneLedgerTestTicker()
	for nowMs := int64(100); nowMs <= 1000; nowMs += 100 {
		ticker.RunTick(nowMs)
	}
	zones := zoneTotalsByKey(ticker.Zones)
	sum := func(keys ...string) time.Duration {
		var total time.Duration
		for _, key := range keys {
			total += zones[key].Total
		}
		return total
	}
	checks := []struct {
		parent   string
		children []string
	}{
		{"phase:total", []string{"phase:prepare", "phase:before_hooks", "phase:divisions", "phase:hooks"}},
		{"phase:divisions", []string{"division:A"}},
		{"division:A", []string{"sub:peerVisibility", "sub:peerCOSVisibility", "sub:playerMap", "sub:sessionLegs", "sub:monsterLeg", "sub:repush"}},
		{"phase:hooks", []string{"hook:probe"}},
	}
	for _, check := range checks {
		if children := sum(check.children...); children > zones[check.parent].Total {
			t.Errorf("%s children %s outlast the parent's %s", check.parent, children, zones[check.parent].Total)
		}
	}
}

/*
================
TestZoneLedgerCountsTheScheduledTick

The production path (Run, shard workers) keeps its own copy of the
tick body; its zones count too, the division's on the shard goroutine.
================
*/
func TestZoneLedgerCountsTheScheduledTick(t *testing.T) {
	ticker := zoneLedgerTestTicker()
	ticker.Interval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go ticker.Run(ctx)
	deadline := time.After(5 * time.Second)
	for zoneTotalsByKey(ticker.Zones)["hook:probe"].Count < 2 {
		select {
		case <-deadline:
			cancel()
			t.Fatalf("the scheduled tick counted no hook zones: %v", zoneTotalsByKey(ticker.Zones))
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-ticker.Done()
	zones := zoneTotalsByKey(ticker.Zones)
	for _, key := range []string{"phase:total", "phase:prepare", "phase:divisions", "division:A", "sub:monsterLeg"} {
		if zones[key].Count == 0 {
			t.Errorf("the scheduled tick never counted %s (zones %v)", key, zones)
		}
	}
}

/*
================
TestStepTimerTotalsEveryStep

Total sees every step, fast or slow; Slow still sees only what
crossed SlowHookThreshold.
================
*/
func TestStepTimerTotalsEveryStep(t *testing.T) {
	clock := time.Unix(0, 0)
	ledger := NewZoneLedger()
	var slow []string
	steps := StepTimer{
		Now:   func() time.Time { return clock },
		Slow:  func(name string, elapsed time.Duration) { slow = append(slow, name) },
		Total: ledger.AddStep,
	}
	steps.Time("fast", func() { clock = clock.Add(2 * time.Millisecond) })
	steps.Time("slow", func() { clock = clock.Add(SlowHookThreshold) })
	steps.Time("fast", func() { clock = clock.Add(3 * time.Millisecond) })
	zones := zoneTotalsByKey(ledger)
	if zones["step:fast"].Count != 2 || zones["step:fast"].Total != 5*time.Millisecond {
		t.Fatalf("fast step totals = %+v, want 2 runs, 5ms", zones["step:fast"])
	}
	if zones["step:slow"].Count != 1 || zones["step:slow"].Total != SlowHookThreshold {
		t.Fatalf("slow step totals = %+v, want 1 run at the threshold", zones["step:slow"])
	}
	if len(slow) != 1 || slow[0] != "slow" {
		t.Fatalf("slow reports = %v, want only the slow step", slow)
	}
}

/*
================
TestZoneLedgerZonesAllocateNothing

Production arms the ledger on every tick, so a zone must cost no
allocation once its key exists; with neither sink a zone is free.
================
*/
func TestZoneLedgerZonesAllocateNothing(t *testing.T) {
	armed := &Ticker{Zones: NewZoneLedger()}
	armed.zone(ZoneKindHook, "action").End()
	if allocs := testing.AllocsPerRun(1000, func() { armed.zone(ZoneKindHook, "action").End() }); allocs != 0 {
		t.Fatalf("an armed zone allocates %.1f times, want none", allocs)
	}
	bare := &Ticker{}
	if allocs := testing.AllocsPerRun(1000, func() { bare.zone(ZoneKindHook, "action").End() }); allocs != 0 {
		t.Fatalf("a disarmed zone allocates %.1f times, want none", allocs)
	}
	if rows := bare.Zones.Snapshot(); rows != nil {
		t.Fatalf("a nil ledger snapshot = %v, want nil", rows)
	}
}

/*
================
BenchmarkZoneLedgerZone

What the always-on ledger costs per zone on the tick.
================
*/
func BenchmarkZoneLedgerZone(b *testing.B) {
	ticker := &Ticker{Zones: NewZoneLedger()}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ticker.zone(ZoneKindHook, "action").End()
	}
}
