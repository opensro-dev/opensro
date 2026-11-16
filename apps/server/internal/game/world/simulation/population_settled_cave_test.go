/*
===========================================================================

population_settled_cave_test.go - the boot fill with the fast cave respawn

SRO_JANGAN_CAVE_RESPAWN draws 1-3 second respawn timers in the Ch'in Tomb.
The GameWorld's tick runs a few milliseconds late as a rule, so a visit
once a second found such timers elapsed again and an ordinary tomb hive
placed something at nearly every visit until it was full: admission
waited about 40 s for the whole tomb, 2 s with the flag off. A perfect
100 ms clock never shows it (population_settled_world_test.go settles
in a second with the flag on), so these tests drive the population with
a late-running clock.

===========================================================================
*/

package simulation

import (
	"math/rand"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

const (
	caveFillMembers  = 20
	caveFillPerNest  = 4
	caveFillLimitMs  = 30000
	caveFillAfterMs  = 10000
	caveFillSeed     = 3
	caveFillRegion   = 32774
	fieldFillRegion  = 257
	caveFillHiveName = "hive"
	// caveFillFirstVisitMs bounds a settle at the visit after the boot
	// visit: one native callback plus the clock's largest lateness.
	caveFillFirstVisitMs = monster.NestHiveTickMs + 300
	// worldCaveFillJitterMs is how late each world pass may run: 0-100 ms
	// on top of the 100 ms tick, the range the probe measured 37-49 s in.
	worldCaveFillJitterMs = 100
	// worldCaveFillSeed fixes the population's random source and the
	// clock's lateness.
	worldCaveFillSeed = 8
)

// lateTickMs is a pass interval pattern running behind the 100 ms tick.
var lateTickMs = []int64{100, 130, 100, 170, 110}

/*
================
caveFillState

One ordinary hive of caveFillMembers nests of caveFillPerNest monsters in
regionID, authored at minSec-maxSec seconds.
================
*/
func caveFillState(t *testing.T, regionID uint16, minSec, maxSec int) (*MonsterState, *time.Time) {
	t.Helper()
	nests := make([]monster.NestRow, 0, caveFillMembers)
	for i := range caveFillMembers {
		nests = append(nests, monster.NestRow{
			SpawnPoint:         monster.SpawnPoint{RefObjID: 1, RegionID: regionID, X: float64(10 + i*40), Z: 34},
			MaxCount:           caveFillPerNest,
			PolicyPinned:       true,
			Respawn:            true,
			RespawnDelayMinSec: minSec,
			RespawnDelayMaxSec: maxSec,
			HiveKey:            caveFillHiveName,
		})
	}
	s := NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{1: {TidWord: 0x00C6, RefObjID: 1, Name: "Cave", MaxHP: 100}},
		nests,
	))
	s.SetRandomSource(rand.New(rand.NewSource(caveFillSeed)).Float64)
	now := time.Unix(100, 0)
	s.SetTimeSource(func() time.Time { return now })
	s.StartDivision("a")
	return s, &now
}

/*
================
advanceUntilSettled

Runs passes on the late clock until the division settles; returns the
milliseconds it took, or -1 past limitMs.
================
*/
func advanceUntilSettled(s *MonsterState, now *time.Time, limitMs int64) int64 {
	for elapsed, pass := int64(0), 0; elapsed <= limitMs; pass++ {
		s.AdvancePopulation(s.CurrentTimeMillis())
		if s.PopulationSettled("a") {
			return elapsed
		}
		step := lateTickMs[pass%len(lateTickMs)]
		elapsed += step
		*now = now.Add(time.Duration(step) * time.Millisecond)
	}
	return -1
}

