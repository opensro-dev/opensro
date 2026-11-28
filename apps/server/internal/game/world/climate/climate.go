/*
===========================================================================

climate.go - the native per-climate weather cycle

Package climate owns the reference shard's weather: every _RefClimate row
runs one state machine, and every _RefRegion row names the climate whose
weather its players see. The state machine is a port of the reference
shard manager's climate update (verified by disassembly of the v1.188
shard manager; the long trail lives in ../research): a periodic roll picks
a rain, snow or clear spell from the climate's chances, the spell lasts its
duration, then the climate returns to its initial weather. The current
weather never jumps: it fades toward the target in tenths, one step at most
every three seconds, and a change of kind fades the old kind out to zero
before the new one rises.

A GM's /WEATHER (State.Force, port-only, not native) starts a spell of
its own on one climate; it then runs out and fades home like a rolled one.

The package holds no clock and no random source of its own: the caller
passes the time and a rand()-style source, so tests are deterministic. It
knows nothing about players or packets; action/weather.go delivers.

===========================================================================
*/
package climate

import "math/rand/v2"

// The weather kinds the client's 0x3BDE takes (byte 0); any other value
// draws clear.
const (
	Clear uint8 = 1
	Rain  uint8 = 2
	Snow  uint8 = 3
)

const (
	// cyclePeriodMs is divided by the climate's Division to give the roll
	// period: Division 4 rolls every 52.5 minutes.
	cyclePeriodMs = 12_600_000
	minDivision   = 1
	maxDivision   = 60

	// A spell's base duration is clamped into [30 s, 30 min].
	minDurationMs = 30_000
	maxDurationMs = 1_800_000

	// stepIntervalMs is the fade's pace: one step at most every 3 s.
	stepIntervalMs = 3000
	// A transition takes about stepDivisor steps; its distance is clamped
	// into [minStepDistance, maxStepDistance] first.
	stepDivisor     = 10
	minStepDistance = 10
	maxStepDistance = 510

	// The port caps a drawn amount at maxAmount. Natively the amount is an
	// 8-bit add (add dl, base; cmp dl, 0xFF), so a sum over 255 wraps
	// instead. No shipped row reaches it (the largest is 3002's
	// 150 + 40 = 190), so the cap is not a native rule to rely on, and
	// it must not be "fixed" into a wrap either.
	maxAmount = 255

	// An InitialAmount of zero draws randomInitialBase + rand%randomInitialSpan.
	randomInitialBase = 50
	randomInitialSpan = 151

	// A rolled clear spell's amount: clearAmountBase + rand%clearAmountSpan.
	clearAmountBase = 150
	clearAmountSpan = 51

	// The roll is rand%rollSpan + 1, a percent compared with the chances.
	rollSpan = 100

	// nativeRandMax is the C runtime's RAND_MAX + 1: the native source
	// answers 0..32767.
	nativeRandMax = 32768
)

// A GM's /WEATHER spell (Force; port-only, not native). The original has no
// command that sets the weather, so these bounds are the port's: a spell
// lasts at least a minute and at most two hours, and an amount of zero
// takes the climate's own rain or snow base (defaultForceAmount when the
// row has none, as most clear-sky climates do).
const (
	MinForceDurationMs = 60_000
	MaxForceDurationMs = 7_200_000
	defaultForceAmount = 150
)

/*
================
Row

One _RefClimate row, as the importer reads it. Durations are milliseconds;
the chances are percents; amounts are the client's 0..255.
================
*/
type Row struct {
	ID               int32
	InitialWeather   uint8
	InitialAmount    uint8
	ChangeWeather    uint8
	Division         uint8
	Duration         int32
	DurationVariance int32
	Snowfall         uint8
	SnowfallVariance uint8
	ProbSnow         uint8
	Rainfall         uint8
	RainfallVariance uint8
	ProbRain         uint8
}

/*
================
regionClimateRange

Inclusive, non-overlapping region ranges generated from _RefRegion.Climate.
================
*/
type regionClimateRange struct {
	first, last uint16
	climate     int32
}

