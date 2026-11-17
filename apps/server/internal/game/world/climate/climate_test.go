/*
===========================================================================

climate_test.go - the climate state machine, step by step

Every test drives the clock by hand and forces each rand() draw, so the
roll, the spell and the fade are checked against the native rules one
decision at a time.

===========================================================================
*/
package climate

import "testing"

/*
================
draws

A rand() source that answers the given values in order and fails the test
when asked once too often.
================
*/
func draws(t *testing.T, values ...int) Rand {
	t.Helper()
	return func() int {
		if len(values) == 0 {
			t.Fatalf("rand() asked once more than the test forced")
		}
		v := values[0]
		values = values[1:]
		return v
	}
}

// rainy is a changing climate with every chance in play.
var rainy = Row{ID: 9001, InitialWeather: Clear, InitialAmount: 180, ChangeWeather: 1, Division: 4,
	Duration: 240000, DurationVariance: 80000, Snowfall: 40, SnowfallVariance: 10, ProbSnow: 30,
	Rainfall: 100, RainfallVariance: 30, ProbRain: 20}

/*
================
TestNewStateClamps
================
*/
func TestNewStateClamps(t *testing.T) {
	for _, tc := range []struct {
		division       uint8
		duration       int32
		period         int64
		storedDuration int32
	}{
		{0, 1000, 12_600_000, 30_000},
		{4, 240000, 3_150_000, 240000},
		{100, 5_000_000, 210_000, 1_800_000},
	} {
		row := rainy
		row.Division, row.Duration = tc.division, tc.duration
		s := NewState(row, 1000, draws(t))
		if s.period != tc.period || s.nextRollAt != 1000+tc.period {
			t.Fatalf("division %d: period %d next %d, want %d", tc.division, s.period, s.nextRollAt, tc.period)
		}
		if s.row.Duration != tc.storedDuration {
			t.Fatalf("duration %d stored as %d, want %d", tc.duration, s.row.Duration, tc.storedDuration)
		}
		if s.eventStartAt != 1000 || s.lastStepAt != 1000 || s.eventDuration != 0 {
			t.Fatalf("start state %+v", s)
		}
		if s.Current() != (Weather{Clear, 180}) || s.target != s.cur {
			t.Fatalf("initial weather %+v target %+v", s.cur, s.target)
		}
	}
}

/*
================
TestNewStateRandomInitialAmount
================
*/
func TestNewStateRandomInitialAmount(t *testing.T) {
	row := rainy
	row.InitialAmount = 0
	// 151*2 + 7: the draw is taken modulo 151, plus 50.
	s := NewState(row, 0, draws(t, 151*2+7))
	if s.cur != (Weather{Clear, 57}) || s.target != s.cur {
		t.Fatalf("random initial %+v target %+v, want clear 57 for both", s.cur, s.target)
	}
}

/*
================
TestStaticClimateNeverChanges
================
*/
func TestStaticClimateNeverChanges(t *testing.T) {
	row := rainy
	row.ChangeWeather = 0
	s := NewState(row, 0, draws(t))
	for _, now := range []int64{1, 3_150_001, 100_000_000} {
		if s.Update(now) {
			t.Fatalf("a ChangeWeather 0 climate changed at %d", now)
		}
	}
	if s.Current() != (Weather{Clear, 180}) {
		t.Fatalf("static climate drifted to %+v", s.Current())
	}
}

/*
================
TestRollThresholds

The percent is rand%100 + 1: up to ProbSnow is snow, up to ProbSnow +
ProbRain is rain, the rest is clear. Each kind then draws its amount and
the spell its duration, in that order.
================
*/
func TestRollThresholds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		draws  []int
		target Weather
		length int64
	}{
		{"snow at the first percent", []int{0, 5, 0}, Weather{Snow, 45}, 240000},
		{"snow at its last percent", []int{29, 10, 80000}, Weather{Snow, 50}, 320000},
		{"rain after snow", []int{30, 0, 7}, Weather{Rain, 100}, 240007},
		{"rain at its last percent", []int{49, 30, 1}, Weather{Rain, 130}, 240001},
		{"clear above both", []int{50, 0, 0}, Weather{Clear, 150}, 240000},
		{"clear at the top", []int{99, 50, 80001}, Weather{Clear, 200}, 240000},
	} {
		s := NewState(rainy, 0, draws(t, tc.draws...))
		roll := s.period + 1
		s.Update(roll)
		if s.target != tc.target || s.eventDuration != tc.length || s.eventStartAt != roll {
			t.Fatalf("%s: target %+v duration %d start %d, want %+v %d %d",
				tc.name, s.target, s.eventDuration, s.eventStartAt, tc.target, tc.length, roll)
		}
		if s.nextRollAt != roll+s.period {
			t.Fatalf("%s: next roll %d, want %d", tc.name, s.nextRollAt, roll+s.period)
		}
	}
}

/*
================
TestRollAmountCaps
================
*/
func TestRollAmountCaps(t *testing.T) {
	row := rainy
	row.Rainfall, row.RainfallVariance = 250, 30
	s := NewState(row, 0, draws(t, 40, 30, 0))
	s.Update(s.period + 1)
	if s.target != (Weather{Rain, 255}) {
		t.Fatalf("capped rain %+v, want rain 255", s.target)
	}
}

