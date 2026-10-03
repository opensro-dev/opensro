/*
===========================================================================

skilltimedeffect.go - complete timed buff programs and their execution routes

Defense, stat blessings and party movement share the native persistent-skill
handler. Admission consumes the whole program before enabling any modifier.
Ordinary casts and item-owned timed jobs share effect descriptors and lifecycle.

===========================================================================
*/

package enterworld

const (
	tagTimedHaste       = 0x68737465
	tagTimedOverride    = 0x68737432
	tagTimedIndependent = 0x68737433
	tagTimedDefense     = 0x64656670
	tagTimedBlock       = 0x6272

	// maxBlockRatePercent is the admission ceiling for a br value, timed or
	// passive: a percent of the whole block chance. Every shipped br is
	// within it (passives 2..10).
	maxBlockRatePercent        = 100
	tagTimedStrength           = 0x73747269
	tagTimedIntellect          = 0x696e7469
	tagTimedLink               = 0x6c6e6b73
	tagTimedLinkedThreat       = 0x6c6b6167
	tagTimedLinkPerTarget      = 0x6c6b7332
	tagTimedLinkedDamage       = 0x6c6b6468
	tagTimedRequireNot         = 0x7265716e
	tagTimedMaxHP              = 0x687069
	tagTimedAttack             = 0x61706175
	tagTimedDamagePenalty      = 0x706d6467
	tagTimedThreat             = 0x746e7432
	tagTimedDamageRate         = 0x647275
	tagTimedMaxHPPenalty       = 0x706d6870
	tagTimedIncomingReduction  = 0x6f646172
	tagTimedOverlap            = 0x6f766c32
	tagTimedPreemptive         = 0x706f6c61
	parameterWizardMP          = 0x57494d44
	parameterBardMP            = 0x42444d44
	parameterMusicArea         = 0x4d554552
	parameterBlessingPhysical  = 0x484c4250
	parameterBlessingMagical   = 0x484c534d
	parameterBlessingStrength  = 0x484c4653
	parameterBlessingIntellect = 0x484c4d49
)

/*
================
SkillTimedEffect

An entire category-three program. Native 5830B0 owns preparation and release;
594AC0 installs recipient modifiers. Area instances have independent lifetimes.
================
*/
type SkillTimedEffect struct {
	ForcedTarget bool // hitm: recipient may target only its caster (58CF7F).
	// Periodic has a separate execution contract from friendly timed buffs.
	Periodic SkillPeriodicEffect
	// ItemProgram marks an item-owned timed job (compileTimedItemEffect).
	ItemProgram               bool
	HP, MP, Evasion, Accuracy SkillFlatRate
	Recovery                  SkillRecoveryRates
	GoldDropPercent           uint32
	Pinned                    bool
	Persistent                bool
	// IncomingReduction marks an admitted odar block (Earth Barrier).
	IncomingReduction             bool
	Physical, Magical, CapPercent uint32
	// Targeted rows (Warrior guards, Cleric blessings) install on a player
	// within column 21's range instead of the caster.
	Targeted bool
	// Area is an efr kind 1 selection. Each selected actor gets an instance;
	// select 4/5 uses the party selector, other masks use around-source.
	Area SkillRecipientArea
	// PhysicalAddend and MagicalAddend are getv HLBP / HLSM: the caster's
	// value joins the recipient's defp (58381F; HLBP wins when both appear).
	PhysicalAddend, MagicalAddend bool
	// Strength and Intellect are stri (+0x3F0) and inti (+0x3F4): 594F69
	// adds {value + context addend} to parameter 1 / 2, capped at cap
	// percent of the recipient's current value when the cap is nonzero.
	Strength, Intellect SkillStatBoost
	// StrengthAddend and IntellectAddend are getv HLFS / HLMI: the caster's
	// value rides context +2C / +30 (583A65..583AB0).
	StrengthAddend, IntellectAddend bool
	// Link is lnks (+0x370): the cast installs a source half on the caster
	// and the recipient half on the target (5830B0, 59DC80).
	Link SkillEffectLink
	// Defense marks a defp block; Block is br (+0x278) {lane mask, value}:
	// the flat block-rate bonus 594AC0 installs (0x595DFD).
	Defense    bool
	Block      SkillBlockBoost
	Attributes SkillAttributeBoost
	// Preemptive is pola (Noise): while the effect lasts, the monsters it
	// names do not acquire its owner first.
	Preemptive SkillPreemptiveGuard
}

