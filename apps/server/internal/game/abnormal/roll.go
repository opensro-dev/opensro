/*
===========================================================================

roll.go - the per-hit abnormal roll

===========================================================================
*/

package abnormal

import "math"

/*
================
Record

Record is one native status-effect record. Offsets name the native
fields each consumer reads.
================
*/
type Record struct {
	Status     Status
	DurationMs uint32
	PeriodMs   uint32
	Chance     uint32 // statuses 0..5
	Grade      uint8
	Level      uint16
	Damage1C   uint32  // time-bomb damage
	Scale20    float32 // burn scale
	Rate24     uint32  // burn damage table value
	Param28    uint32  // electric-shock evasion cut
	Param2C    uint32  // panic/combustion maximum cut
	Param34    uint32  // decay/weaken cap, panic/combustion recovery
	Param38    uint32  // poison/bleeding damage, curse magnitude, drain percent
	Param3C    uint32  // myopia range
	Param40    uint32  // bleeding defense cut
	Param44    uint32  // disease bonus
	Param48    uint32  // dark hit-rate cut
	SourceGID  uint32
	TargetGID  uint32
	// SourceName is the port's durable caster identity; native resolves the
	// source by its id alone.
	SourceName string
}

/*
==================
Resistance

Resistance is one native status-resistance entry: a flat chance
reduction halved once per grade above its grade, and a percentage
applied to a positive chance.
==================
*/
type Resistance struct {
	Flat, Grade, Percent int32
}

/*
================
RollInput

RollInput is everything the roll reads besides its random source.
================
*/
type RollInput struct {
	Params *SkillParams
	// SkipGroup is hit-group+05 == 2, which never rolls.
	SkipGroup bool
	// TargetImmune is set for an attack COS or a fortress structure.
	TargetImmune bool
	// WallMask is the optional pw context of the skill record, not
	// hit-result flags. The roll reads its lane mask; Stun is excluded whenever the
	// context is present. A shield-blocked hit is SkipGroup instead.
	WallMask    *uint32
	TargetLevel uint8
	// TargetBonus is target parameter 0xA9 (raised by disease), truncated.
	TargetBonus float32
	// TargetResist and TargetFlat are parameters 1B+i and 91+i for the six
	// element statuses in Sources order.
	TargetResist [6]float32
	TargetFlat   [6]float32
	// Resistance is indexed by Source.Resist.
	Resistance  [17]Resistance
	CasterLevel uint8
	// CasterFortressHeart marks a fortress-heart caster.
	CasterFortressHeart bool
	CasterSiegeGrade    uint8
	// CasterModifier reads the caster's skill modifiers.
	CasterModifier       func(key uint32) (uint32, bool)
	SourceGID, TargetGID uint32
	SourceName           string
}

/*
================
Random

Random is the caster's keyed probability stream and the process rand()
consumed by the time bomb.
================
*/
type Random interface {
	Chance(key uint32, chance int32) bool
	Rand() int32
}

/*
==================
durationPerLevel

durationPerLevel is the native milliseconds per level of states 0..5.
It sizes rolled durations and level-cure cuts. Other slots carry no
per-level duration (0).
==================
*/
func durationPerLevel(status Status, level uint16) uint32 {
	if status > Zombie {
		return 0
	}
	return uint32(level) * [...]uint32{97, 250, 750, 500, 1000, 750}[status]
}

/*
================
burnDamage

burnDamage reads the native burn table by level.
================
*/
func burnDamage(level uint32) uint32 {
	if level >= uint32(len(burnDamageTable)) {
		return 0
	}
	return uint32(burnDamageTable[level])
}

/*
================
ftol

================
*/
func ftol(v float64) int32 {
	if math.IsNaN(v) || v >= 2147483648 || v < -2147483648 {
		return math.MinInt32
	}
	return int32(v)
}

