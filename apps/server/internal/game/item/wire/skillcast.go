/*
===========================================================================

skillcast.go - native cast brackets and committed impact packets

Keep hit magnitude separate from HP bookkeeping. This wire owner encodes the
full committed impact, its fatal flag and optional displacement/absorption.

===========================================================================
*/

package wire

import "math"

// The S->C skill-cast bracket: server accept, then 0xB245/0xB505.
//
// Ordinary cast brackets own one 0xB505 mode-2 finalize with the
// same token. Exception: tele guided travel closes on arrival (8DD611/8DD61C),
// with secondary motion authored by its effect metadata. The client has no
// arbitrary wall-clock finalize - sub_8dcf40
// callers are event-driven - so without a steering payload the ONLY thing that
// clears busy bit 2 (and reopens the 0x2476 move gate) is a received 0xB505
// mode 2. B245 opens the action bracket immediately. B505 mode 1 releases
// WAIT at Action_CastingTime without movement steering; mode 2 closes the
// bracket at Action_CastingTime + Action_ActionDuration. This clock is independent
// of the client BAN clip and its presentation blend/completion clock.

// Opcodes of the skill-cast bracket.
const (
	// OpSkillCastResult is the Sâ†’C 0xB245 cast bracket open: success enters
	// motion state 2 through sub_776830 â†’ sub_8e06e0 Init. The fail form
	// ([ok != 1][errCode]) is a pure notice - not composed here; landing 1
	// keeps every non-accepted shape on its pre-existing refuse path.
	OpSkillCastResult uint16 = 0xB245
	// OpSkillEffectControl is the Sâ†’C 0xB505 effect-control packet
	// (sub_8e2c90 via thunk sub_7754f0): mode 1 re-arms steering, mode 2
	// finalizes (sub_8dcf40 â†’ busy bit 2 clears). The client never sends
	// it - a deferred close must be a server emit.
	OpSkillEffectControl uint16 = 0xB505
)

// SkillCastSuccess is the common 0xB245 success prefix pinned in the
// sub_776830 fold research. Live combat appends its target/result block via
// SkillCastSingleTargetResult:
//
//	[ok=0x01][btResult u8][skillId u32][casterGid u32][instanceToken u32]
//	[ownerOrTargetGid u32]
//
// Length must be EXACT - leftover bytes trip the client's 0xed assert.
/*
================
SkillCastSuccess
================
*/
type SkillCastSuccess struct {
	// BtResult must be 0 or 2 - the client accepts exactly {0,2} and both
	// jump to the same label 0x776940, so they are indistinguishable;
	// anything else trips the 0xe7 assert and retail silently drops the
	// cast. Landing 1 pins 0.
	BtResult uint8
	// SkillId keys the client's effect-record lookup (g_effectRecordMap /
	// cec870+0x16c) and the cool-time insert.
	SkillId uint32
	// CasterGid MUST be the gid the enter-world plane assigned
	// (enterworld.ObjectIDForCharacter): the local-cast tail gates on
	// casterGid == data_cedb50 and a mismatch silently skips the
	// cool-time insert.
	CasterGid uint32
	// InstanceToken is server-minted and unique per cast; it keys the
	// client's f08a48 instance map and the finalize must echo it.
	InstanceToken uint32
	// OwnerOrTargetGid is the 4 bytes the plan mislabeled "dur f32": a u32
	// GID, stored to SkillEffectObj+0xcc by an integer mov (sub_8e0440
	// @0x8e0493/97, no fld/fstp) and consumed by
	// GidObjectRegistry_FindCharacterByGid (sub_8d9740), read on the
	// local-echo path REGARDLESS of steeringFlags. Single-target result
	// encoding derives this from TargetGid so the animation/effect owner and
	// the damage-row owner cannot diverge. Other cast shapes may still send 0.
	OwnerOrTargetGid uint32
}

/*
================
writePrefix
================
*/
func (s SkillCastSuccess) writePrefix(writer *Writer) *Writer {
	return writer.
		U8(0x01).
		U8(s.BtResult).
		U32(s.SkillId).
		U32(s.CasterGid).
		U32(s.InstanceToken).
		U32(s.OwnerOrTargetGid)
}