/*
================
TestCaveHiveSettlesAtItsBootBurst

A tomb hive authored at 6-8 seconds settles at the visit after its boot
burst with the flag off and on; on, it still respawns at the fast window
after admission (full within caveFillAfterMs). An ordinary hive outside
the tomb authored at 1-3 seconds keeps the native rule with the flag on:
it settles only at a visit that places nothing.
================
*/
func TestCaveHiveSettlesAtItsBootBurst(t *testing.T) {
	for _, tc := range []struct {
		name   string
		env    string
		region uint16
		minSec int
		maxSec int
		native bool
	}{
		{"tomb off", "off", caveFillRegion, 6, 8, false},
		{"tomb on", "on", caveFillRegion, 6, 8, false},
		{"field 1-3 s on", "on", fieldFillRegion, 1, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvJanganCaveRespawn, tc.env)
			s, now := caveFillState(t, tc.region, tc.minSec, tc.maxSec)
			settledAt := advanceUntilSettled(s, now, caveFillLimitMs)
			resident := s.Observatory("a", nil).Resident
			t.Logf("settled after %d ms with %d resident", settledAt, resident)
			if settledAt < 0 {
				t.Fatalf("not settled within %d ms (%d resident)", caveFillLimitMs, resident)
			}
			if resident < caveFillMembers {
				t.Fatalf("settled with %d resident; the boot burst places one per member (%d)", resident, caveFillMembers)
			}
			if tc.native {
				if settledAt <= caveFillFirstVisitMs {
					t.Fatalf("a field hive still placing settled after %d ms: the cave rule leaked out of the tomb", settledAt)
				}
				return
			}
			if settledAt > caveFillFirstVisitMs {
				t.Fatalf("settled after %d ms; the boot burst ends at the next visit (%d ms)", settledAt, caveFillFirstVisitMs)
			}
			if tc.env != "on" {
				return
			}
			for elapsed, pass := int64(0), 0; elapsed < caveFillAfterMs; pass++ {
				step := lateTickMs[pass%len(lateTickMs)]
				elapsed += step
				*now = now.Add(time.Duration(step) * time.Millisecond)
				s.AdvancePopulation(s.CurrentTimeMillis())
			}
			if full := s.Observatory("a", nil).Resident; full != caveFillMembers*caveFillPerNest {
				t.Fatalf("%d resident %d ms after admission; the fast window fills %d", full, caveFillAfterMs, caveFillMembers*caveFillPerNest)
			}
		})
	}
}

/*
================
TestRealWorldBootFillSettlesWithTheCaveWindow

The shipped world on a clock running 0-worldCaveFillJitterMs late per
pass: with the flag on the fill settles inside worldFillBoundMs and no
later than one callback after the same world with the flag off.
================
*/
func TestRealWorldBootFillSettlesWithTheCaveWindow(t *testing.T) {
	tier.Long(t, "the real world boot fill settles with the cave window on")
	template := monster.LoadTemplate(gamedatatest.TextdataDir(t))
	settle := func(env string) (int64, int) {
		t.Setenv(EnvJanganCaveRespawn, env)
		s := NewMonsterState(template)
		s.SetRandomSource(rand.New(rand.NewSource(worldCaveFillSeed)).Float64)
		late := rand.New(rand.NewSource(worldCaveFillSeed))
		now := time.Unix(100, 0)
		s.SetTimeSource(func() time.Time { return now })
		s.StartDivision("world")
		for elapsed := int64(0); elapsed <= worldFillBoundMs*2; {
			s.AdvancePopulation(s.CurrentTimeMillis())
			if s.PopulationSettled("world") {
				return elapsed, s.Observatory("world", nil).Resident
			}
			step := int64(DefaultTickInterval/time.Millisecond) + late.Int63n(worldCaveFillJitterMs+1)
			elapsed += step
			now = now.Add(time.Duration(step) * time.Millisecond)
		}
		return -1, s.Observatory("world", nil).Resident
	}
	offMs, offResident := settle("off")
	onMs, onResident := settle("on")
	t.Logf("%d nests on a late clock: off settled after %d ms (%d resident), on after %d ms (%d resident)",
		len(template.Nests), offMs, offResident, onMs, onResident)
	if onMs < 0 || onMs > worldFillBoundMs {
		t.Fatalf("with the cave window on the fill settled after %d ms (bound %d ms)", onMs, worldFillBoundMs)
	}
	if offMs >= 0 && onMs > offMs+monster.NestHiveTickMs {
		t.Fatalf("on settled after %d ms, off after %d ms: the cave window holds admission", onMs, offMs)
	}
}
