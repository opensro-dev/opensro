/*
===========================================================================

satiety.go - attack-pet hunger clock and parameter-keeper contribution

The actor lifetime owns the fractional clock. Durable HGP belongs to the
companion record; offline time never advances this clock.

===========================================================================
*/
package companion

import (
	"math"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/paramkeeper"
)

const (
	MaximumSatiety        = 10000
	HungrySatiety         = 3000
	minutesToSeconds      = 60
	millisecondsToSeconds = 1000
	hungryFactor          = 50
)

/*
================
SatietyClock

4D5E00 retains a float32 fractional drain, reset by actor initialization.
================
*/
type SatietyClock struct {
	GID       uint32
	LastMs    int64
	Started   bool
	Carry     float32
	Exhausted bool
}

/*
================
Advance

6A602E derives HGP/second as 100 / (reference parameter 4 * 60).
4D5E72 and 4D5E84 each spill to float32 before extracting whole points.
A replaced actor and a backwards clock establish a new time origin.
================
*/
func (clock *SatietyClock) Advance(gid uint32, nowMs int64, minutes uint32, satiety uint16) uint16 {
	if !clock.Started || clock.GID != gid || nowMs < clock.LastMs {
		*clock = SatietyClock{GID: gid, LastMs: nowMs, Started: true}
		return satiety
	}
	clock.Exhausted = false
	elapsed := float32(float64(nowMs-clock.LastMs) / millisecondsToSeconds)
	clock.LastMs = nowMs
	if minutes == 0 {
		return satiety
	}
	rate := float32(100 / (float64(minutes) * minutesToSeconds))
	drain := float32(float64(rate) * float64(elapsed))
	clock.Carry = float32(float64(clock.Carry) + float64(drain))
	if clock.Carry < 1 {
		return satiety
	}
	whole := math.Trunc(float64(clock.Carry))
	clock.Carry = float32(float64(clock.Carry) - whole)
	if whole >= float64(satiety) {
		clock.Exhausted = true
		return 0
	}
	return satiety - uint16(whole)
}

/*
================
PublishSatiety

4D5F49 publishes when the old truncated percentage is divisible by ten and
the new one is not. Death also publishes the exhausted gauge. This follows
the native predicate rather than manufacturing a packet for every point.
================
*/
func PublishSatiety(before, after uint16) bool {
	if before == after {
		return false
	}
	return after == 0 || (int(before)/100)%10 == 0 && (int(after)/100)%10 != 0
}

/*
================
HungryParameter

4D60A0 contributes source-zero factor 50 to exactly these eight parameters.
Resistances, movement, maxima, parry, block and critical chance are unaffected.
================
*/
func HungryParameter(id uint16) bool {
	return id == 5 || id == 6 || id == 9 || id == 11 || id >= 13 && id <= 16
}

/*
================
Parameter

Replay hunger and status writes into one keeper so flat replacements and
clamps remain in native order. Feeding removes hunger by rebuilding this
projection, never by modifying or erasing the abnormal-state owner.
================
*/
func Parameter(id uint16, base float32, satiety uint16, block *abnormal.Block) (float32, error) {
	return projectParameter(parameterInput{id: id, base: base, satiety: satiety, block: block})
}

/*
================
parameterInput
================
*/
type parameterInput struct {
	runFactor  float32
	runSet     bool
	id         uint16
	base       float32
	satiety    uint16
	attributes uint8
	block      *abnormal.Block
}

/*
================
projectParameter

Guild attributes and abnormal writes share one native keeper evaluation.
================
*/
func projectParameter(in parameterInput) (float32, error) {
	id, base, satiety, block := in.id, in.base, in.satiety, in.block
	channel, attribute := mercenaryModifier(id, in.attributes)
	hungry := satiety < HungrySatiety && HungryParameter(id)
	touched := block != nil && block.Touches(id)
	if !hungry && !touched && !attribute && !in.runSet {
		return base, nil
	}
	definition, exists := paramkeeper.NativeDefinition(id)
	if !exists {
		return base, nil
	}
	element, err := paramkeeper.New(definition)
	if err != nil {
		return 0, err
	}
	if _, err = element.Apply(paramkeeper.Flat, 0, base); err != nil {
		return 0, err
	}
	if attribute {
		if _, err = element.Apply(channel, mercenaryAttributeSource, mercenaryAttributeAmount); err != nil {
			return 0, err
		}
	}
	if in.runSet {
		if _, err = element.Apply(paramkeeper.FactorProduct, 0, in.runFactor); err != nil {
			return 0, err
		}
	}
	if hungry {
		if _, err = element.Apply(paramkeeper.FactorProduct, 0, hungryFactor); err != nil {
			return 0, err
		}
	}
	if block != nil {
		if err = block.ApplyTo(id, element); err != nil {
			return 0, err
		}
	}
	return element.Value()
}

/*
================
FollowRunParameter

Source-zero channel 3 combines with abnormal speed factors in one keeper.
================
*/
func FollowRunParameter(base, factor float32, installed bool, block *abnormal.Block) (float32, error) {
	return projectParameter(parameterInput{id: 0x18, base: base, satiety: MaximumSatiety, block: block, runFactor: factor, runSet: installed})
}