/*
================
TestRollWaitsForTheSpell

A roll due while the spell still runs takes no draw and sets no target;
the next roll is still one period away.
================
*/
func TestRollWaitsForTheSpell(t *testing.T) {
	row := rainy
	row.Division = 60 // a 210 s period, shorter than the 240 s spell
	s := NewState(row, 0, draws(t, 30, 0, 0))
	first := s.period + 1
	s.Update(first)
	if s.target != (Weather{Rain, 100}) {
		t.Fatalf("first roll %+v", s.target)
	}
	second := first + s.period + 1
	s.Update(second)
	if s.target != (Weather{Rain, 100}) || s.eventStartAt != first || s.nextRollAt != second+s.period {
		t.Fatalf("blocked roll moved state: target %+v start %d next %d", s.target, s.eventStartAt, s.nextRollAt)
	}
}

/*
================
TestStepSize
================
*/
func TestStepSize(t *testing.T) {
	for _, tc := range []struct {
		cur, target Weather
		step        int
	}{
		{Weather{Clear, 180}, Weather{Clear, 80}, 10},
		{Weather{Clear, 80}, Weather{Clear, 180}, 10},
		{Weather{Clear, 180}, Weather{Clear, 175}, 1},
		{Weather{Clear, 180}, Weather{Rain, 120}, 30},
		{Weather{Snow, 255}, Weather{Rain, 255}, 51},
		{Weather{Clear, 0}, Weather{Rain, 0}, 1},
	} {
		s := State{cur: tc.cur, target: tc.target}
		s.updateStep()
		if s.step != tc.step {
			t.Fatalf("%+v -> %+v: step %d, want %d", tc.cur, tc.target, s.step, tc.step)
		}
	}
}

/*
================
TestFadeAcrossAKindChange

Clear 180 to rain 120 steps by 30: down to zero, the kind switches at
zero, then up to the target without passing it.
================
*/
func TestFadeAcrossAKindChange(t *testing.T) {
	s := NewState(rainy, 0, draws(t, 30, 20, 0))
	now := s.period + 1
	if !s.Update(now) {
		t.Fatalf("the roll's first step did not move the weather")
	}
	want := []Weather{{Clear, 150}, {Clear, 120}, {Clear, 90}, {Clear, 60}, {Clear, 30}, {Rain, 0},
		{Rain, 30}, {Rain, 60}, {Rain, 90}, {Rain, 120}}
	if s.Current() != want[0] {
		t.Fatalf("step 0: %+v, want %+v", s.Current(), want[0])
	}
	for i := 1; i < len(want); i++ {
		now += stepIntervalMs
		if !s.Update(now) || s.Current() != want[i] {
			t.Fatalf("step %d: %+v, want %+v", i, s.Current(), want[i])
		}
	}
	now += stepIntervalMs
	if s.Update(now) {
		t.Fatalf("the fade moved past its target: %+v", s.Current())
	}
}

/*
================
TestFadeSnapsToTarget
================
*/
func TestFadeSnapsToTarget(t *testing.T) {
	s := State{cur: Weather{Rain, 100}, target: Weather{Rain, 125}, step: 10}
	for _, want := range []uint8{110, 120, 125} {
		s.lastStepAt -= stepIntervalMs
		s.fade(s.lastStepAt + stepIntervalMs)
		if s.cur.Amount != want {
			t.Fatalf("fade up %d, want %d", s.cur.Amount, want)
		}
	}
	s = State{cur: Weather{Rain, 100}, target: Weather{Rain, 75}, step: 10}
	for _, want := range []uint8{90, 80, 75} {
		s.lastStepAt -= stepIntervalMs
		s.fade(s.lastStepAt + stepIntervalMs)
		if s.cur.Amount != want {
			t.Fatalf("fade down %d, want %d", s.cur.Amount, want)
		}
	}
}

/*
================
TestFadeWaitsThreeSeconds
================
*/
func TestFadeWaitsThreeSeconds(t *testing.T) {
	s := NewState(rainy, 0, draws(t, 50, 0, 0))
	roll := s.period + 1
	s.Update(roll)
	// Clear 180 to clear 150 steps by 3.
	if s.Current() != (Weather{Clear, 177}) {
		t.Fatalf("first step %+v", s.Current())
	}
	if s.Update(roll + stepIntervalMs - 1) {
		t.Fatalf("stepped before three seconds")
	}
	if !s.Update(roll+stepIntervalMs) || s.Current() != (Weather{Clear, 174}) {
		t.Fatalf("no step at three seconds: %+v", s.Current())
	}
}

/*
================
TestSpellEndsAtInitialWeather

Once the weather has reached the spell and the spell's duration has
passed, the target returns to the initial weather and the fade follows.
================
*/
func TestSpellEndsAtInitialWeather(t *testing.T) {
	s := NewState(rainy, 0, draws(t, 30, 0, 0))
	roll := s.period + 1
	now := roll
	s.Update(now)
	for s.Current() != s.target {
		now += stepIntervalMs
		s.Update(now)
	}
	end := roll + s.eventDuration
	s.Update(end)
	if s.target != (Weather{Rain, 100}) || s.eventDuration == 0 {
		t.Fatalf("the spell ended at its duration, not after it")
	}
	s.Update(end + 1)
	if s.target != (Weather{Clear, 180}) || s.eventDuration != 0 {
		t.Fatalf("after the spell target %+v duration %d, want clear 180 and none", s.target, s.eventDuration)
	}
	// Rain 100 back to clear 180: a kind change, (100+180)/10 = 28.
	if s.step != 28 {
		t.Fatalf("return step %d, want 28", s.step)
	}
}
