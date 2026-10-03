package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

const lifecycleRegion = uint16(25258)

var lifecycleBase = time.UnixMilli(1_784_000_000_000)

func lifecycleRef(typeID4 uint8) monster.MonsterRef {
	return monster.MonsterRef{RefObjID: 1, TidWord: 0x00C6, TypeID4: typeID4, MaxHP: 100, ScaleDenom: 100}
}

func lifecycleNest(x float64) monster.NestRow {
	return monster.NestRow{
		SpawnPoint:         monster.SpawnPoint{RefObjID: 1, RegionID: lifecycleRegion, X: x, Y: 10, Z: 100},
		RetailEvidence:     true,
		MaxCount:           1,
		Respawn:            true,
		RespawnDelayMinSec: 10,
		RespawnDelayMaxSec: 10,
	}
}

// constantWord pins every population draw to one rand() result.
func constantWord(word uint32) func() float64 {
	return func() float64 { return (float64(word) + 0.5) / 32768 }
}

type lifecycleWorld struct {
	t   *testing.T
	s   *MonsterState
	now time.Time
}

func newLifecycleWorld(t *testing.T, ref monster.MonsterRef, nests ...monster.NestRow) *lifecycleWorld {
	t.Helper()
	w := &lifecycleWorld{t: t, now: lifecycleBase}
	w.s = NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{1: ref}, nests))
	w.s.SetTimeSource(func() time.Time { return w.now })
	w.s.SetRandomSource(constantWord(16384))
	return w
}

func (w *lifecycleWorld) at(offsetMs int64) []monster.Instance {
	target := lifecycleBase.Add(time.Duration(offsetMs) * time.Millisecond)
	for next := w.now.Add(time.Second); !next.After(target); next = next.Add(time.Second) {
		w.now = next
		w.s.AdvancePopulation(next.UnixMilli())
	}
	w.now = target
	w.s.StartDivision("division")
	w.s.AdvancePopulation(w.s.CurrentTimeMillis())
	return w.s.InstancesInRegions("division", []uint16{lifecycleRegion})
}

func (w *lifecycleWorld) expectLive(offsetMs int64, want int) []monster.Instance {
	w.t.Helper()
	live := w.at(offsetMs)
	if len(live) != want {
		w.t.Fatalf("+%dms live = %d, want %d", offsetMs, len(live), want)
	}
	return live
}

func (w *lifecycleWorld) kill(offsetMs int64, gid uint32) {
	w.t.Helper()
	if !w.s.Defeat("division", gid, lifecycleBase.Add(time.Duration(offsetMs)*time.Millisecond)) {
		w.t.Fatalf("defeat of %d rejected", gid)
	}
}

func (w *lifecycleWorld) nest(index int) *nestRuntime {
	return w.s.divs["division"].nests[index]
}

func TestPopulationAdvancesWithoutViewersAndObservationDoesNotFillNests(t *testing.T) {
	a, b := lifecycleNest(100), lifecycleNest(200)
	a.MaxCount, b.MaxCount = 3, 3
	b.RegionID++
	a.HiveKey, b.HiveKey = "ordinary", "ordinary"
	a.HiveOrder, b.HiveOrder = 1, 0
	w := newLifecycleWorld(t, lifecycleRef(1), a, b)
	w.s.StartDivision("division")
	if len(w.s.MaterializedInstances("division")) != 0 {
		t.Fatal("world creation bypassed timers")
	}
	w.s.AdvancePopulation(w.now.UnixMilli())
	live := w.s.MaterializedInstances("division")
	if len(live) != 2 || live[0].Spawn.RegionID != b.RegionID {
		t.Fatalf("viewer-free callback lost hive order: %+v", live)
	}
	w.expectLive(0, 1)
	w.expectLive(9999, 1)
	w.now = lifecycleBase.Add(10000 * time.Millisecond)
	w.s.AdvancePopulation(w.now.UnixMilli())
	if len(w.s.MaterializedInstances("division")) != 4 {
		t.Fatal("unobserved region did not refill")
	}
}

