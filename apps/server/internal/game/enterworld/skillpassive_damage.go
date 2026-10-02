/*
===========================================================================

skillpassive_damage.go - setv / getv parameter slots

Projects native parameter keys into compact values owned by the combat stat
snapshot. Admission validates the whole passive program before enabling it.

===========================================================================
*/

package enterworld

/*
================
SkillParameter

By-value slots for the native setv/getv dictionary consumed by 40DCE0/40DFF0.
These identities are separate from skill IDs and displayed character stats.
================
*/
type SkillParameter uint8

const (
	ParameterTwoHandPower SkillParameter = iota
	ParameterOneHandPower
	ParameterDualPower
	ParameterCrossbowPower
	ParameterDaggerPower
	ParameterEarthPower
	ParameterColdPower
	ParameterFirePower
	ParameterLightningPower
	ParameterDotPower
	ParameterBloodPower
	ParameterMusicPower
	ParameterHolyPower
	ParameterDaggerHit
	ParameterDualHit
	// Abnormal-status modifiers read by 590680 through getv: poison damage
	// (RPDU, +500), poison duration (RPTU, +504) and trap damage (TRAA, +53C).
	ParameterPoisonDamage
	ParameterPoisonDuration
	ParameterTrapDamage
	// MP Decrease keys. SkillAction_Instant 5868F1/58695F and
	// CastLifecycle_ProcessPersistent 58327A/5832E6/583352 look these up in the
	// caster's modifier map and scale prepared MP cost (+0x14), not damage.
	// WIMD +0x4E4, BDMD +0x554 (0x42444D44), HLMD +0x55C (0x484C4D44).
	ParameterWizardMPDecrease
	ParameterBardMPDecrease
	ParameterHealerMPDecrease
	// Area range keys. CastLifecycle_ProcessPersistent 583657 adds these
	// caster getv values to the efr radius: MUER +0x544, DSER +0x54C.
	ParameterMusicRange
	ParameterDanceRange
	// Reach keys. The command actor adds the caster's value to the action
	// range (4ADAB8 / 4AE87E): CBRA +0x50C, WIRU +0x4E8.
	ParameterCrossbowRange
	ParameterWizardRange
	// DGAA +0x524: added to the physical attack point of a strike whose
	// command was issued in stealth (40E4B9, hit flag 8 from 58EDD7).
	ParameterStealthStrike
	// Stealth keys a hide reads through getv. STDU +0x518 adds milliseconds
	// to the effect's duration (5833C8, and the B5ED/hit-data rider); STSP
	// +0x514 adds a percentage back onto the hide's speed cut (596626).
	ParameterStealthDuration
	ParameterStealthSpeed
	// Bless keys. CastLifecycle_ProcessPersistent 58381F/583844 copies the
	// caster's HLBP (+0x56C) into a recipient's context +0x34, else its HLSM
	// (+0x570) into +0x38; 59520C adds them to defp's physical and magical.
	ParameterBlessPhysical
	ParameterBlessMagical
	// Charity's keys for the stat blessings: HLFS +0x564, HLMI +0x568.
	ParameterBlessStrength
	ParameterBlessIntellect
	// HLRU +0x558: SkillCombat_ApplySkillEffectsToTargets 59425E adds the
	// caster's value to both percent words of a heal (Faith).
	ParameterHealRecoveryUp
	// DTDR +0x52C extends a linked effect by a percentage of its base
	// duration (5833EB..583450), independent of DTAT's damage scaling.
	ParameterDotDuration
	// RPBU +0x508 adds milliseconds to the coating, not its poison victim.
	ParameterPoisonCoatingDuration
	SkillParameterCount
)

/*
================
SkillParameterMask

Records which parameter values a fully compiled program consumes.
================
*/
type SkillParameterMask uint64

/*
================
Has
================
*/
func (m SkillParameterMask) Has(p SkillParameter) bool {
	return p < SkillParameterCount && m&(SkillParameterMask(1)<<p) != 0
}

/*
================
SkillParameterValues

An actor snapshot owns these values; readers never consult mutable dictionaries.
================
*/
type SkillParameterValues [SkillParameterCount]uint32