/*
================
Weather

What a client is told: the kind and its amount.
================
*/
type Weather struct {
	Kind   uint8
	Amount uint8
}

/*
================
Rand

A rand()-style source: a non-negative int. The state machine takes it
modulo each span, as the native code takes rand() modulo.
================
*/
type Rand func() int

/*
================
NativeRand

The production source: the C runtime's range, 0..32767.
================
*/
func NativeRand() int {
	return rand.IntN(nativeRandMax)
}

/*
================
State

One climate's weather. The current weather is what players see; the
target is where the fade is heading. All times are milliseconds on the
caller's clock.
================
*/
type State struct {
	row            Row
	cur, target    Weather
	period         int64
	nextRollAt     int64
	eventStartAt   int64
	eventDuration  int64
	lastStepAt     int64
	step           int
	random         Rand
	initialWeather Weather
	// gm is true while a GM's spell (Force) holds the climate: from the
	// command until the spell's duration ends, before the fade home.
	// gmReported is the value the last Update reported, so a flip is a
	// change even when the weather word does not move.
	gm, gmReported bool
}

/*
================
NewState

The climate's start: its initial weather, the first roll one period away.
An InitialAmount of zero draws one random amount for both the current and
the target weather.
================
*/
func NewState(row Row, nowMs int64, random Rand) State {
	if random == nil {
		random = NativeRand
	}
	division := min(max(int64(row.Division), minDivision), maxDivision)
	row.Duration = min(max(row.Duration, minDurationMs), maxDurationMs)
	s := State{row: row, random: random, period: cyclePeriodMs / division}
	s.nextRollAt = nowMs + s.period
	s.initialWeather = Weather{Kind: row.InitialWeather, Amount: row.InitialAmount}
	s.cur = s.initialWeather
	if row.InitialAmount == 0 {
		s.cur.Amount = uint8(randomInitialBase + s.rand(randomInitialSpan))
	}
	s.target = s.cur
	s.eventStartAt, s.lastStepAt = nowMs, nowMs
	return s
}

/*
================
State.Current
================
*/
func (s *State) Current() Weather { return s.cur }

/*
================
State.GMSpell

Whether a GM's spell (Force) holds the climate: from the command until
the spell's duration ends, before the fade home. A seasonal sky gives way
to it (action/seasonal_sky.go); Update reports each flip as a change.
================
*/
func (s *State) GMSpell() bool { return s.gm }

/*
================
State.Update

One server tick. Reports whether the current weather changed, which is
when its players must be told. A climate that never changes weather
(ChangeWeather 0) stays at its initial weather for good.
================
*/
func (s *State) Update(nowMs int64) bool {
	if s.row.ChangeWeather == 0 {
		// Natively such a climate never moves. Only a GM's spell (Force)
		// sets it going; it runs out and fades home like any spell, and
		// once home and idle the climate is still again.
		if s.cur == s.target && s.eventDuration == 0 && s.gm == s.gmReported {
			return false
		}
	} else if nowMs > s.nextRollAt {
		s.roll(nowMs)
	}
	if s.cur == s.target && s.eventDuration != 0 && nowMs-s.eventStartAt > s.eventDuration {
		// The spell is over: back to the climate's initial weather. The
		// stored row value, not a drawn one (no shipped row has zero).
		s.target = s.initialWeather
		s.eventDuration = 0
		s.gm = false
		s.updateStep()
	}
	moved := s.fade(nowMs)
	// A spell starting or ending changes what a seasonal sky shows even
	// when the weather word stands still (a spell on the weather already
	// falling, or one ending on the climate's initial weather).
	if s.gm != s.gmReported {
		s.gmReported = s.gm
		return true
	}
	return moved
}

