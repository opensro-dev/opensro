/*
===========================================================================

skillconcealment.go - hiding and detection programs

Four shipped families, every one ActionHandler 3 and released by
CastLifecycle_ProcessPersistent (5830B0):

	hide skc [getv reqi]           the caster hides (Rogue stealth,
	                               Wizard invisibility A)
	hide skc efr1 [getv]           the caster and nearby party hide
	                               (Wizard invisibility B)
	dttp efr1 [nbuf bbuf getv reqi] a reveal placed on the characters
	                               around the caster
	dtt efr2 [reqi cbuf]           the caster's own sight

The RefSkill words: hide +0x428 {mode, level, speed%}, dtt +0x420 and
dttp +0x424 {mask, level}, skc +0x490 {damage mask, event mask, keep%}.
Mode 1 is stealth (body status 6), mode 2 invisibility (7); the hide
masks are 1 stealth, 2 invisibility, 4 trap.

===========================================================================
*/

package enterworld

// Tags this file reads.
const (
	tagHide = 0x68696465
	tagDtt  = 0x647474
	tagDttp = 0x64747470
	tagSkc  = 0x736b63
	tagEfr  = 0x656672
	tagGetv = 0x67657476
	tagDura = 0x64757261
	tagReqi = 0x72657169
	tagNbuf = 0x6e627566
	tagBbuf = 0x62627566
	tagCbuf = 0x63627566
)

/*
==================
SkillDamageCancel

skc words 0 and 2. CSkillManager_ProcessDamageEffects (5A1612) retires an
effect when the landing attack's att flags (+0x230 word 0) share a bit
with Mask. Word 1, the event mask, is SkillReplacement.EventCancelMask.

Chance is word 2, the percent chance that such a hit ends the effect; 0
means every hit does. Owner's rule: the Bard's skc(15,0,80) ends the aura
on 80 % of the hits its Bard receives. The shipped hides and duplicates
author 0 and keep ending on every masked hit, as before.
==================
*/
type SkillDamageCancel struct {
	Present bool
	Mask    uint32
	Chance  uint32
}

func encodedDamageCancel(fields []string) SkillDamageCancel {
	values, ok := encodedLastParameters(fields, tagSkc)
	if !ok || len(values) < 3 || values[0] == 0 {
		return SkillDamageCancel{}
	}
	return SkillDamageCancel{Present: true, Mask: values[0], Chance: min(values[2], 100)}
}

/*
==================
SkillConcealment

The executable part of a hiding or detection program. Pinned rows are
admitted by action.acceptTimedSelfEffect; everything else stays closed.
==================
*/
type SkillConcealment struct {
	Pinned bool
	// Persistent is cbuf: the item-cast detection timed job.
	Persistent bool

	// Hide is the +0x428 block.
	Hide         bool
	HideMode     uint8 // 1 stealth, 2 invisibility
	HideLevel    uint32
	SpeedPercent uint32 // taken off walk and run (0x17/0x18)
	// DurationBonus and SpeedBonus are getv STDU (+0x518) and STSP (+0x514).
	DurationBonus bool
	SpeedBonus    bool

	// Sight is dtt (+0x420): what the caster itself sees through.
	// Reveal is dttp (+0x424): laid on the characters around the caster.
	Sight, Reveal SkillStatusLevel

	// Area is the efr kind 1 selection the recipients come from; without
	// it the caster alone receives the effect.
	Area SkillRecipientArea
	// Range is the client's detection distance: an efr whose shape is 1
	// lends its radius (85CC70), 0 means everywhere.
	Range uint32
}

// SkillRecipientArea is efr kind 1 {kind, shape, radius, max, reduction,
// select}; see SkillOffensiveArea.Select for the select bits.
type SkillRecipientArea struct {
	Present    bool
	Radius     uint32
	MaxTargets uint32
	Select     uint32
}

// The select bits TargetSelection_AroundSource (58A020) reads.
const (
	SelectCaster    uint32 = 0x01
	SelectCharacter uint32 = 0x02
	SelectParty     uint32 = 0x04
	SelectHostile   uint32 = 0x08
)