/*
================
Roll

Roll is the per-hit abnormal roll. Returned records are in native list
order.
================
*/
func Roll(in RollInput, random Random) []Record {
	p := in.Params
	if p == nil || in.SkipGroup || in.TargetImmune {
		return nil
	}
	if in.WallMask != nil && *in.WallMask&8 != 0 && !p.Stun() {
		return nil
	}
	bonus := ftol(float64(in.TargetBonus))
	rank := int32(0)
	if bonus != 0 {
		rank = 1
	}
	modifier := func(key uint32) uint32 {
		if in.CasterModifier == nil {
			return 0
		}
		v, _ := in.CasterModifier(key)
		return v
	}
	pulse := uint32(2000)
	if p.PulsePresent {
		pulse = p.Pulse
	}
	var out []Record
	base := func(status Status) Record {
		return Record{Status: status, SourceGID: in.SourceGID, TargetGID: in.TargetGID, SourceName: in.SourceName}
	}
	for index, source := range Sources[:6] {
		param := p.Params[index]
		if !param.Present {
			continue
		}
		if !random.Chance(source.Key, int32(param.Args[1])+bonus) {
			continue
		}
		resist := ftol(float64(in.TargetResist[index]))
		if resist >= 100 {
			continue
		}
		level := x87Fraction(float64(100-resist), float64(param.Args[0]))
		if source.Status == Poison && p.PoisonDurationGetv {
			level += int32(modifier(KeyPoisonDuration))
		}
		level -= ftol(float64(in.TargetFlat[index]))
		if level <= 0 {
			continue
		}
		r := base(source.Status)
		r.Level = uint16(level)
		r.Chance = param.Args[1]
		r.DurationMs = durationPerLevel(source.Status, r.Level)
		switch source.Status {
		case ElectricShock:
			r.Param28 = param.Args[2]
		case Burn:
			r.PeriodMs = pulse
			r.Scale20 = 1
			r.Rate24 = burnDamage(param.Args[2])
		case Poison:
			r.PeriodMs = pulse
			r.Param38 = param.Args[2]
			if p.PoisonDamageGetv {
				r.Param38 += modifier(KeyPoisonDamage)
			}
		}
		out = append(out, r)
	}
	for index := 6; index < SourceCount; index++ {
		source, param := Sources[index], p.Params[index]
		if source.Status == Stun && in.WallMask != nil {
			continue
		}
		if !param.Present {
			continue
		}
		grade := param.Args[2]
		if (source.Status == Slow || source.Status == Stun) && grade == 0 {
			if !in.CasterFortressHeart {
				grade = uint32(in.CasterLevel) / 10
			} else {
				grade = uint32(in.CasterSiegeGrade)
			}
		}
		delta := int32(in.TargetLevel) - (int32(grade)+rank)*10
		if delta < 0 {
			delta = 0
		}
		durationValue, chanceValue := float64(param.Args[0]), float64(float32(param.Args[1]))
		duration := x87Scale(durationValue, float64(delta)*2.5)
		chance := x87Scale(chanceValue, float64(delta*5))
		if source.Status != TimeBomb {
			if floor := ftol(durationValue * 0.5); duration < floor {
				duration = floor
			}
		}
		if floor := ftol(chanceValue * 0.10000000149011612); chance < floor {
			chance = floor
		}
		resistance := in.Resistance[source.Resist]
		flat := resistance.Flat
		if resistance.Grade != 0 {
			for n := int32(param.Args[2]) - resistance.Grade; n > 0 && flat != 0; n-- {
				flat /= 2
			}
		}
		chance = chance - flat + bonus
		if resistance.Percent != 0 && chance > 0 {
			chance = x87Scale(float64(chance), float64(resistance.Percent))
		}
		var bombDuration uint32
		if source.Status == TimeBomb {
			// The nonzero bytes of the first argument are candidate
			// seconds; rand() is drawn before the probability roll.
			var candidates []uint8
			for shift := 0; shift < 32; shift += 8 {
				if b := uint8(param.Args[0] >> shift); b != 0 {
					candidates = append(candidates, b)
				}
			}
			draw := random.Rand()
			if len(candidates) > 0 {
				bombDuration = uint32(candidates[int(draw)%len(candidates)]) * 1000
			}
		}
		if !random.Chance(source.Key, chance) {
			continue
		}
		r := base(source.Status)
		r.Grade = uint8(grade)
		r.DurationMs = uint32(duration)
		switch source.Status {
		case Myopia:
			r.PeriodMs = pulse
			r.Param3C = param.Args[3]
		case Bleeding:
			r.PeriodMs = pulse
			r.Param38, r.Param40 = param.Args[3], param.Args[4]
		case Dark:
			r.Param48 = param.Args[3]
		case Disease:
			r.PeriodMs = pulse
			r.Param44 = param.Args[3]
		case Confusion:
			r.PeriodMs = pulse
		case Impotent, Division:
			r.Param38 = param.Args[3]
		case Decay, Weaken:
			r.Param34 = param.Args[3]
		case Panic, Combustion:
			r.Param2C, r.Param34, r.Param38 = param.Args[3], param.Args[4], param.Args[5]
		case TimeBomb:
			r.DurationMs = bombDuration
			r.Damage1C = param.Args[3]
			if p.TrapDamageGetv {
				r.Damage1C += modifier(KeyTrapDamage)
			}
		}
		out = append(out, r)
	}
	return out
}