// No-target native action: no invented damage row or movement steering.
/*
================
SkillCastUntargetedFrame
================
*/
func SkillCastUntargetedFrame(cast SkillCastSuccess) Frame {
	return Frame{Opcode: OpSkillCastResult, Payload: cast.writePrefix(NewWriter(19)).U8(0).Payload()}
}

// Native tele has a position payload without a fabricated damage target.
/*
================
SkillCastTravelFrame
================
*/
func SkillCastTravelFrame(cast SkillCastSuccess, destination SkillCastFacingPoint) Frame {
	return Frame{Opcode: OpSkillCastResult, Payload: destination.writeTo(cast.writePrefix(NewWriter(27)).U8(8)).Payload()}
}

const (
	skillCastSteeringTargets = uint8(1 << 0)
	skillCastSteeringPoint   = uint8(1 << 3)
)

// SkillCastFacingPoint is the target-position publication owned by one
// accepted single-target action. The fields are intentionally private: action
// code may supply semantic world coordinates, but it may not construct wire
// flags or partially initialized payload bytes.
//
// The v1.150 client reads [region u16][x i16][y i16][z i16] when B245 steering
// bit 3 is set. It starts the effect's target-point leg and synchronously faces
// the holder toward that point. Coordinates use the client's cvttsd2si
// semantics (truncate toward zero) before the signed-16-bit wire admission.
/*
================
SkillCastFacingPoint
================
*/
type SkillCastFacingPoint struct {
	regionID uint16
	x        int16
	y        int16
	z        int16
	valid    bool
}

// NewSkillCastFacingPoint admits one authoritative live target pose to the
// B245 steering domain. Invalid/non-finite or unrepresentable coordinates fail
// closed so an action producer cannot silently wrap the facing destination.
/*
================
NewSkillCastFacingPoint
================
*/
func NewSkillCastFacingPoint(regionID uint16, x, y, z float64) (SkillCastFacingPoint, bool) {
	quantize := func(value float64) (int16, bool) {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, false
		}
		truncated := math.Trunc(value)
		if truncated < math.MinInt16 || truncated > math.MaxInt16 {
			return 0, false
		}
		return int16(truncated), true
	}

	qx, okX := quantize(x)
	qy, okY := quantize(y)
	qz, okZ := quantize(z)
	if !okX || !okY || !okZ {
		return SkillCastFacingPoint{}, false
	}
	return SkillCastFacingPoint{
		regionID: regionID,
		x:        qx,
		y:        qy,
		z:        qz,
		valid:    true,
	}, true
}

/*
================
writeTo
================
*/
func (p SkillCastFacingPoint) writeTo(writer *Writer) *Writer {
	if !p.valid {
		panic("wire: single-target skill result has no admitted facing point")
	}
	return writer.
		U16(p.regionID).
		U16(uint16(p.x)).
		U16(uint16(p.y)).
		U16(uint16(p.z))
}

// MaxSkillActionDamage is the 24-bit damage ceiling of a type-0 action-result
// row. The client obtains damage from packedResult>>8, and the v1.188
// Formulae.cpp helpers clamp their result to the same 16,777,215 ceiling.
const MaxSkillActionDamage uint32 = 0x00FFFFFF

// SkillCastTargetImpact is one authoritative stage row in a single-target
// action. Multi-impact actions carry several of these under the SAME cast
// token; they are not several casts and therefore do not own independent
// cooldown/finalize lifecycles.
/*
================
SkillCastTargetImpact
================
*/
type SkillCastTargetImpact struct {
	ResultFlags     uint8
	Damage          uint32
	Fatal           bool
	SecondaryAmount uint32
	// Knockdown is admitted before the authority commit; fatal impacts suppress it.
	Knockdown SkillCastFacingPoint
	Knockback SkillCastFacingPoint
	// Absorb makes this a Force wall's type-7 record; Skipped a bare type 8;
	// Blocked a bare type 2 (5855F0 writes only the kind byte).
	Absorb  *SkillCastAbsorb
	Skipped bool
	Blocked bool
}

// SkillCastAbsorb is the rest of a type-7 record (5855F0): the pool left
// once this impact's absorbed damage is counted, and the authored pool.
// Broken (tag bit 7) marks the impact that emptied it.
/*
================
SkillCastAbsorb
================
*/
type SkillCastAbsorb struct {
	Remaining, Max uint16
	Broken         bool
}