/*
================
SkillParameterFromKey

Resolve authored four-character keys without accepting unknown parameters.
================
*/
func SkillParameterFromKey(key uint32) (SkillParameter, bool) {
	switch key {
	case 0x45325341:
		return ParameterTwoHandPower, true
	case 0x45315341:
		return ParameterOneHandPower, true
	case 0x45324141:
		return ParameterDualPower, true
	case 0x43424154:
		return ParameterCrossbowPower, true
	case 0x44474154:
		return ParameterDaggerPower, true
	case 0x45414154:
		return ParameterEarthPower, true
	case 0x434f4154:
		return ParameterColdPower, true
	case 0x46494154:
		return ParameterFirePower, true
	case 0x4c494154:
		return ParameterLightningPower, true
	case 0x44544154:
		return ParameterDotPower, true
	case 0x44544452:
		return ParameterDotDuration, true
	case 0x424c4154:
		return ParameterBloodPower, true
	case 0x4d554154:
		return ParameterMusicPower, true
	case 0x484c4154:
		return ParameterHolyPower, true
	case 0x44474852:
		return ParameterDaggerHit, true
	case 0x45324148:
		return ParameterDualHit, true
	case 0x52504455:
		return ParameterPoisonDamage, true
	case 0x52505455:
		return ParameterPoisonDuration, true
	case 0x52504255:
		return ParameterPoisonCoatingDuration, true
	case 0x54524141:
		return ParameterTrapDamage, true
	case 0x57494d44:
		return ParameterWizardMPDecrease, true
	case 0x42444d44:
		return ParameterBardMPDecrease, true
	case 0x484c4d44:
		return ParameterHealerMPDecrease, true
	case 0x4d554552:
		return ParameterMusicRange, true
	case 0x44534552:
		return ParameterDanceRange, true
	case 0x43425241:
		return ParameterCrossbowRange, true
	case 0x57495255:
		return ParameterWizardRange, true
	case 0x44474141:
		return ParameterStealthStrike, true
	case 0x53544455:
		return ParameterStealthDuration, true
	case 0x53545350:
		return ParameterStealthSpeed, true
	case 0x484c4250:
		return ParameterBlessPhysical, true
	case 0x484c534d:
		return ParameterBlessMagical, true
	case 0x484c4653:
		return ParameterBlessStrength, true
	case 0x484c4d49:
		return ParameterBlessIntellect, true
	case 0x484c5255:
		return ParameterHealRecoveryUp, true
	}
	return 0, false
}

/*
================
SkillPassiveParameters

A complete passive program, including the status resistance instructions that
may accompany its parameter values.
================
*/
type SkillPassiveParameters struct {
	Pinned bool
	Mask   SkillParameterMask
	Values SkillParameterValues
	// Reat is reat {mask, value} (+0x2FC): 595542..59568F adds value to the
	// keeper's flat status reduction 0x91+i for each mask bit i (0..5).
	Reat SkillPassiveReat
	// Real is real {status mask, flat, grade} (+0x300): 59DF20 files the flat
	// under the grade in each masked status's resistance bucket.
	Real SkillPassiveReal
	// Br is br {lane mask, value}: 594AC0 (0x595DFD..0x595EFA) adds value to
	// the flat block-rate parameter of each lane the normalized mask selects;
	// see combat.BlockRateWrites.
	Br SkillPassiveBlockRate
}

/*
================
SkillPassiveReat

Flat status reduction applied to each selected resistance lane.
================
*/
type SkillPassiveReat struct{ Mask, Value uint32 }

/*
================
SkillPassiveReal

Grade-specific resistance, kept separate from flat status reduction.
================
*/
type SkillPassiveReal struct{ Mask, Flat, Grade uint32 }

/*
================
SkillPassiveBlockRate

A passive block-rate addend. Mask is already normalized by the 587630 lane
rule, the same form the timed br buff stores.
================
*/
type SkillPassiveBlockRate struct{ Mask, Value uint32 }

// tagPassiveBlockRate is br, the token the timed compiler knows as
// tagTimedBlock; a passive row carries the same two words.
const tagPassiveBlockRate = 0x6272

// maxPassiveBlockRate is the admission ceiling for a br value. It is the
// same percent bound the timed br compiler applies, so both parsers refuse
// the same malformed rows; every shipped passive br is between 2 and 10.
const maxPassiveBlockRate = 100