/*
================
State.roll

The periodic roll. The next roll is always one period away; a spell still
running blocks this one. Otherwise one percent picks snow, then rain, then
clear, in that order of the climate's chances, and a fresh duration.
================
*/
func (s *State) roll(nowMs int64) {
	s.nextRollAt = nowMs + s.period
	if nowMs-s.eventStartAt < s.eventDuration {
		return
	}
	// A roll past the spell's end replaces a GM's spell as well as a rolled
	// one, so the spell is over here too (Update reports the flip).
	s.gm = false
	s.eventStartAt = nowMs
	r := s.rand(rollSpan) + 1
	switch {
	case r <= int(s.row.ProbSnow):
		s.target = Weather{Kind: Snow, Amount: s.spellAmount(s.row.Snowfall, s.row.SnowfallVariance)}
	case r <= int(s.row.ProbSnow)+int(s.row.ProbRain):
		s.target = Weather{Kind: Rain, Amount: s.spellAmount(s.row.Rainfall, s.row.RainfallVariance)}
	default:
		s.target = Weather{Kind: Clear, Amount: uint8(clearAmountBase + s.rand(clearAmountSpan))}
	}
	s.eventDuration = int64(s.row.Duration) + int64(s.rand(int(s.row.DurationVariance)+1))
	s.updateStep()
}

/*
================
State.Force

A GM's weather spell (port-only, not native; gmcommand's /WEATHER): the
climate heads for the weather through the native fade, holds it for
durationMs, then returns to its initial weather exactly as a rolled spell
does. The periodic roll cannot replace it while it runs (roll returns
early inside a spell). An amount of zero is the climate's own base for
the kind; durationMs is clamped into the force bounds. Returns the weather
the climate is heading for.
================
*/
func (s *State) Force(nowMs int64, weather Weather, durationMs int64) Weather {
	if weather.Amount == 0 {
		weather.Amount = s.forceAmount(weather.Kind)
	}
	s.target = weather
	s.gm = true
	s.eventStartAt = nowMs
	s.eventDuration = min(max(durationMs, MinForceDurationMs), MaxForceDurationMs)
	s.updateStep()
	return weather
}

/*
================
State.forceAmount

The amount a GM's spell takes when none was typed: the row's rain or snow
base, else defaultForceAmount.
================
*/
func (s *State) forceAmount(kind uint8) uint8 {
	switch {
	case kind == Snow && s.row.Snowfall != 0:
		return s.row.Snowfall
	case kind == Rain && s.row.Rainfall != 0:
		return s.row.Rainfall
	}
	return defaultForceAmount
}

/*
================
State.spellAmount

A rain or snow spell's amount: the base plus up to its variance, capped at
the client's byte.
================
*/
func (s *State) spellAmount(base, variance uint8) uint8 {
	return uint8(min(int(base)+s.rand(int(variance)+1), maxAmount))
}

/*
================
State.updateStep

The fade's step for the current transition: a tenth of the distance, where
a change of kind travels down to zero and up again (the sum of both
amounts). The distance is clamped first, so a step is 1 to 51.
================
*/
func (s *State) updateStep() {
	distance := int(s.cur.Amount) + int(s.target.Amount)
	if s.cur.Kind == s.target.Kind {
		distance = int(s.cur.Amount) - int(s.target.Amount)
		if distance < 0 {
			distance = -distance
		}
	}
	distance = min(max(distance, minStepDistance), maxStepDistance)
	s.step = distance / stepDivisor
}

/*
================
State.fade

One step toward the target, at most every stepIntervalMs. A change of
kind lowers the old kind to zero, then switches kind at zero; the same
kind moves toward the target amount without passing it.
================
*/
func (s *State) fade(nowMs int64) bool {
	if s.cur == s.target || nowMs-s.lastStepAt < stepIntervalMs {
		return false
	}
	s.lastStepAt = nowMs
	amount := int(s.cur.Amount)
	if s.cur.Kind != s.target.Kind {
		amount = max(amount-s.step, 0)
		if amount == 0 {
			s.cur.Kind = s.target.Kind
		}
	} else if amount < int(s.target.Amount) {
		amount = min(amount+s.step, int(s.target.Amount))
	} else {
		amount = max(amount-s.step, int(s.target.Amount))
	}
	s.cur.Amount = uint8(amount)
	return true
}

/*
================
State.rand

rand() modulo span, as the native code draws.
================
*/
func (s *State) rand(span int) int {
	return s.random() % span
}
