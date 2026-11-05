/*
===========================================================================

monsterleg_staircase_test.go - the monster leg staircase bench (#2137 slice 4, #2206)

Prices the leg the worker decision is made from: 200 and 500 due actors,
planned serially (workers=serial, the leg as it runs with
SRO_MONSTER_LEG_WORKERS unset) and on the pool at 1, 2 and 4 workers.
Poses sit either in one region - the hot-region worst case, where the
partition has a single bin and the pool can win nothing - or spread over
sixteen regions, where bins balance. Each sub-bench settles the seeded
population first, then reports the leg's own phase walls (ops.lastLeg:
total, dormancy, visibility, batch, queue, advance, lane) per leg, so
the serial remainder prices itself next to the parallel part.

Run: go test ./internal/game/world/simulation/ -run '^$' \
	-bench MonsterLegStaircase -benchtime=100x

===========================================================================
*/

package simulation

import (
	"fmt"
	"testing"
	"time"
)

// staircaseRegions is the spread case's region count: enough bins that
// 2 and 4 workers both balance, few enough that a region still carries
// a realistic share of the due actors.
const staircaseRegions = 16

/*
================
spreadPoseRegions

Round-robin the seeded movers' pose regions over spread synthetic
sectors (monsterTestRegion upward). The partition keys on the pose
region, so this is what turns one bin into spread bins; coordinates
do not move, and the fixture's steps never recompute the region, so
the spread survives settling.
================
*/
func spreadPoseRegions(ops *MonsterMoverOps, spread int) {
	if spread <= 1 {
		return
	}
	instances := ops.Monsters.InstancesInRegions(monsterTestDivision, []uint16{monsterTestRegion})
	for i, instance := range instances {
		mover, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if !ok {
			continue
		}
		mover.Pose.RegionID = monsterTestRegion + uint16(i%spread)
		ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	}
}

/*
================
legStaircaseSessions

The viewers a staircase cell runs under: the seeded world's one viewer
for the single-region case, one viewer per pose region for the spread
case. Due-ness follows the viewers' regions (an actor outside every
viewer's region leaves the due set), so the spread case needs a viewer
standing in each region to keep the same due actors priced.
================
*/
func legStaircaseSessions(seeded []SessionSnapshot, spread int) []SessionSnapshot {
	if spread <= 1 {
		return seeded
	}
	base := seeded[0]
	sessions := make([]SessionSnapshot, 0, spread)
	for i := 0; i < spread; i++ {
		viewer := base
		viewer.SessionID = fmt.Sprintf("viewer-%d", i)
		viewer.CharacterID = int64(100 + i)
		viewer.World.Spawn.RegionID = monsterTestRegion + uint16(i)
		sessions = append(sessions, viewer)
	}
	return sessions
}

/*
================
runLegStaircase

One staircase cell: build the seeded world, spread its pose regions,
settle it into steady state, then run one 100 ms leg per benchmark
iteration and average the leg record's walls over the measured legs.
================
*/
func runLegStaircase(b *testing.B, actors, spread, workers int) {
	ops, seeded, now := seededLegWorld(b, actors)
	spreadPoseRegions(ops, spread)
	sessions := legStaircaseSessions(seeded, spread)
	ops.LegWorkers = workers
	settle := &fakePusher{}
	for tick := 0; tick < 50; tick++ {
		*now = seededLegBaseMs + int64(tick)*100
		ops.RunMonsterLeg(*now, sessions, settle)
	}
	if len(settle.toSession) == 0 && len(settle.toDivision) == 0 {
		b.Fatal("seeded leg published nothing while settling")
	}
	var total, dormancy, visibility, batch, queue, advance, lane time.Duration
	stepped := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		*now += 100
		ops.RunMonsterLeg(*now, sessions, discardPusher{})
		stats := ops.lastLeg
		total += stats.total
		dormancy += stats.dormancy
		visibility += stats.visibility
		batch += stats.batch
		queue += stats.queue
		advance += stats.advance
		lane += stats.lane
		stepped += stats.actors
	}
	b.StopTimer()
	legs := float64(b.N)
	us := func(d time.Duration) float64 { return float64(d) / legs / float64(time.Microsecond) }
	b.ReportMetric(us(total), "total_us/leg")
	b.ReportMetric(us(dormancy), "dormancy_us/leg")
	b.ReportMetric(us(visibility), "visibility_us/leg")
	b.ReportMetric(us(batch), "batch_us/leg")
	b.ReportMetric(us(queue), "queue_us/leg")
	b.ReportMetric(us(advance), "advance_us/leg")
	b.ReportMetric(us(lane), "lane_us/leg")
	b.ReportMetric(float64(stepped)/legs, "actors/leg")
}

/*
================
BenchmarkMonsterLegStaircase

The staircase itself: actors x region spread x worker count. The
worker counts stop at 4 because the live host has 4 cores; the pool
can only spend cores the host has.
================
*/
func BenchmarkMonsterLegStaircase(b *testing.B) {
	for _, actors := range []int{200, 500} {
		for _, spread := range []int{1, staircaseRegions} {
			for _, workers := range []int{0, 1, 2, 4} {
				name := fmt.Sprintf("actors=%d/regions=%d/workers=%d", actors, spread, workers)
				if workers == 0 {
					name = fmt.Sprintf("actors=%d/regions=%d/workers=serial", actors, spread)
				}
				b.Run(name, func(b *testing.B) {
					runLegStaircase(b, actors, spread, workers)
				})
			}
		}
	}
}