// 55EA90 refills a nest one monster per interval: after a wipe the timer
// restarts at the first death from full, and each spawn re-rolls it.
func TestNestRefillsOneMonsterPerInterval(t *testing.T) {
	nest := lifecycleNest(100)
	nest.MaxCount = 3
	w := newLifecycleWorld(t, lifecycleRef(1), nest)
	w.expectLive(-20000, 1)
	w.expectLive(-10000, 2)
	live := w.expectLive(0, 3)
	for _, instance := range live {
		w.kill(1000, instance.Gid)
	}
	w.expectLive(10999, 0)
	w.expectLive(11000, 1)
	w.expectLive(20999, 1)
	w.expectLive(21000, 2)
	w.expectLive(31000, 3)
}

// 560D8B restarts the timer only for a nest dying from full.
func TestNestDeathBelowCapacityKeepsTheRunningTimer(t *testing.T) {
	nest := lifecycleNest(100)
	nest.MaxCount = 2
	w := newLifecycleWorld(t, lifecycleRef(1), nest)
	w.expectLive(-10000, 1)
	live := w.expectLive(0, 2)
	w.kill(0, live[0].Gid)
	w.kill(5000, live[1].Gid)
	w.expectLive(9999, 0)
	w.expectLive(10000, 1)
}

// 5F6EB0: a blocked ordinary candidate fails without touching the timer
// and retries on the next hive callback.
func TestBlockedOrdinaryCandidateRetriesNextCallback(t *testing.T) {
	nest := lifecycleNest(100)
	nest.GenerateRadius = 30
	w := newLifecycleWorld(t, lifecycleRef(1), nest)
	blocked := true
	calls := 0
	w.s.SetSpawnCollisionTest(func(from, to Spawn) MonsterSpawnMove {
		calls++
		if from.RegionID != lifecycleRegion || from.X != 100 || from.Z != 100 || from.Y != 10 {
			t.Fatalf("move test from %+v, want the nest centre", from)
		}
		if blocked {
			return MonsterSpawnMove{Result: monster.NavResultBlocked, Rest: from}
		}
		return MonsterSpawnMove{Rest: to}
	})
	w.expectLive(0, 0)
	if calls != 1 {
		t.Fatalf("initial callback made %d move tests, want one", calls)
	}
	if n := w.nest(0); n.lastMs != 0 || n.intervalMs != 10000 {
		t.Fatalf("rejected creation touched the timer: %+v", *n)
	}
	blocked = false
	w.expectLive(999, 0)
	live := w.expectLive(1000, 1)
	if live[0].Spawn.X == 100 && live[0].Spawn.Z == 100 {
		t.Fatalf("admitted candidate collapsed to the centre: %+v", live[0].Spawn)
	}
}

// 5F7078: a blocked candidate keeps a promoted grade at the nest centre.
func TestBlockedPromotedCandidateSpawnsAtTheCentre(t *testing.T) {
	nest := lifecycleNest(100)
	nest.GenerateRadius = 30
	nest.HasChampionTactics = true
	nest.ChampionGenPercentage = 100
	w := newLifecycleWorld(t, lifecycleRef(1), nest)
	w.s.SetSpawnCollisionTest(func(from, _ Spawn) MonsterSpawnMove {
		return MonsterSpawnMove{Result: monster.NavResultBlocked, Rest: from}
	})
	live := w.expectLive(0, 1)
	if live[0].Rarity()&0x0f == 0 || live[0].Spawn.X != 100 || live[0].Spawn.Z != 100 {
		t.Fatalf("promoted blocked spawn = rarity %#02x at %+v, want the centre", live[0].Rarity(), live[0].Spawn)
	}
}

// 560E10: a clipped candidate halves the interval (floor 1 s) and restarts
// the timer.
func TestClippedCandidateHalvesTheNestInterval(t *testing.T) {
	nest := lifecycleNest(100)
	nest.GenerateRadius = 30
	w := newLifecycleWorld(t, lifecycleRef(1), nest)
	clipped := false
	w.s.SetSpawnCollisionTest(func(from, to Spawn) MonsterSpawnMove {
		if clipped {
			return MonsterSpawnMove{Result: monster.NavResultClipped, Rest: from}
		}
		return MonsterSpawnMove{Rest: to}
	})
	live := w.expectLive(0, 1)
	w.kill(0, live[0].Gid)
	clipped = true
	w.expectLive(10000, 0)
	if n := w.nest(0); n.intervalMs != 5000 || n.lastMs != lifecycleBase.Add(10*time.Second).UnixMilli() {
		t.Fatalf("clipped creation timer = %+v, want interval 5000 restarted at +10s", *n)
	}
	clipped = false
	w.expectLive(14999, 0)
	w.expectLive(15000, 1)
}