/*
================
SkillPreemptiveGuard

pola {grade mask, level}, the "Preemptive attack prevention (lv word 1)"
row of the client tooltip (sub_7f9bd0). Inferred from the protection
branch of CGObjMob_EvaluateHostility (5299E0), whose target carries a
mask and a level: a regular monster (type word 0x8C6) whose grade bit
(normal 1, champion 2, unique 4, elite 8) is in Mask and whose level does
not exceed Level does not choose the protected player. Every v1.150 row
authors mask 1 (normal grade only) and a level that grows with the tier
(30 .. 100); the client prints that word as a level, not a percentage.
================
*/
type SkillPreemptiveGuard struct {
	Present     bool
	Mask, Level uint32
}

/*
================
SkillAttributeBoost

594AC0 installs these keeper contributions for the descriptor's lifetime.
pmdg mode two is an outgoing-damage multiplier, not an abnormal status.
================
*/
type SkillAttributeBoost struct {
	MaxHP, Attack, DamagePenalty    bool
	HPFlat, HPPercent               uint32
	PhysicalAttack, MagicalAttack   uint32
	PhysicalPenalty, MagicalPenalty uint32
	// DamageRate is dru: 594AC0 installs it from BuffModifiers for every
	// effect, so the program only has to be admitted. MaxHPPenalty is pmhp.
	DamageRate, MaxHPPenalty bool
	HPPenaltyPercent         uint32
}

/*
================
SkillBlockBoost

One br block with its physical/magical lane mask normalized.
================
*/
type SkillBlockBoost struct {
	Present     bool
	Mask, Value uint32
}

/*
================
SkillRecoveryRates

irgc contains separate HP and MP percentage additions. The recovery tick
supplies the standing or sitting base through the same parameter keeper.
================
*/
type SkillRecoveryRates struct {
	Present bool
	HP, MP  uint32
}

/*
================
SkillStatBoost

One stri or inti block, including the recipient-relative cap.
================
*/
type SkillStatBoost struct {
	Present           bool
	Value, CapPercent uint32
}

/*
================
SkillEffectLink

lnks {group, max distance, max outgoing, board}. A zero board word hides the
source half from the caster's board; both halves still exist on the server.
Threat is lkag {percent, 0}: the share of the recipient's aggression that
5A03A0 (combat.SplitLinkedThreat) hands to the link source.

Mana is lkdh {HP percent, MP percent, cap} (the Bard's Mana Switch): the
MP share of each hit the recipient deals, at most cap per hit, goes to
the source. Inferred from the three words (0, 50, 305..1596 on every
tier) and the description ("When the target damages an enemy, some part
of the damage will be converted to mana and your MP will be recovered"):
word 1 is the MP percent, word 2 the per-hit ceiling, which grows with
the tier, and word 0, by symmetry with dmgt, an HP share no row authors.
PerTarget is lks2, which makes lnks' outgoing count per recipient
(skillperiodic.go).
================
*/
type SkillEffectLink struct {
	Present                             bool
	Group, MaxDistance, MaxOutgoing     uint32
	Board                               uint32
	Threat                              bool
	ThreatPercent                       uint32
	PerTarget                           bool
	Mana                                bool
	ManaHPPercent, ManaPercent, ManaCap uint32
}