// SkillCastSingleTargetResult is the authoritative B245 combat-result shape:
// one target and one or more type-0 impact rows.
//
//	SkillCastSuccess prefix with steeringFlags=9 (targets + facing point)
//	[impactCount=N][targetCount=1]
//	[targetGid u32]
//	repeated N times: [tag u8][packedResult u32][secondaryAmount u32]
//	[facingRegion u16][facingX i16][facingY i16][facingZ i16]
//
// The native reader stores one outer row per authored impact phase and one
// inner result per target. That orientation matters: animation callbacks
// consume outer rows one at a time. Transposing it to one outer target row
// with N inner impacts makes every impact fire together.
//
// tag bit 7 is the stop/death transition and its low seven bits are the result
// kind (zero here). packedResult keeps result flags in its low byte and each
// full hit damage in its high 24 bits (native 585664). Damage must come from
// a committed hit; its magnitude may exceed the victim's remaining HP. The
// HP owner clamps its debit separately from this feedback value.
/*
================
SkillCastSingleTargetResult
================
*/
type SkillCastSingleTargetResult struct {
	stationary bool
	cast       SkillCastSuccess
	targetGid  uint32
	impacts    []SkillCastTargetImpact
	// facingPoint is present only in the movement-steered variant. Native
	// bit 3 installs a movement controller; stationary casts omit it.
	facingPoint SkillCastFacingPoint
	// absorb is the defender's wall group: a second target entry with the
	// same gid (58EC9F appends it to the result's target list).
	absorb []SkillCastTargetImpact
}

// WithAbsorb returns the result carrying the defender's wall group, one
// record per impact.
/*
================
WithAbsorb
================
*/
func (r SkillCastSingleTargetResult) WithAbsorb(impacts []SkillCastTargetImpact) SkillCastSingleTargetResult {
	if len(impacts) != len(r.impacts) {
		panic("wire: wall records must match the impact count")
	}
	r.absorb = append([]SkillCastTargetImpact(nil), impacts...)
	return r
}

// NewStationarySkillCastSingleTargetResult retains target/result ownership without bit 3. In 8e0440 that
// bit starts a 500-unit/s movement controller; it is not a facing-only hint.
/*
================
NewStationarySkillCastSingleTargetResult
================
*/
func NewStationarySkillCastSingleTargetResult(cast SkillCastSuccess, target uint32, impacts []SkillCastTargetImpact) SkillCastSingleTargetResult {
	return SkillCastSingleTargetResult{cast: cast, targetGid: target, impacts: append([]SkillCastTargetImpact(nil), impacts...), stationary: true}
}

// SkillCastReleaseFrame is 8e2c90 mode 1 followed by 8e0440's no-steering
// target/flags record. It releases WAIT through 8df180 without moving the actor.
/*
================
SkillCastReleaseFrame
================
*/
func SkillCastReleaseFrame(token, target uint32) Frame {
	return Frame{Opcode: OpSkillEffectControl, Payload: NewWriter(10).U8(1).U32(token).U32(target).U8(0).Payload()}
}

// The release packet uses the SAME target/impact grammar as B245, after
// [mode=1][token]. Keep one encoder so delayed hits cannot drift from starts.
/*
================
SkillCastReleaseResultFrame
================
*/
func SkillCastReleaseResultFrame(result SkillCastSingleTargetResult) Frame {
	payload := result.Encode()
	writer := NewWriter(len(payload) - 9).U8(1).U32(result.cast.InstanceToken)
	return Frame{Opcode: OpSkillEffectControl, Payload: append(writer.Payload(), payload[14:]...)}
}

// NewSkillCastSingleTargetResult constructs the movement-steered variant.
// Its point starts native actor movement as well as setting the facing.
// Ordinary stationary attacks use NewStationarySkillCastSingleTargetResult.
/*
================
NewSkillCastSingleTargetResult
================
*/
func NewSkillCastSingleTargetResult(
	cast SkillCastSuccess,
	targetGid uint32,
	impacts []SkillCastTargetImpact,
	facingPoint SkillCastFacingPoint,
) SkillCastSingleTargetResult {
	return SkillCastSingleTargetResult{
		cast:        cast,
		targetGid:   targetGid,
		impacts:     append([]SkillCastTargetImpact(nil), impacts...),
		facingPoint: facingPoint,
	}
}

