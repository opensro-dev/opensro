package grounditem

import (
	"math"
	"testing"
	"time"

	worldgeom "opensro.online/server/internal/game/world"
)

// movementModeRun is any mode that is not the walk mode.
const movementModeRun uint8 = 3

func TestDistance2DWithinOneRegion(t *testing.T) {
	from := Point{RegionID: 0x6B4F, X: 1200, Z: 400}
	to := Point{RegionID: 0x6B4F, X: 1203, Z: 404}

	if got, want := Distance2D(from, to), 5.0; math.Abs(got-want) > 1e-6 {
		t.Fatalf("distance = %v, want %v", got, want)
	}
}

// The x sector is the region's low byte and the z sector its high byte, each
// worth 1920 units, so a one-sector step is 1920 units away.
func TestDistance2DAcrossSectors(t *testing.T) {
	cases := []struct {
		name string
		from Point
		to   Point
		want float64
	}{
		{
			name: "one sector east",
			from: Point{RegionID: 0x6B4F, X: 0, Z: 0},
			to:   Point{RegionID: 0x6B50, X: 0, Z: 0},
			want: RegionSize,
		},
		{
			name: "one sector north",
			from: Point{RegionID: 0x6B4F, X: 0, Z: 0},
			to:   Point{RegionID: 0x6C4F, X: 0, Z: 0},
			want: RegionSize,
		},
		{
			name: "the sector boundary is continuous",
			from: Point{RegionID: 0x6B4F, X: 1919, Z: 0},
			to:   Point{RegionID: 0x6B50, X: 0, Z: 0},
			want: 1,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Distance2D(testCase.from, testCase.to); math.Abs(got-testCase.want) > 1e-6 {
				t.Fatalf("distance = %v, want %v", got, testCase.want)
			}
		})
	}
}

// The dungeon bit is not part of the sector grid: it must not add 128 sectors
// of z distance, and two positions that disagree on it are not comparable.
func TestDungeonBitStaysOutOfTheSectorMath(t *testing.T) {
	overworld := Point{RegionID: 0x6B4F, X: 100, Z: 200}
	dungeon := Point{RegionID: 0x6B4F | worldgeom.DungeonRegionBit, X: 100, Z: 200}

	// Same sector coordinates inside the dungeon world: distance is zero.
	sameDungeon := Point{RegionID: 0x6B4F | worldgeom.DungeonRegionBit, X: 100, Z: 200}
	if got := Distance2D(dungeon, sameDungeon); got != 0 {
		t.Fatalf("distance between identical dungeon positions = %v, want 0", got)
	}

	if SameWorld(overworld, dungeon) {
		t.Fatal("an overworld and a dungeon position were reported as the same world")
	}
	if !SameWorld(overworld, Point{RegionID: 0x6C50}) {
		t.Fatal("two overworld positions were reported as different worlds")
	}
}

func TestSpeedForMovementMode(t *testing.T) {
	if got := SpeedForMovementMode(MovementModeWalk); got != WalkSpeed {
		t.Fatalf("walk speed = %v, want %v", got, WalkSpeed)
	}
	// Anything that is not the walk mode runs, including an unset mode.
	for _, mode := range []uint8{0, 1, 3, 255} {
		if got := SpeedForMovementMode(mode); got != RunSpeed {
			t.Fatalf("mode %d speed = %v, want %v", mode, got, RunSpeed)
		}
	}
}

func TestPlanApproachExecutesWithinRange(t *testing.T) {
	// A 3-4-5 triangle: 5 units, inside the measured 10u execute range.
	from := Point{RegionID: 0x6B4F, X: 1200, Z: 400}
	to := Point{RegionID: 0x6B4F, X: 1203, Z: 404}

	got := PlanApproach(from, to, movementModeRun)
	if !got.InRange {
		t.Fatalf("a %v unit gap was not in range (limit %v)", got.Distance, ExecuteRange)
	}
	if got.Travel != 0 {
		t.Fatalf("travel = %v, want 0 when in range", got.Travel)
	}
}

// The measured native range: the server pickup executor's single global
// float 10.0, compared inclusively. A gap just past it walks.
func TestExecuteRangeIsTheMeasuredTenUnits(t *testing.T) {
	if ExecuteRange != 10.0 {
		t.Fatalf("ExecuteRange = %v, want the measured 10.0", ExecuteRange)
	}
	from := Point{RegionID: 0x6B4F, X: 0, Z: 0}
	if got := PlanApproach(from, Point{RegionID: 0x6B4F, X: 10.5, Z: 0}, movementModeRun); got.InRange {
		t.Fatalf("a 10.5 unit gap was in range; the limit is inclusive at exactly %v", ExecuteRange)
	}
}

func TestPlanApproachAtExactlyTheRangeLimit(t *testing.T) {
	from := Point{RegionID: 0x6B4F, X: 0, Z: 0}
	to := Point{RegionID: 0x6B4F, X: float32(ExecuteRange), Z: 0}

	if got := PlanApproach(from, to, movementModeRun); !got.InRange {
		t.Fatalf("a gap of exactly %v was not in range", ExecuteRange)
	}
}