/*
================
parseSkillTimedEffect

Qualify the envelope and every instruction together. A movement descriptor is
enabled only after this complete producer succeeds, never from hste alone.
================
*/
func parseSkillTimedEffect(fields []string, row *SkillRow) {
	if forced := compileForcedTarget(fields, *row); forced.Pinned {
		row.TimedEffect = forced
		return
	}
	if item, ok := compileTimedItemEffect(fields, *row); ok {
		row.TimedEffect = item
		return
	}
	if periodic := compileSkillPeriodicEffect(fields, *row); periodic.Pinned {
		row.TimedEffect = SkillTimedEffect{Periodic: periodic}
		return
	}
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || fields[68] != "3" ||
		row.ChainNext != 0 || !row.Consumption.Pinned || !row.ActionCastingTimePinned ||
		!row.ActionDurationPinned || !row.TimingPinned || !row.ReplacementPinned ||
		row.Consumption.HP != 0 || row.Consumption.HPPercent != 0 {
		return
	}
	// Other selection, projectile, repeat and periodic producers have their
	// own contracts. None may be erased to manufacture a self-only program.
	// The one targeted shape is Required+Animal+Ally+Party with a range.
	// Self (column 26) may join it.
	targeted := fields[21] != "0" && fields[22] == "1" && fields[23] == "1" && fields[27] == "1" && fields[28] == "1"
	var result SkillTimedEffect
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return
	}
	// Mana Switch also names Enemy_M and Enemy_P (columns 29, 30). Inferred:
	// the link binds two players (acceptTimedTargetEffect refuses anything
	// else with 0x3006), so those bytes only widen 58D7A0's player check;
	// they are tolerated on a targeted lkdh row alone.
	damageLink := false
	for i := 0; i < program.Len(); i++ {
		damageLink = damageLink || program.Instruction(i).Tag == tagTimedLinkedDamage
	}
	for i := 0; i < program.Len(); i++ {
		if op := program.Instruction(i); op.Tag == tagEfr {
			kind, shape, radius, most, reduction, sel := op.Arguments[0], op.Arguments[1], op.Arguments[2], op.Arguments[3], op.Arguments[4], op.Arguments[5]
			if result.Area.Present || targeted || kind != 1 || shape != 1 || radius == 0 || reduction != 0 {
				return
			}
			result.Area = SkillRecipientArea{Present: true, Radius: radius, MaxTargets: most, Select: sel}
		}
	}
	for _, col := range []int{15, 16, 17, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 56} {
		if targeted && (col == 21 || col == 22 || col == 23 || col == 26 || col == 27 || col == 28) {
			continue
		}
		if targeted && damageLink && (col == 29 || col == 30) {
			continue
		}
		if result.Area.Present && col == 21 {
			continue // an area buff's range word is not a target reach
		}
		if fields[col] != "0" {
			return
		}
	}
	for _, col := range []int{50, 51} {
		if _, ok := textdataByte(fields[col]); !ok {
			return
		}
	}
	duration, defense, movement, musicParameters := false, false, false, false
	attributeTags := make(map[uint32]bool)
	boost := func(b *SkillStatBoost, op SkillInstruction) bool {
		if b.Present || op.Count != 2 {
			return false
		}
		*b = SkillStatBoost{Present: true, Value: op.Arguments[0], CapPercent: op.Arguments[1]}
		return true
	}
	// lkag and lkdh may be authored anywhere in the program; the native index
	// keeps them wherever they sit, and they ride the row's lnks, which is
	// checked once every block is read.
	var linkThreat, linkDamage bool
	var linkThreatPercent uint32
	var linkDamageWords [3]uint32
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		switch op.Tag {
		case tagTimedMaxHP, tagTimedAttack, tagTimedDamagePenalty, tagTimedThreat, tagTimedDamageRate, tagTimedMaxHPPenalty:
			if attributeTags[op.Tag] || targeted || result.Area.Present {
				return
			}
			attributeTags[op.Tag] = true
			a := &result.Attributes
			switch op.Tag {
			case tagTimedMaxHP:
				a.MaxHP, a.HPFlat, a.HPPercent = true, op.Arguments[0], op.Arguments[1]
			case tagTimedAttack:
				a.Attack, a.PhysicalAttack, a.MagicalAttack = true, op.Arguments[0], op.Arguments[1]
			case tagTimedDamagePenalty:
				if op.Arguments[3] != 2 || op.Arguments[0] != row.EffectDurationMs ||
					op.Arguments[1] > 100 || op.Arguments[2] > 100 {
					return
				}
				a.DamagePenalty, a.PhysicalPenalty, a.MagicalPenalty = true, op.Arguments[1], op.Arguments[2]
			case tagTimedDamageRate:
				if op.Count != 2 || !row.BuffModifiers.Dru || row.BuffModifiers.DruWords != [2]uint32{op.Arguments[0], op.Arguments[1]} {
					return
				}
				a.DamageRate = true
			case tagTimedMaxHPPenalty:
				// pmhp has pmdg's four-word shape: duration, two words, mode 2.
				// The client (8DCF61) reads the third word as the percentage;
				// 100 marks a deferred cancellation this owner does not port.
				// The second word is zero in every v1.150 row and stays refused
				// otherwise, since its lane is unproven.
				if op.Count != 4 || op.Arguments[0] != row.EffectDurationMs || op.Arguments[1] != 0 ||
					op.Arguments[2] == 0 || op.Arguments[2] >= 100 || op.Arguments[3] != 2 {
					return
				}
				a.MaxHPPenalty, a.HPPenaltyPercent = true, op.Arguments[2]
			case tagTimedThreat:
				// 5903EC consumes tnt2 only when producing a target hit.
				// A self buff has no hostile target result; 594AC0 does not
				// install tnt2 as tant's independent keeper contribution.
				if op.Arguments[0] != 0 {
					return
				}
			}
		case tagDura:
			if duration || op.Count != 1 {
				return
			}
			duration = true
		case tagTimedDefense:
			if defense || op.Count != 3 {
				return
			}
			defense = true
			result.Physical, result.Magical, result.CapPercent = op.Arguments[0], op.Arguments[1], op.Arguments[2]
		case tagTimedHaste, tagTimedOverride, tagTimedIndependent:
			if movement || op.Count != 1 || op.Arguments[0] == 0 || !row.MovementModifier.Present ||
				op.Arguments[0] != row.MovementModifier.Percent {
				return
			}
			movement = true
		case tagTimedBlock:
			if result.Block.Present || op.Count != 2 || op.Arguments[1] > maxBlockRatePercent {
				return
			}
			result.Block = SkillBlockBoost{Present: true, Mask: normalizeLaneMask(op.Arguments[0]), Value: op.Arguments[1]}
		case tagTimedStrength:
			if !boost(&result.Strength, op) {
				return
			}
		case tagTimedIntellect:
			if !boost(&result.Intellect, op) {
				return
			}
		case tagTimedLink:
			if result.Link.Present || op.Count != 4 || !targeted || op.Arguments[0] == 0 {
				return
			}
			result.Link = SkillEffectLink{Present: true, Group: op.Arguments[0], MaxDistance: op.Arguments[1], MaxOutgoing: op.Arguments[2], Board: op.Arguments[3]}
		case tagTimedLinkedThreat:
			// lkag rides the link context (ApplyLink, 594EAC): 5A03A0 reads
			// word 0 only (+0x47C), word 1 has no reader, and the share is
			// formed as authored. The row's lnks is checked after the walk.
			if linkThreat || op.Count != 2 {
				return
			}
			linkThreat, linkThreatPercent = true, op.Arguments[0]
		case tagTimedLinkPerTarget:
			// lks2 rides an lnks that sets an outgoing count; Mana Switch
			// authors none (word 2 is 0), so the per-recipient count has
			// nothing to change. A counted link would need it ported first.
			if !result.Link.Present || result.Link.PerTarget || result.Link.MaxOutgoing != 0 {
				return
			}
			result.Link.PerTarget = true
		case tagTimedLinkedDamage:
			// lkdh {HP percent, MP percent, cap} (+0x3E0): 5A04A0 pays the
			// link source both shares as authored, each held at the cap.
			if linkDamage || op.Count != 3 {
				return
			}
			linkDamage = true
			linkDamageWords = [3]uint32{op.Arguments[0], op.Arguments[1], op.Arguments[2]}
		case tagTimedIncomingReduction:
			// odar is installed from BuffModifiers for every recipient (594AC0);
			// the program only has to agree with that projection.
			if result.IncomingReduction || op.Count != 2 || !row.BuffModifiers.Odar || op.Arguments[1] != row.BuffModifiers.OdarWord {
				return
			}
			result.IncomingReduction = true
		case tagTimedPreemptive:
			// A self-only guard: the protection is the owner's, so it
			// never rides a target, an area or a link.
			if result.Preemptive.Present || op.Count != 2 || op.Arguments[0] == 0 || targeted || result.Area.Present {
				return
			}
			result.Preemptive = SkillPreemptiveGuard{Present: true, Mask: op.Arguments[0], Level: op.Arguments[1]}
		case tagTimedOverlap: // ovl2: the replacement descriptor's casting-state word
		case tagNbuf, tagBbuf: // cancellation policy and secondary board, already projected
		case tagReqi, tagTimedRequireNot: // 58D480 admits, 59F0E0 re-checks on equipment change
		case tagEfr: // read above
		case tagGetv:
			switch op.Arguments[0] {
			case parameterBardMP, parameterMusicArea:
				// 5832E6 applies BDMD to the prepared cost. MUER is indexed,
				// but 5830B0 reads it only when creating an efr-kind-2 aura;
				// the kind-1 March selection keeps its authored radius.
				musicParameters = true
			case parameterWizardMP:
				// The prepared cost already applies WIMD (noteParameterIndex).
			case parameterBlessingPhysical:
				result.PhysicalAddend = true
			case parameterBlessingMagical:
				result.MagicalAddend = true
			case parameterBlessingStrength:
				result.StrengthAddend = true
			case parameterBlessingIntellect:
				result.IntellectAddend = true
			default:
				return
			}
		case tagCbuf: // 59B8D0: cbuf + dura enters the owner timed-job path
			result.Persistent = true
		default:
			return
		}
	}
	if linkThreat || linkDamage {
		if !result.Link.Present {
			return
		}
		result.Link.Threat, result.Link.ThreatPercent = linkThreat, linkThreatPercent
		result.Link.Mana = linkDamage
		result.Link.ManaHPPercent, result.Link.ManaPercent, result.Link.ManaCap =
			linkDamageWords[0], linkDamageWords[1], linkDamageWords[2]
	}
	// A link carries only the stat blessings and lkag so far; a linked defp
	// would need the source-half rule of 5951FC checked first. The linked
	// runtime installs only stri/inti writes, so a threat link with blk or
	// odar would silently drop them and stays refused.
	// A damage link carries lkdh alone: the linked runtime has no rule for
	// it beside a threat share or stat writes.
	if result.Link.Mana && (result.Link.Threat || result.Strength.Present || result.Intellect.Present ||
		result.Block.Present || result.IncomingReduction || len(attributeTags) != 0) {
		return
	}
	if result.Link.Threat && (result.Block.Present || result.IncomingReduction) ||
		result.Link.Present && (defense || result.Area.Present || result.Persistent) ||
		result.StrengthAddend && !result.Strength.Present || result.IntellectAddend && !result.Intellect.Present ||
		result.Preemptive.Present && (result.Link.Present || result.Persistent || row.EffectDurationMs == 0) {
		return
	}
	partySelection := result.Area.Select == SelectParty || result.Area.Select == SelectParty|SelectCaster
	if movement && (targeted || !result.Area.Present || !partySelection || result.Persistent || result.Link.Present || row.EffectDurationMs == 0) ||
		musicParameters && !movement && !result.Preemptive.Present && !result.Link.Mana {
		return
	}
	result.Defense = defense
	attributes := result.Attributes.MaxHP || result.Attributes.Attack || result.Attributes.DamagePenalty ||
		result.Attributes.DamageRate || result.Attributes.MaxHPPenalty
	if len(attributeTags) != 0 && (!attributes || result.Persistent || result.Link.Present || movement || defense ||
		result.Block.Present || result.Strength.Present || result.Intellect.Present || row.EffectDurationMs == 0) {
		return
	}
	result.Pinned = duration && (attributes || defense || movement || result.Block.Present || result.Strength.Present ||
		result.Intellect.Present || result.IncomingReduction || result.Link.Present && (result.Link.Threat || result.Link.Mana) ||
		result.Preemptive.Present)
	result.Targeted = targeted
	row.TimedEffect = result
	if result.Pinned && movement {
		row.MovementModifier.Supported = true
	}
}

/*
================
TimedJobExecutable

Require a complete producer, not cbuf presence alone. Ordinary party buffs do
not survive logout through the timed-item-job protocol (59B8D0/650E70).
================
*/
func (row SkillRow) TimedJobExecutable() bool {
	return row.MovementModifier.Supported && row.MovementModifier.Persistent ||
		row.TimedEffect.Pinned && row.TimedEffect.Persistent ||
		row.Concealment.Pinned && row.Concealment.Persistent
}