// Encode returns the exact single-target B245 result payload. Internal
// producers must provide 1..255 impacts, matching the wire's u8 impact count.
/*
================
Encode
================
*/
func (r SkillCastSingleTargetResult) Encode() []byte {
	if len(r.impacts) == 0 || len(r.impacts) > 0xff {
		panic("wire: skill action impact count is outside 1..255")
	}

	cast := r.cast
	cast.OwnerOrTargetGid = r.targetGid
	flags := skillCastSteeringTargets
	if !r.stationary {
		flags |= skillCastSteeringPoint
	}
	targets := uint8(1)
	if r.absorb != nil {
		targets = 2
	}
	writer := cast.writePrefix(NewWriter(37 + 18*len(r.impacts))).
		U8(flags).
		U8(uint8(len(r.impacts))).
		U8(targets).
		U32(r.targetGid)
	for _, impact := range r.impacts {
		impact.writeTo(writer)
	}
	if r.absorb != nil {
		writer.U32(r.targetGid)
		for _, impact := range r.absorb {
			impact.writeTo(writer)
		}
	}
	if !r.stationary {
		return r.facingPoint.writeTo(writer).Payload()
	}
	return writer.Payload()
}

// SkillCastFinalize is the 0xB505 mode-2 finalize, shape pinned in the
// sub_8e2c90 fold research:
//
//	[mode=0x02][extra u8 (read, discarded)][instanceToken u32]
//
// A token miss is a SILENT consume-and-return (sub_8e2bc0) - the bracket
// never closes - so the token must echo the success frame's exactly.
/*
================
SkillCastFinalize
================
*/
type SkillCastFinalize struct {
	InstanceToken uint32
}

// Encode returns the 0xB505 mode-2 payload (6 bytes).
/*
================
Encode
================
*/
func (f SkillCastFinalize) Encode() []byte {
	return NewWriter(6).
		U8(0x02).
		U8(0x00).
		U32(f.InstanceToken).
		Payload()
}

// SkillCastSingleTargetResultFrame opens one accepted cast bracket and
// carries the committed single-target HP result in the same B245 payload.
/*
================
SkillCastSingleTargetResultFrame
================
*/
func SkillCastSingleTargetResultFrame(result SkillCastSingleTargetResult) Frame {
	return Frame{Opcode: OpSkillCastResult, Payload: result.Encode()}
}

// SkillCastFinalizeFrame closes the bracket identified by instanceToken.
/*
================
SkillCastFinalizeFrame
================
*/
func SkillCastFinalizeFrame(instanceToken uint32) Frame {
	return Frame{Opcode: OpSkillEffectControl, Payload: SkillCastFinalize{
		InstanceToken: instanceToken,
	}.Encode()}
}

// Types 4 and 5 append the displaced pose to the packed damage payload
// (v1.150 8e177b and 8e17f6). Both area and single-target casts use this encoder.
/*
================
writeTo
================
*/
func (impact SkillCastTargetImpact) writeTo(writer *Writer) {
	if impact.Skipped {
		writer.U8(8)
		return
	}
	if impact.Blocked {
		writer.U8(2)
		return
	}
	if a := impact.Absorb; a != nil {
		tag := uint8(7)
		if a.Broken {
			tag |= 0x80
		}
		writer.U8(tag).U32(min(impact.Damage, MaxSkillActionDamage)<<8 | uint32(impact.ResultFlags)).U16(a.Remaining).U16(a.Max)
		return
	}
	tag := uint8(0)
	if impact.Fatal {
		tag = 0x80
	} else if impact.Knockdown.valid {
		tag = 4
	} else if impact.Knockback.valid {
		tag = 5
	}
	writer.U8(tag).U32(min(impact.Damage, MaxSkillActionDamage)<<8 | uint32(impact.ResultFlags)).U32(impact.SecondaryAmount)
	if tag == 4 {
		impact.Knockdown.writeTo(writer)
	} else if tag == 5 {
		impact.Knockback.writeTo(writer)
	}
}