// Out of range, retail walks the character there rather than refusing, and the
// travel time is the distance over the character's speed.
func TestPlanApproachWalksWhenOutOfRange(t *testing.T) {
	from := Point{RegionID: 0x6B4F, X: 0, Z: 0}
	to := Point{RegionID: 0x6B4F, X: 500, Z: 0}

	running := PlanApproach(from, to, movementModeRun)
	if running.InRange {
		t.Fatal("a 500 unit gap was reported as in range")
	}
	if math.Abs(running.Distance-500) > 1e-6 {
		t.Fatalf("distance = %v, want 500", running.Distance)
	}
	// 500 units at 50/s is 10s.
	if running.Travel != 10*time.Second {
		t.Fatalf("run travel = %v, want 10s", running.Travel)
	}

	walking := PlanApproach(from, to, MovementModeWalk)
	// 500 units at 20/s is 25s.
	if walking.Travel != 25*time.Second {
		t.Fatalf("walk travel = %v, want 25s", walking.Travel)
	}
	if walking.Travel <= running.Travel {
		t.Fatal("walking was not slower than running")
	}
}

// Rounding up keeps a pending approach from maturing a tick early.
func TestPlanApproachRoundsTravelUp(t *testing.T) {
	from := Point{RegionID: 0x6B4F, X: 0, Z: 0}
	to := Point{RegionID: 0x6B4F, X: 51, Z: 0}

	got := PlanApproach(from, to, movementModeRun)
	// 51 / 50 = 1.02s -> 1020ms exactly; nudge to a value that needs rounding.
	if got.Travel != 1020*time.Millisecond {
		t.Fatalf("travel = %v, want 1020ms", got.Travel)
	}

	odd := PlanApproach(from, Point{RegionID: 0x6B4F, X: 50.001, Z: 0}, movementModeRun)
	if odd.Travel%time.Millisecond != 0 {
		t.Fatalf("travel %v is not a whole number of milliseconds", odd.Travel)
	}
	if odd.Travel < 1000*time.Millisecond {
		t.Fatalf("travel = %v, want at least 1000ms after rounding up", odd.Travel)
	}
}

func TestPlanApproachRefusesCrossWorld(t *testing.T) {
	from := Point{RegionID: 0x6B4F, X: 100, Z: 100}
	to := Point{RegionID: 0x6B4F | worldgeom.DungeonRegionBit, X: 100, Z: 100}

	got := PlanApproach(from, to, movementModeRun)
	if got.InRange {
		t.Fatal("a cross-world pickup was reported as in range")
	}
	if !math.IsInf(got.Distance, 1) {
		t.Fatalf("cross-world distance = %v, want +Inf", got.Distance)
	}
}

func TestPendingTrackerArmAndTake(t *testing.T) {
	tracker := NewPendingTracker()
	key := PendingKey("1", "asd")
	start := time.Unix(1000, 0)

	tracker.Arm(key, 300001, start.Add(2*time.Second))

	// Before arrival: not matured, and the remaining time is reported.
	matured, remaining := tracker.TakeMatured(key, 300001, start)
	if matured {
		t.Fatal("the approach matured before its arrival time")
	}
	if remaining != 2*time.Second {
		t.Fatalf("remaining = %v, want 2s", remaining)
	}

	// After arrival: matured and consumed.
	matured, remaining = tracker.TakeMatured(key, 300001, start.Add(2*time.Second))
	if !matured {
		t.Fatal("the approach did not mature at its arrival time")
	}
	if remaining != 0 {
		t.Fatalf("remaining = %v, want 0", remaining)
	}
	if _, ok := tracker.Peek(key); ok {
		t.Fatal("a matured approach was not consumed")
	}
}

// An execute request for a different item must not consume the approach in flight.
func TestTakeMaturedIgnoresAnotherItem(t *testing.T) {
	tracker := NewPendingTracker()
	key := PendingKey("1", "asd")
	start := time.Unix(1000, 0)

	tracker.Arm(key, 300001, start)

	if matured, _ := tracker.TakeMatured(key, 300002, start); matured {
		t.Fatal("an approach matured for the wrong item")
	}
	if _, ok := tracker.Peek(key); !ok {
		t.Fatal("querying the wrong item consumed the approach")
	}
}

// The native target-move latch is one slot: a new command replaces it.
func TestArmReplacesThePreviousApproach(t *testing.T) {
	tracker := NewPendingTracker()
	key := PendingKey("1", "asd")
	start := time.Unix(1000, 0)

	tracker.Arm(key, 300001, start.Add(time.Hour))
	tracker.Arm(key, 300002, start)

	entry, ok := tracker.Peek(key)
	if !ok {
		t.Fatal("no approach is armed")
	}
	if entry.ItemGid != 300002 {
		t.Fatalf("armed item = %d, want the replacement 300002", entry.ItemGid)
	}
}

func TestClearIsSafeWhenNothingArmed(t *testing.T) {
	tracker := NewPendingTracker()
	key := PendingKey("1", "asd")

	tracker.Clear(key)
	if _, ok := tracker.Peek(key); ok {
		t.Fatal("clearing an empty tracker armed something")
	}

	tracker.Arm(key, 300001, time.Unix(1000, 0))
	tracker.Clear(key)
	if _, ok := tracker.Peek(key); ok {
		t.Fatal("Clear did not remove the approach")
	}
}

func TestTakeMaturedOnUnknownKey(t *testing.T) {
	tracker := NewPendingTracker()

	if matured, remaining := tracker.TakeMatured(PendingKey("1", "nobody"), 300001, time.Unix(1000, 0)); matured || remaining != 0 {
		t.Fatalf("unknown key = matured %v, remaining %v; want false and 0", matured, remaining)
	}
}