/*
==================
parseSkillConcealment

Every instruction must be one this file executes. Durations are required;
shapes other than 1 (around the caster) are refused.
==================
*/
func parseSkillConcealment(fields []string, row *SkillRow) {
	row.DamageCancel = encodedDamageCancel(fields)
	if !row.ReplacementPinned || row.ChainNext != 0 || !row.TimingPinned ||
		!row.ActionCastingTimePinned || !row.Consumption.Pinned {
		return
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return
	}

	var c SkillConcealment
	var efr [6]uint32
	haveEfr, duration := false, false
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		switch op.Tag {
		case tagDura:
			if duration || op.Arguments[0] == 0 {
				return
			}
			duration = true
		case tagHide:
			mode, level, speed := op.Arguments[0], op.Arguments[1], op.Arguments[2]
			if c.Hide || mode != 1 && mode != 2 || level == 0 || speed >= 100 {
				return
			}
			c.Hide, c.HideMode, c.HideLevel, c.SpeedPercent = true, uint8(mode), level, speed
		case tagDtt:
			if c.Sight.Present || op.Arguments[0] == 0 || op.Arguments[1] == 0 {
				return
			}
			c.Sight = SkillStatusLevel{Present: true, Mask: op.Arguments[0], Level: op.Arguments[1]}
		case tagDttp:
			if c.Reveal.Present || op.Arguments[0] == 0 || op.Arguments[1] == 0 {
				return
			}
			c.Reveal = SkillStatusLevel{Present: true, Mask: op.Arguments[0], Level: op.Arguments[1]}
		case tagEfr:
			if haveEfr {
				return
			}
			haveEfr = true
			copy(efr[:], op.Arguments[:6])
		case tagGetv:
			switch op.Arguments[0] {
			case 0x53544455: // STDU
				c.DurationBonus = true
			case 0x53545350: // STSP
				c.SpeedBonus = true
			case 0x57494d44: // WIMD, the MP Decrease the cost already applies
			default:
				return
			}
		case tagSkc, tagReqi, tagNbuf, tagBbuf:
			// skc is projected by the replacement and DamageCancel; reqi by
			// the equipment gate; nbuf/bbuf by the buff presentation.
		case tagCbuf:
			c.Persistent = true
		default:
			return
		}
	}
	// Exactly one family: a hide, a sight or a reveal.
	families := 0
	for _, present := range []bool{c.Hide, c.Sight.Present, c.Reveal.Present} {
		if present {
			families++
		}
	}
	if !duration || families != 1 {
		return
	}

	if haveEfr {
		kind, shape, radius, most, reduction, sel := efr[0], efr[1], efr[2], efr[3], efr[4], efr[5]
		if shape != 1 || radius == 0 || reduction != 0 {
			return
		}
		c.Range = radius
		switch {
		case kind == 1 && (c.Hide || c.Reveal.Present):
			c.Area = SkillRecipientArea{Present: true, Radius: radius, MaxTargets: most, Select: sel}
		case kind == 2 && c.Sight.Present:
			// 5840FB: a dtt row opens no area context; efr only lends
			// the client its range.
		default:
			return
		}
	}
	// A reveal reaches others, a hide the caster (and its party); a sight
	// is the caster's alone.
	switch {
	case c.Reveal.Present && !c.Area.Present:
		return
	case c.Hide && c.Area.Present && c.Area.Select&SelectCaster == 0:
		return
	case c.Sight.Present && c.Area.Present:
		return
	}
	if c.Hide && !row.BodyStatus.Present {
		return
	}
	c.Pinned = true
	row.Concealment = c
}

// encodedDetectRange is what 85CC70 reads from +0x64: the radius of the
// last efr block when its shape is 1, else 0.
func encodedDetectRange(fields []string) uint32 {
	values, ok := encodedLastParameters(fields, tagEfr)
	if !ok || len(values) < 3 || values[1] != 1 {
		return 0
	}
	return values[2]
}