/*
================
encodedPassiveParameters

Native stores up to five three-argument setv blocks. Repeated keys overwrite
in source order. reat, real and the reqi/reqn gate complete the resistance
passives; br is the block-rate passive (a duplicate br, a zero mask or a
value above maxPassiveBlockRate is malformed). Refuse the whole program if
any operation lacks execution.

Any one consumed block pins the program: none of the cited installers for
reat (595542..59568F), real (59DF20) or br (0x595DFD) reads a setv.
Protection is reat + real + reqi and Blockade br + reqi; a program holding
only its reqi gate has nothing to install and stays unpinned.
================
*/
func encodedPassiveParameters(fields []string) SkillPassiveParameters {
	var out SkillPassiveParameters
	if len(fields) <= 72 || fields[68] != "4" || fields[8] != "0" || fields[9] != "0" {
		return out
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return out
	}
	count := 0
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		switch op.Tag {
		case 0x73657476: // setv
			if count == 5 || op.Arguments[2] != 0 {
				return SkillPassiveParameters{}
			}
			// The value is an unsigned word; a negative column is malformed.
			if v, ok := textdataInt(fields[int(op.Column)+2]); !ok || v < 0 {
				return SkillPassiveParameters{}
			}
			slot, known := SkillParameterFromKey(op.Arguments[0])
			if !known {
				return SkillPassiveParameters{}
			}
			out.Mask |= SkillParameterMask(1) << slot
			out.Values[slot] = op.Arguments[1]
			count++
		case 0x72656174: // reat
			if out.Reat.Mask != 0 || op.Arguments[0] == 0 || op.Arguments[0]&^0x3f != 0 {
				return SkillPassiveParameters{}
			}
			out.Reat = SkillPassiveReat{Mask: op.Arguments[0], Value: op.Arguments[1]}
		case 0x7265616c: // real
			if out.Real.Mask != 0 || op.Arguments[0] == 0 {
				return SkillPassiveParameters{}
			}
			out.Real = SkillPassiveReal{Mask: op.Arguments[0], Flat: op.Arguments[1], Grade: op.Arguments[2]}
		case tagPassiveBlockRate:
			if out.Br.Mask != 0 || op.Arguments[0] == 0 || op.Arguments[1] > maxPassiveBlockRate {
				return SkillPassiveParameters{}
			}
			out.Br = SkillPassiveBlockRate{Mask: normalizeLaneMask(op.Arguments[0]), Value: op.Arguments[1]}
		case 0x72657169, 0x7265716e: // reqi/reqn: row.Reqi
		default:
			return SkillPassiveParameters{}
		}
	}
	out.Pinned = count > 0 || out.Reat.Mask != 0 || out.Real.Mask != 0 || out.Br.Mask != 0
	return out
}

/*
================
encodedAttackParameters

Metadata extraction is independent of execution admission. Offense validates
every getv key separately; unknown keys cannot silently become neutral buffs.
================
*/
func encodedAttackParameters(fields []string) SkillParameterMask {
	var mask SkillParameterMask
	for i := skilldataColEncodedTail; i < len(fields); {
		tag, ok := textdataInt(fields[i])
		if !ok || tag == 0x73736f75 {
			return mask
		}
		arity := spawnParamArity(uint32(tag))
		if i+arity >= len(fields) {
			return mask
		}
		if tag == 0x67657476 {
			key, valid := textdataInt(fields[i+1])
			if valid && key >= 0 && key <= 0xffffffff {
				if slot, known := SkillParameterFromKey(uint32(key)); known {
					mask |= 1 << slot
				}
			}
		}
		i += 1 + arity
	}
	return mask
}

/*
================
encodedTailHasParameter

Walk instruction boundaries; argument values that equal a tag are not programs.
================
*/
func encodedTailHasParameter(fields []string, wantedTag, wantedKey uint32) bool {
	for i := skilldataColEncodedTail; i < len(fields); {
		tag, ok := textdataInt(fields[i])
		if !ok || tag == 0x73736f75 {
			return false
		}
		arity := spawnParamArity(uint32(tag))
		if i+arity >= len(fields) {
			return false
		}
		if uint32(tag) == wantedTag && arity > 0 {
			key, valid := textdataInt(fields[i+1])
			if valid && key == int64(wantedKey) {
				return true
			}
		}
		i += arity + 1
	}
	return false
}
