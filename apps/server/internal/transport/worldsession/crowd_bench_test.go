//go:build !windows

/*
===========================================================================

crowd_bench_test.go - what a walking plaza crowd costs the server

BenchmarkCrowd runs the crowd fixture (crowd_test.go) on the real 100 ms
tick clock: one benchmark op is one tick, with the walkers clicking like
the live bot crowd beside it. It reports per tick and per second what a
read of /transport/metrics would show, plus the process CPU (the server
and, over sockets, the readers and the kernel) per frame:

	go test -run '^$' -bench BenchmarkCrowd -benchtime 300x ./internal/transport/worldsession/

SRO_POSITION_STREAM=on measures the port-only position stream instead of
the native fan-out (simulation/position_stream.go).

Each case attaches its crowd once and warms it for crowdWarmTicks before
the first measured tick. It loads the machine (1,000 loopback sockets
and their readers at the largest case); it is a measurement, not a gate.
Not built on Windows: the CPU read is getrusage.

===========================================================================
*/
package worldsession

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/simulation"
)

// crowdWarmTicks settle the crowd (everyone's first spawn, the first
// clicks) before a case measures.
const crowdWarmTicks = 30

/*
================
processCPUSeconds

User plus system CPU of the whole process.
================
*/
func processCPUSeconds() float64 {
	var usage syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
	return float64(usage.Utime.Sec+usage.Stime.Sec) + float64(usage.Utime.Usec+usage.Stime.Usec)/1e6
}

// crowdReportedZones are the tick zones the benchmark reports per tick.
var crowdReportedZones = []string{"phase:prepare", "sub:peerVisibility", "sub:sessionLegs", "sub:monsterLeg", "phase:hooks"}

/*
================
crowd.zoneTotals

The ticker's zone ledger, keyed "<kind>:<name>".
================
*/
func (c *crowd) zoneTotals() map[string]time.Duration {
	out := map[string]time.Duration{}
	for _, row := range c.ticker.Zones.Snapshot() {
		out[row.Kind+":"+row.Name] = row.Total
	}
	return out
}

/*
================
crowd.runTicks

Runs n ticks on the real 100 ms clock and returns each tick's duration.
================
*/
func (c *crowd) runTicks(n int) []time.Duration {
	clock := time.NewTicker(simulation.DefaultTickInterval)
	defer clock.Stop()
	durations := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		now := <-clock.C
		started := time.Now()
		c.ticker.RunTick(now.UnixMilli())
		durations = append(durations, time.Since(started))
	}
	return durations
}

/*
================
crowd.measure

One benchmark run: b.N ticks, reported as the transport's own counters
over the run's wall time.
================
*/
func (c *crowd) measure(b *testing.B) {
	before := c.hub.Metrics()
	zonesBefore := c.zoneTotals()
	cpu := processCPUSeconds()
	started := time.Now()
	b.ResetTimer()
	durations := c.runTicks(b.N)
	b.StopTimer()
	seconds := time.Since(started).Seconds()
	cpu = processCPUSeconds() - cpu
	after := c.hub.Metrics()
	zonesAfter := c.zoneTotals()

	frames := float64(after.FramesOut - before.FramesOut)
	bytes := float64(after.BytesOut - before.BytesOut)
	writes := float64(after.WritesOut - before.WritesOut)
	slices.Sort(durations)
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
	b.ReportMetric(ms(total)/float64(len(durations)), "tick-mean-ms")
	b.ReportMetric(ms(durations[(len(durations)-1)*99/100]), "tick-p99-ms")
	b.ReportMetric(frames/seconds, "frames/s")
	b.ReportMetric(bytes/seconds/1e6, "MB/s")
	b.ReportMetric(frames/float64(len(durations))/float64(len(c.sessions)), "frames/viewer/tick")
	b.ReportMetric(writes/seconds, "writes/s")
	if writes > 0 {
		b.ReportMetric(frames/writes, "frames/write")
	}
	if frames > 0 {
		b.ReportMetric(cpu*1e6/frames, "cpu-us/frame")
	}
	b.ReportMetric(cpu/seconds, "cpu-cores")
	b.ReportMetric(float64(after.WriteErrors-before.WriteErrors), "write-errors")
	b.ReportMetric(float64(after.Detaches-before.Detaches), "detaches")
	b.ReportMetric(float64(after.SessionsClosedSlowConsumer-before.SessionsClosedSlowConsumer), "closures")
	// The tick's own breakdown, as /transport/metrics tick_zones shows it.
	for _, zone := range crowdReportedZones {
		spent := zonesAfter[zone] - zonesBefore[zone]
		b.ReportMetric(float64(spent)/float64(time.Millisecond)/float64(len(durations)), zone+"-ms")
	}
	var gap int64
	for _, reader := range c.readers {
		gap = max(gap, reader.maxGapNs.Swap(0))
	}
	if len(c.readers) > 0 {
		b.ReportMetric(float64(gap)/float64(time.Second), "reader-max-gap-s")
	}
}

/*
================
BenchmarkCrowd
================
*/
func BenchmarkCrowd(b *testing.B) {
	for _, cfg := range []crowdConfig{
		// The live shape first: the scratch stack's Hotan plaza with NPCs.
		{hotan: true, players: 1000, walkers: 500},
		{hotan: true, players: 1000, walkers: 1000},
		{players: 600, walkers: 300},
		{players: 1000, walkers: 500},
		{players: 1000, walkers: 1000},
		{ws: true, players: 600, walkers: 300},
		{ws: true, players: 1000, walkers: 500},
		{ws: true, players: 1000, walkers: 1000},
	} {
		mode := "mem"
		if cfg.ws {
			mode = "ws"
		}
		// The crowd outlives the runs Go makes of one case (a first
		// run of one op, then the asked count): built once, closed
		// after the case.
		var c *crowd
		var cancel context.CancelFunc
		if cfg.hotan {
			mode += "-hotan"
		}
		b.Run(fmt.Sprintf("mode=%s/players=%d/walkers=%d", mode, cfg.players, cfg.walkers), func(b *testing.B) {
			if c == nil {
				c = newCrowd(b, cfg)
				var ctx context.Context
				ctx, cancel = context.WithCancel(context.Background())
				var clicks atomic.Int64
				go c.walkBots(ctx, &clicks)
				c.runTicks(crowdWarmTicks)
			}
			c.measure(b)
		})
		if c != nil {
			cancel()
			c.close()
		}
	}
}
