/*
===========================================================================

returnscroll_bench_test.go - what one return-scroll tick costs in a wave

advanceReturnScrolls hands the due wave to the return lane and runs
no arrival itself (#2143); this bench reproduces the wave that used
to produce 100 ms ticks: many casts due at once. The tick bench
times the hand-off, the lane bench times the work that moved.

===========================================================================
*/
package action

import (
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
returnBurst

Builds one runtime holding count characters, each with a return cast
already due. seed re-arms every cast so a bench iteration starts from
the same wave the first tick of a real burst sees.
================
*/
type returnBurst struct {
	rt    *Runtime
	chars []*enterworld.Character
	now   int64
	done  int
}

func newReturnBurst(b *testing.B, count int) *returnBurst {
	b.Helper()
	chars := make([]*enterworld.Character, 0, count)
	for i := 0; i < count; i++ {
		c := rebirthTestCharacter(20, 100)
		c.ID = int64(100 + i)
		c.Name = fmt.Sprintf("burst%02d", i)
		want := simulation.Spawn{RegionID: 25416, X: 703, Y: 42, Z: 1575, Angle: 12345}
		c.World.RebirthPoint = worldSpawnFromMission(want)
		chars = append(chars, c)
	}
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: chars},
		Items:      testItems(),
		NpcSpawns:  enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	rt.Now = clock.Now
	burst := &returnBurst{rt: rt, chars: chars, now: clock.NowMs()}
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) {
		for _, frame := range frames {
			if frame.Opcode == enterworld.OpcodeResetClient {
				burst.done++
			}
		}
	}
	rt.PushDivisionPeerFrames = func(_, _ string, _ []wire.Frame) {}
	return burst
}

/*
================
seed

Re-arms every character's due cast (the completion clears the mode and
consumes the job, so each iteration re-stores both).
================
*/
func (burst *returnBurst) seed() {
	for _, c := range burst.chars {
		c.NativeTeleportMode = 1
		burst.rt.returnCasts.Store(simulation.WorldKey(testDivision, c.Name), pendingReturn{
			division: testDivision, name: c.Name, character: c, due: burst.now,
		})
	}
}

/*
================
BenchmarkAdvanceReturnScrollsBurst

One advanceReturnScrolls call with a full wave due: the due scan,
the sort and the hand-off to the return lane. The drain between
iterations runs untimed, so every timed call sees the same wave.
================
*/
func BenchmarkAdvanceReturnScrollsBurst(b *testing.B) {
	burst := newReturnBurst(b, 16)
	burst.seed()
	burst.rt.advanceReturnScrolls(burst.now)
	burst.rt.processReturnArrivals()
	if burst.done == 0 {
		b.Fatal("fixture completed no return scroll")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		burst.seed()
		burst.rt.advanceReturnScrolls(burst.now)
		b.StopTimer()
		burst.rt.processReturnArrivals()
		b.StartTimer()
	}
}

/*
================
BenchmarkReturnArrivalLaneBurst

One return-lane drain of a full due wave: the arrival work the tick
used to run inline, now on the lane. The hand-off between
iterations runs untimed.
================
*/
func BenchmarkReturnArrivalLaneBurst(b *testing.B) {
	burst := newReturnBurst(b, 16)
	burst.seed()
	burst.rt.advanceReturnScrolls(burst.now)
	burst.rt.processReturnArrivals()
	if burst.done == 0 {
		b.Fatal("fixture completed no return scroll")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		burst.seed()
		burst.rt.advanceReturnScrolls(burst.now)
		b.StartTimer()
		burst.rt.processReturnArrivals()
	}
}
