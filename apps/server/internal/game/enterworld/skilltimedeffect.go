/*
===========================================================================

skilltimedeffect.go - complete timed-effect descriptor admission

The compiler consumes the whole program before granting an executable route.
Ordinary casts and item-owned timed jobs share effect descriptors and lifecycle.

===========================================================================
*/
package enterworld

// SkillTimedEffect is an entire category-three, unlinked self program. The
// instruction compiler must consume every operation before granting a route.
// Native 5830B0 owns preparation/release; 5951FC..59533C owns defp writes.
/*
================
SkillTimedEffect
================
*/
type SkillTimedEffect struct {
	ItemProgram                   bool
	HP, MP, Evasion, Accuracy     SkillFlatRate
	Pinned                        bool
	Persistent                    bool
	Physical, Magical, CapPercent uint32
	// Targeted rows (Warrior guards, Cleric blessings) install on a player
	// within column 21's range instead of the caster.
	Targeted bool
	// Area is an efr kind 1 selection (Heal Shield): the caster and the
	// recipients TargetSelection_AroundSource picks each get an instance.
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
	Defense bool
	Block   SkillBlockBoost
}

// SkillBlockBoost is one br block, its mask normalized.
/*
================
SkillBlockBoost
================
*/
type SkillBlockBoost struct {
	Present     bool
	Mask, Value uint32
}

// SkillStatBoost is one stri / inti block.
/*
================
SkillStatBoost
================
*/
type SkillStatBoost struct {
	Present           bool
	Value, CapPercent uint32
}

// SkillEffectLink is lnks {group, max distance, max outgoing, board}. A
// zero board word keeps the source half off the caster's board (client
// B5ED); the server installs and announces it either way.
/*
================
SkillEffectLink
================
*/
type SkillEffectLink struct {
	Present                         bool
	Group, MaxDistance, MaxOutgoing uint32
	Board                           uint32
}

/*
================
parseSkillTimedEffect
================
*/
func parseSkillTimedEffect(fields []string, row *SkillRow) {
	if item, ok := compileTimedItemEffect(fields, *row); ok {
		row.TimedEffect = item
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
	duration, defense := false, false
	boost := func(b *SkillStatBoost, op SkillInstruction) bool {
		if b.Present || op.Count != 2 {
			return false
		}
		*b = SkillStatBoost{Present: true, Value: op.Arguments[0], CapPercent: op.Arguments[1]}
		return true
	}
	for i := 0; i < program.Len(); i++ {
		op := program.Instruction(i)
		switch op.Tag {
		case 0x64757261:
			if duration || op.Count != 1 {
				return
			}
			duration = true
		case 0x64656670:
			if defense || op.Count != 3 {
				return
			}
			defense = true
			result.Physical, result.Magical, result.CapPercent = op.Arguments[0], op.Arguments[1], op.Arguments[2]
		case 0x6272: // br: lane mask, value
			if result.Block.Present || op.Count != 2 || op.Arguments[1] > 100 {
				return
			}
			result.Block = SkillBlockBoost{Present: true, Mask: normalizeLaneMask(op.Arguments[0]), Value: op.Arguments[1]}
		case 0x73747269: // stri
			if !boost(&result.Strength, op) {
				return
			}
		case 0x696e7469: // inti
			if !boost(&result.Intellect, op) {
				return
			}
		case 0x6c6e6b73: // lnks
			if result.Link.Present || op.Count != 4 || !targeted || op.Arguments[0] == 0 {
				return
			}
			result.Link = SkillEffectLink{Present: true, Group: op.Arguments[0], MaxDistance: op.Arguments[1], MaxOutgoing: op.Arguments[2], Board: op.Arguments[3]}
		case 0x6e627566, 0x62627566: // cancellation policy and secondary board, already projected
		case 0x72657169, 0x7265716e: // reqi/reqn: 58D480 admits, 59F0E0 re-checks on equipment change
		case tagEfr: // read above
		case tagGetv:
			switch op.Arguments[0] {
			case 0x484c4250:
				result.PhysicalAddend = true
			case 0x484c534d:
				result.MagicalAddend = true
			case 0x484c4653:
				result.StrengthAddend = true
			case 0x484c4d49:
				result.IntellectAddend = true
			default:
				return
			}
		case 0x63627566: // 59B8D0: cbuf + dura enters the owner timed-job path
			result.Persistent = true
		default:
			return
		}
	}
	// A link carries only the stat blessings so far; a linked defp would
	// need the source-half rule of 5951FC checked first.
	if result.Link.Present && (defense || result.Area.Present || result.Persistent) ||
		result.StrengthAddend && !result.Strength.Present || result.IntellectAddend && !result.Intellect.Present {
		return
	}
	result.Defense = defense
	result.Pinned = duration && (defense || result.Block.Present || result.Strength.Present || result.Intellect.Present)
	result.Targeted = targeted
	row.TimedEffect = result
}

// TimedJobExecutable requires a complete producer, not cbuf presence alone.
// The persistence protocol is shared by all such producers (59B8D0/650E70).
/*
================
TimedJobExecutable
================
*/
func (row SkillRow) TimedJobExecutable() bool {
	return row.MovementModifier.Supported && row.MovementModifier.Persistent ||
		row.TimedEffect.Pinned && row.TimedEffect.Persistent ||
		row.Concealment.Pinned && row.Concealment.Persistent
}