// 5608CF / 560CAD: an armed nest rolls a party monster and disarms; monster
// TID4 4 draws the roll but is never promoted to a party monster.
func TestArmedNestSpawnsOnePartyMonster(t *testing.T) {
	for _, tc := range []struct {
		name      string
		typeID4   uint8
		wantGrade uint8
		stayArmed bool
	}{
		{name: "ordinary", typeID4: 1, wantGrade: 0x10},
		{name: "quest monster", typeID4: 4, wantGrade: 0, stayArmed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newLifecycleWorld(t, lifecycleRef(tc.typeID4), lifecycleNest(100))
			live := w.expectLive(0, 1)
			w.kill(0, live[0].Gid)
			w.nest(0).partyArmed = true
			live = w.expectLive(10000, 1)
			if live[0].Rarity() != tc.wantGrade || w.nest(0).partyArmed != tc.stayArmed {
				t.Fatalf("grade %#02x armed=%v, want %#02x armed=%v", live[0].Rarity(), w.nest(0).partyArmed, tc.wantGrade, tc.stayArmed)
			}
			if tc.wantGrade == 0x10 && live[0].EffectiveMaxHP() != 1000 {
				t.Fatalf("party monster HP = %d, want x10", live[0].EffectiveMaxHP())
			}
		})
	}
}

func overwriteHiveNests(limit int, orders ...int) []monster.NestRow {
	nests := make([]monster.NestRow, len(orders))
	for i, order := range orders {
		nests[i] = lifecycleNest(float64(100 + 100*i))
		nests[i].MaxCount = 2
		nests[i].HiveKey = "hive"
		nests[i].HiveMaxCount = limit
		nests[i].HiveOrder = order
	}
	return nests
}

// 55EC10 fills an overwrite hive one monster per member per callback, in
// native hive order, until the shared limit.
func TestOverwriteHiveFillsInHiveOrder(t *testing.T) {
	w := newLifecycleWorld(t, lifecycleRef(1), overwriteHiveNests(2, 2, 0, 1)...)
	live := w.expectLive(0, 2)
	for _, instance := range live {
		if instance.Nest.HiveOrder == 2 {
			t.Fatalf("hive order 2 filled before orders 0 and 1: %+v", live)
		}
	}
	if live[0].Nest.X == live[1].Nest.X {
		t.Fatalf("one member took two monsters before the others took one: %+v", live)
	}
}

// 55EEC0: a single-occupant hive dying from full draws its next location
// (rand()%(n+1), the extra bucket folding onto the last member) and every
// member restarts its timer.
func TestSingleOccupantHiveMovesToTheDrawnMember(t *testing.T) {
	w := newLifecycleWorld(t, lifecycleRef(1), overwriteHiveNests(1, 1, 2, 0)...)
	live := w.expectLive(0, 1)
	if live[0].Nest.HiveOrder != 0 {
		t.Fatalf("initial occupant at hive order %d, want 0", live[0].Nest.HiveOrder)
	}
	w.s.SetRandomSource(constantWord(3)) // 3 % 4 folds onto the last member
	w.kill(2000, live[0].Gid)
	w.expectLive(11999, 0)
	live = w.expectLive(12000, 1)
	if live[0].Nest.HiveOrder != 2 {
		t.Fatalf("respawned at hive order %d, want the drawn last member", live[0].Nest.HiveOrder)
	}
}

// 5609EE: an ordinary monster is created facing the drawn heading, and the
// mover publishes it.
func TestSpawnHeadingReachesTheMoverPose(t *testing.T) {
	w := newLifecycleWorld(t, lifecycleRef(1), lifecycleNest(100))
	live := w.expectLive(0, 1)
	fraction := float32(16384.0 / 32767.0)
	want := HeadingWordFromRadians(float64(float32(float64(fraction) * 6.2831854820251465)))
	mover := w.s.divs["division"].movers.get(live[0].Gid)
	if live[0].SpawnHeading != want || mover.Pose.Heading != want {
		t.Fatalf("heading instance=%d mover=%d, want %d", live[0].SpawnHeading, mover.Pose.Heading, want)
	}
}
