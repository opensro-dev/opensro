/*
===========================================================================

skilloffense.go - skill parameter index and offense admission (SkillGlobal_BuildParameterIndex 587630)

Compile authored rows into executable offensive shapes. A recognized parameter
alone cannot admit a program whose resource or action lifecycle is unsupported.

===========================================================================
*/

package enterworld

import (
	"strconv"

	"opensro.online/server/internal/game/abnormal"
)

/*
==================
SkillConsumption

SkillConsumption is the authored HP/MP requirement, not a learn/SP price.
v1.150 7f970d/7f971d/7f972d/7f973e store columns 52..55 at
CSkillData+774/+778/+77c/+77e. Server 58e1b0..58e2bb checks the
corresponding flat amounts plus truncated percentages of maximum vitals.
==================
*/
type SkillConsumption struct {
	HP, MP               uint32
	HPPercent, MPPercent uint16
	Pinned               bool
}

/*
==================
ammunitionFreeWeapons

An explicit weapon family without either bow kind requires no ammunition.
The unspecified family remains subject to the caller's equipment checks.
==================
*/
func ammunitionFreeWeapons(kinds [2]uint8) bool {
	return kinds[0] != 255 && kinds[0] != 6 && kinds[0] != 12 && kinds[1] != 6 && kinds[1] != 12
}

/*
==================
SkillAmmunition

cnsm is an equipped-item family/count requirement (587F57, 58E331),
independent of the HP/MP consumption columns.
==================
*/
type SkillAmmunition struct{ TID3, TID4, Count uint32 }

const (
	crossbowWeaponKind     = 12
	bowWeaponKind          = 6
	maximumAmmunitionStack = 0xffff
)

/*
==================
SkillOffensiveArea

EFR kind 1 retains its authored selection: shape 1 is caster-centered,
shape 2 is primary-target-centered, and shape 6 orders secondary targets by
center-to-center distance from the primary without body-radius expansion.
==================
*/
type SkillOffensiveArea struct {
	Radius           uint32
	MaxTargets       uint8
	ReductionPercent uint8
	Shape            uint8
	// Select is the efr +0x14 candidate mask (TargetSelection_AroundSource
	// 58A020): 0x01 caster, 0x02 every character, 0x04 party, 0x08
	// hostile, 0x10 non-character objects. Every admitted offense row is
	// 24; 10 and 26 belong to the unported Detect rows.
	Select uint8
}

/*
==================
parseSkillOffense

Index shared admission before decoding executable programs. A quest trap is
a world object; decoding its program must not admit it as a combat attack.
==================
*/
func parseSkillOffense(fields []string, row *SkillRow) {
	noteParameterIndex(fields, row)
	row.OffenseRefusal = decodeSkillOffense(fields, row)
	row.CastGate.QuestTrap = compileQuestTrap(fields)
	row.CombatTrap = compileCombatTrap(fields, *row)
}

/*
==================
noteParameterIndex

noteParameterIndex is the part of SkillGlobal_BuildParameterIndex (587630)
that every skill kind reads, recorded on every row: 58D8F0 admits attacks,
buffs and heals alike. It is the only writer of these fields;
decodeSkillOffense only validates them.

	getv WIMD/BDMD/HLMD  +0x4E4/+0x554/+0x55C  prepared MP cut
	getv WIRU/CBRA       +0x4E8/+0x50C         cast reach addends (4AE87E)
	getv MUER/DSER       +0x544/+0x54C         aura radius addends
	getv MUCR/DSCR       -                     aura cut resistance (Prism, Screen Dance)
	scls                 +0x380                selector bits (5842AC)
	reqc                 +0x39C                bits 0, 4, 5 (SkillReqc)
	reqi                 +0x3A0                up to five {kind, value}
	reqn                 +0x3B4                every reqi pair must match
	efr kind 2, onff     +0x290, +0x284        persistent aura (SkillAura)
	efr kind 3           +0x294                qest radius word
	dru, odar, ru, hr,
	rhru, dcmp           +0x3E4, +0x270, ...   SkillBuffModifiers
	heal, mwhh, mwmh     +0x324..+0x32C        SkillHeal
	eshp                 +0x298                aura heals the lowest HP ratio
	nmf, tele/tel2/tel3,
	ao, pw, rpkt, qest,
	msch, hide, trap     see SkillCastGate     58D8F0 caster-state gates

==================
*/
func noteParameterIndex(fields []string, row *SkillRow) {
	word := func(i int) (uint32, bool) {
		if i >= len(fields) {
			return 0, false
		}
		v, ok := textdataInt(fields[i])
		return uint32(v), ok && v >= 0 && v <= 0xffffffff
	}
	for i := skilldataColEncodedTail; i < len(fields); {
		tag, ok := textdataInt(fields[i])
		if !ok || tag == 0x73736f75 {
			return
		}
		// A zero word is padding, not the end of the program: the native
		// indexers skip it, as CompileSkillProgram does, so a getv or reqi
		// authored after a padded opcode ("odar 4 n 0 getv ...") still
		// reaches the row.
		if tag == 0 {
			i++
			continue
		}
		switch tag {
		case tagGetv: // getv
			// WIRU/CBRA (+0x4E8/+0x50C) sit beside WIMD (+0x4E4) in this
			// per-row index, so a row without att records them too, and
			// 4AE87E adds them to the cast's reach. Owners that never
			// compute a reach simply ignore the bit.
			if key, ok := word(i + 1); ok {
				if slot, known := SkillParameterFromKey(key); known &&
					(slot == ParameterWizardMPDecrease || slot == ParameterBardMPDecrease || slot == ParameterHealerMPDecrease ||
						slot == ParameterMusicRange || slot == ParameterDanceRange || slot == ParameterHealRecoveryUp ||
						slot == ParameterWizardRange || slot == ParameterCrossbowRange ||
						slot == ParameterMusicCutResist || slot == ParameterDanceCutResist) {
					row.Attack.Parameters |= SkillParameterMask(1) << slot
				}
			}
		case 0x73636c73: // scls
			if mask, ok := word(i + 1); ok && mask != 0 {
				row.SelectorMask = mask
			}
		case 0x72657163: // reqc
			if flags, ok := word(i + 1); ok {
				row.Reqc = SkillReqc{Present: true, KnockedDown: flags&1 != 0, LowHP: flags&4 != 0, Flag16: flags&0x10 != 0, Dance: flags&32 != 0}
			}
		case 0x72657169: // reqi; a sixth pair is fatal at 589357, never stored
			kind, kindOK := word(i + 1)
			value, valueOK := word(i + 2)
			if kindOK && valueOK && row.Reqi.Count < len(row.Reqi.Pairs) {
				row.Reqi.Pairs[row.Reqi.Count] = SkillReqiPair{Kind: kind, Value: value}
				row.Reqi.Count++
				row.Reqi.Present = true
			}
		case 0x7265716e: // reqn
			row.Reqi.All = true
		case tagEfr: // efr; kind 1 is the action area at +0x28C, kind 2 a persistent area at +0x290, kind 3 at +0x294
			kind, kindOK := word(i + 1)
			radius, radiusOK := word(i + 3)
			maxTargets, maxOK := word(i + 4)
			selectWord, selectOK := word(i + 6)
			if area, ok := actionAreaAt(fields, i); kindOK && kind == 1 && ok {
				row.ActionArea = area
			} else if kindOK && kind == 2 && radiusOK && maxOK && selectOK {
				row.Aura.Present = true
				row.Aura.Radius = radius
				row.Aura.MaxTargets = maxTargets
				row.Aura.Select = selectWord
			} else if kindOK && kind == 3 && radiusOK {
				row.CastGate.Efr3Present = true
				row.CastGate.Efr3Radius = radius
			}
		case 0x72706b74: // rpkt +0x2CC
			row.CastGate.Rpkt = true
		case 0x71657374: // qest +0x49C
			row.CastGate.Qest = true
		case 0x6d736368: // msch +0x4A4
			if mode, ok := word(i + 1); ok {
				row.CastGate.MschPresent = true
				row.CastGate.MschMode = mode
				row.CastGate.MschLevel, _ = word(i + 2)
			}
		case 0x68696465: // hide +0x428
			if mode, ok := word(i + 1); ok {
				row.CastGate.HideGatePresent = true
				row.CastGate.HideGateMode = mode
			}
		case 0x74726170: // trap +0x4A0
			row.CastGate.TrapPresent = true
		case 0x7275: // ru +0x250
			if rate, ok := word(i + 1); ok {
				row.BuffModifiers.Ru = true
				row.BuffModifiers.RuRate = rate
			}
		case 0x6872: // hr +0x24C
			flat, flatOK := word(i + 1)
			rate, rateOK := word(i + 2)
			if flatOK && rateOK {
				row.BuffModifiers.Hr = true
				row.BuffModifiers.HrFlat = flat
				row.BuffModifiers.HrRate = rate
			}
		case 0x647275: // dru +0x3E4
			first, firstOK := word(i + 1)
			second, secondOK := word(i + 2)
			if firstOK && secondOK {
				row.BuffModifiers.Dru = true
				row.BuffModifiers.DruWords = [2]uint32{first, second}
			}
		case 0x72687275: // rhru: healing received, HP and MP percent
			hp, hpOK := word(i + 1)
			mp, mpOK := word(i + 2)
			if hpOK && mpOK {
				row.BuffModifiers.Rhru = true
				row.BuffModifiers.RhruWords = [2]uint32{hp, mp}
			}
		case 0x64636d70: // dcmp: MP consumption cut, percent
			if percent, ok := word(i + 1); ok {
				row.BuffModifiers.Dcmp = true
				row.BuffModifiers.DcmpPercent = percent
			}
		case 0x6f646172: // odar +0x270
			bits, bitsOK := word(i + 1)
			value, valueOK := word(i + 2)
			if bitsOK && valueOK {
				switch bits {
				case 4, 8, 0xc:
					bits |= 1 | 2
				case 1, 2, 3:
					bits |= 4 | 8
				}
				row.BuffModifiers.Odar = true
				row.BuffModifiers.OdarBits = bits
				row.BuffModifiers.OdarWord = value
			}
		case 0x6e6d66: // nmf +0x594
			row.CastGate.Nmf = true
		case 0x74656c65: // tele +0x2EC
			row.CastGate.Tele = true
		case 0x74656c32: // tel2 +0x2F0
			row.CastGate.Tel2 = true
		case 0x74656c33: // tel3 +0x2F4
			row.CastGate.Tel3 = true
		case 0x616f: // ao +0x274
			row.CastGate.Ao = true
		case 0x7077: // pw +0x2B4
			row.CastGate.Pw = true
		case 0x65736870: // eshp +0x298
			row.Aura.Eshp = true
		case 0x6865616c: // heal +0x324
			hp, hpOK := word(i + 1)
			hpPct, hpPctOK := word(i + 2)
			mp, mpOK := word(i + 3)
			mpPct, mpPctOK := word(i + 4)
			if hpOK && hpPctOK && mpOK && mpPctOK {
				row.Heal.Present = true
				row.Heal.HP, row.Heal.HPPercent, row.Heal.MP, row.Heal.MPPercent = hp, hpPct, mp, mpPct
			}
		case 0x6d776868: // mwhh +0x328
			if value, ok := word(i + 1); ok {
				row.Heal.WeaponHP, row.Heal.WeaponHPWord = true, value
			}
		case 0x6d776d68: // mwmh +0x32C
			if value, ok := word(i + 1); ok {
				row.Heal.WeaponMP, row.Heal.WeaponMPWord = true, value
			}
		case 0x6e6d68: // nmh +0x598
			row.Heal.OfMaxHP = true
		case 0x636b, 0x6c667374, 0x70646d67, 0x70646d32: // ck, lfst, pdmg, pdm2 (589EE0)
			row.WallBypass = true
			// ck (+0x248) also takes the target's block chance away (58E624).
			if tag == 0x636b {
				row.Ck = true
				// 58EC61: the low byte of ck's first word is the kill chance.
				if chance, ok := word(i + 1); ok {
					row.CkChance = uint8(chance)
				}
			}
		case 0x6f6e6666: // onff
			period, periodOK := word(i + 1)
			cost, costOK := word(i + 2)
			if periodOK && costOK {
				row.Aura.PulseMs = period
				row.Aura.PulseMP = cost
			}
		}
		i += 1 + spawnParamArity(uint32(tag))
	}
}

/*
==================
actionAreaAt

The efr kind-1 block whose tag sits at fields[i]: {kind 1, shape, radius,
max targets, reduction percent, select}. Shapes 1-4 and 6 are the native
selectors (TargetSelection_DispatchByShape); 10, 24 and 26 the shipped
select words. Native shape 6 bounds its initial spatial search to 450
units, so a larger authored radius needs a search volume no row has.
==================
*/
func actionAreaAt(fields []string, i int) (SkillOffensiveArea, bool) {
	if i+6 >= len(fields) {
		return SkillOffensiveArea{}, false
	}
	var area [6]int64
	for j := range area {
		value, valid := textdataInt(fields[i+j+1])
		if !valid {
			return SkillOffensiveArea{}, false
		}
		area[j] = value
	}
	validShape := area[1] >= 1 && area[1] <= 4 || area[1] == 6
	validSelect := area[5] == 10 || area[5] == 24 || area[5] == 26
	if area[0] != 1 || !validShape || area[2] <= 0 || area[2] > 0xffff ||
		area[3] <= 0 || area[3] > 255 || area[4] < 0 || area[4] > 100 || !validSelect ||
		area[1] == 6 && area[2] > 450 {
		return SkillOffensiveArea{}, false
	}
	return SkillOffensiveArea{
		Radius:           uint32(area[2]),
		MaxTargets:       uint8(area[3]),
		ReductionPercent: uint8(area[4]),
		Shape:            uint8(area[1]),
		Select:           uint8(area[5]),
	}, true
}

/*
==================
decodeSkillOffense

Production and coverage reports share this gate. A partial parameter parse
must never make an unsupported attack look executable.
==================
*/
func decodeSkillOffense(fields []string, row *SkillRow) string {
	if len(fields) != 118 {
		return "offense:invalid-envelope-or-arguments"
	}
	if _, err := CompileSkillProgram(fields); err != nil {
		return "offense:invalid-envelope-or-arguments"
	}
	values := [4]int64{}
	for i := range values {
		value, ok := textdataInt(fields[52+i])
		limit := int64(0xffffffff)
		if i >= 2 {
			limit = 65535
		}
		if !ok || value < 0 || value > limit {
			return "offense:invalid-envelope-or-arguments"
		}
		values[i] = value
	}
	row.Consumption = SkillConsumption{uint32(values[0]), uint32(values[1]), uint16(values[2]), uint16(values[3]), true}
	if taunt := compileSkillTaunt(fields, *row); taunt.Only {
		row.Threat = taunt
		return ""
	}
	if decrease, ok := compileSkillThreatDecrease(fields, *row); ok {
		row.Threat = decrease
		return ""
	}
	if threat, ok := compileSkillStatusCast(fields, *row); ok {
		// Retail initializes the generated-result count to one even without
		// att or cm; the single record carries the status roll.
		row.StatusCast = true
		row.Threat = threat
		row.OffensiveArea, row.Threat.Area = threat.Area, SkillOffensiveArea{}
		row.Attack.ImpactCount = 1
		row.OffensiveStagePinned = true
		row.DirectOffensePinned = true
		return ""
	}
	if fixed, ok := compileSkillFixedDamage(fields, *row); ok {
		// One fixed-damage record (skillfixeddamage.go), released by the
		// ordinary single-target offensive owner.
		row.FixedDamage = fixed
		row.Attack.ImpactCount = 1
		row.OffensiveStagePinned = true
		row.DirectOffensePinned = true
		return ""
	}
	if area, ok := compileSkillAreaBurst(fields, *row); ok {
		// Untargeted caster-centred attack (skillareaburst.go): the target
		// gate below would refuse it, as it did before an owner existed.
		row.AreaBurst = true
		row.OffensiveArea = area
		row.OffensiveStagePinned = true
		row.DirectOffensePinned = true
		return ""
	}
	// Admission is by executable shape, never a hand-maintained skill-name list.
	// Linked casts and additional effect blocks require their own authority
	// operations; they cannot be silently reduced to one damage result.
	// An HP cost is checked on maximum HP (58E1B6) and charged on current HP,
	// never below 1 (58312C, 4A8770; action/skillcost.go).
	if !row.CombatPinned || !row.TimingPinned || !row.ActionRangePinned || !row.TargetRequired ||
		fields[0] != "1" || fields[15] != "0" || fields[17] != "0" || fields[56] != "0" {
		return "offense:invalid-envelope-or-arguments"
	}
	// Metadata is loaded by the shared row parser; malformed flight data still
	// refuses player-offense admission rather than becoming an instant attack.
	if speed, ok := textdataInt(fields[16]); !ok || speed < 0 || speed > 0xffffffff {
		return "offense:invalid-envelope-or-arguments"
	}
	seen := map[int64]bool{}
	reqiPairs := 0
	for i := 69; i < len(fields); {
		tag, ok := textdataInt(fields[i])
		if !ok {
			return "offense:invalid-envelope-or-arguments"
		}
		if tag == 0 {
			for _, rest := range fields[i:] {
				if rest != "0" {
					return "offense:invalid-envelope-or-arguments"
				}
			}
			if seen[int64(tagPositionCharge)] && seen[tagEfr] {
				return "offense:charge-area"
			}
			// Crossbow stages own their cnsm debit (585FB6), including
			// zero-preparation linked shots. Keep other weapon families on
			// their reviewed envelope; graph validation remains root-owned.
			// FIXME: routing still keys on the flying speed, so ActionHandler 1
			// rows at speed 0 (SKILL_CH_SPEAR_SHOOT_*, area shape 4) run the
			// instant owner. SkillAction_Projectile (5857B0) at speed 0 flies
			// 0 ms but, unlike SkillAction_Instant (586700), applies no
			// position effect, registers no hostile target and hits when the
			// bow-shot record lands. Only the MP rule follows the handler so far.
			if row.ProjectileSpeed != 0 || seen[0x636e736d] {
				crossbow := row.RequiredWeaponKinds == ([2]uint8{crossbowWeaponKind, 255})
				// SkillAction_Projectile (5857B0) links stages for any
				// launcher: the bow's Arrow Combo C and D chain zero-preparation
				// shots exactly as the crossbow's lines do.
				chained := crossbow || row.RequiredWeaponKinds == ([2]uint8{bowWeaponKind, 255})
				// Several mc impacts resolve at release together and spend
				// cnsm count x impacts arrows (585AF0).
				if row.ProjectileSpeed == 0 ||
					!chained && (row.ActionCastingTimeMs == 0 || row.ChainSub || row.ChainNext != 0) ||
					row.Attack.ImpactCount == 0 ||
					!(row.Ammunition == (SkillAmmunition{4, 1, 1}) && row.RequiredWeaponKinds == ([2]uint8{6, 255}) ||
						row.Ammunition.TID3 == 4 && row.Ammunition.TID4 == 2 &&
							row.Ammunition.Count > 0 && row.Ammunition.Count <= maximumAmmunitionStack && crossbow ||
						!seen[0x636e736d] && row.Ammunition == (SkillAmmunition{}) && ammunitionFreeWeapons(row.RequiredWeaponKinds)) {
					return "offense:invalid-envelope-or-arguments"
				}
			}
			row.OffensiveStagePinned = seen[skillAttackTag]
			row.DirectOffensePinned = row.OffensiveStagePinned && row.ChainNext == 0
			return ""
		}
		if seen[tag] && tag != tagGetv && tag != 0x72657169 {
			return "offense:duplicate-instruction:" + strconv.FormatInt(tag, 16)
		}
		seen[tag] = true
		arity := 0
		// 590680 rolls every tagRefSkill status block; admission needs only
		// well-formed words. Values are read by the shared parser.
		if index, found := abnormal.SourceIndex(uint32(tag)); found {
			source := abnormal.Sources[index]
			if !validAbnormalBlock(fields, i, source) {
				return "offense:abnormal-arguments:" + strconv.FormatInt(tag, 16)
			}
			i += 1 + int(source.Arity)
			continue
		}
		switch tag {
		case 0x7275: // ru: flat weapon-range addend, 4AE849..4AE87A
			// 4AE849 adds it to any equipment-derived reach, whatever the
			// weapon: the crossbow's Dual Shot and the bow's Arrow Combo D
			// and Strong Bow C author it alike (skillActionReach).
			arity = 1
			if i+arity >= len(fields) {
				return "offense:range-arguments"
			}
			value, valid := textdataInt(fields[i+1])
			if !valid || value < 0 || value > 0xffffffff {
				return "offense:range-arguments"
			}
		case 0x74656c33: // tel3: instant target charge, planned by 5862E0
			arity = 2
			if i+arity >= len(fields) || fields[68] != "0" || row.ChainNext != 0 || row.ChainSub || row.ProjectileSpeed != 0 ||
				row.ActionCastingTimeMs != 0 || row.ActionDurationMs != 0 || row.Attack.ImpactCount != 1 {
				return "offense:charge-envelope"
			}
			parameter, parameterOK := textdataInt(fields[i+1])
			rangeWord, rangeOK := textdataInt(fields[i+2])
			if !parameterOK || parameter < 0 || parameter > 0xffffffff || !rangeOK || rangeWord <= 0 || rangeWord > 0x7fffffff {
				return "offense:charge-arguments"
			}
		case skillPulseTag:
			arity = 1
			if i+arity >= len(fields) {
				return "offense:invalid-envelope-or-arguments"
			}
		case 0x6872: // hr {flat, rate}: the attack's own hit-rate bonus
			// SkillCombat_EngageSkill (593540) installs the engaged skill's
			// modifier block through 594AC0 (+0x24C, parameter 11), so the
			// attack's hits roll with it (action/skillengage.go). The bow's
			// Arrow Rain lines (AREA_A..C) author it.
			arity = 2
			if i+arity >= len(fields) || !row.BuffModifiers.Hr {
				return "offense:invalid-envelope-or-arguments"
			}
		case 0x6b6f: // ko: victim-level rank and probability; full action consequence
			arity = 2
			if i+arity >= len(fields) {
				return "offense:invalid-envelope-or-arguments"
			}
			var args [2]uint32
			for j := range args {
				value, valid := textdataInt(fields[i+j+1])
				if !valid || value < 0 || value > 0xffffffff {
					return "offense:invalid-envelope-or-arguments"
				}
				args[j] = uint32(value)
			}
			row.Knockdown = SkillKnockdown{Present: true, Rank: args[0], Chance: args[1]}
		case 0x6b62:
			arity = 2
			if i+arity >= len(fields) {
				return "offense:invalid-envelope-or-arguments"
			}
			var args [2]uint32
			for j := range args {
				v, valid := textdataInt(fields[i+j+1])
				if !valid || v < 0 || v > 0xffffffff {
					return "offense:invalid-envelope-or-arguments"
				}
				args[j] = uint32(v)
			}
			row.Knockback = SkillKnockback{Present: true, Chance: args[0], Distance: args[1]}
		case 0x746e7432: // tnt2: flat aggression, cumulative percentage
			arity = 2
			if i+arity >= len(fields) {
				return "offense:invalid-envelope-or-arguments"
			}
			var args [2]uint32
			for j := range args {
				value, valid := textdataInt(fields[i+j+1])
				if !valid || value < 0 || value > 0xffffffff {
					return "offense:invalid-envelope-or-arguments"
				}
				args[j] = uint32(value)
			}
			row.Threat = SkillThreat{Present: true, Flat: args[0], Percent: args[1]}
		case 0x61746361: // atca: flag mask and percent at RefSkill+0x3BC
			arity = 2
			if i+arity >= len(fields) {
				return "offense:invalid-envelope-or-arguments"
			}
			mask, maskOK := textdataInt(fields[i+1])
			percent, percentOK := textdataInt(fields[i+2])
			if !maskOK || !percentOK || mask < 0 || percent < 0 || mask > 0xffffffff || percent > 0xffffffff {
				return "offense:invalid-envelope-or-arguments"
			}
			row.Attack.Atca = true
			row.Attack.AtcaMask = uint32(mask)
			row.Attack.AtcaPercent = uint32(percent)
		case 0x72657169: // reqi: recorded by noteParameterIndex
			arity = 2
			reqiPairs++
			if i+arity >= len(fields) || reqiPairs > len(row.Reqi.Pairs) {
				return "offense:invalid-envelope-or-arguments"
			}
			kind, kindOK := textdataInt(fields[i+1])
			value, valueOK := textdataInt(fields[i+2])
			if !kindOK || !valueOK || kind < 0 || kind > 0xffffffff || value < 0 || value > 0xffffffff {
				return "offense:invalid-envelope-or-arguments"
			}
		case 0x7265716e: // reqn: recorded by noteParameterIndex
			arity = 1
			if i+arity >= len(fields) {
				return "offense:invalid-envelope-or-arguments"
			}
		case 0x72657163: // reqc: recorded by noteParameterIndex
			arity = 1
			if i+arity >= len(fields) {
				return "offense:invalid-envelope-or-arguments"
			}
			value, valid := textdataInt(fields[i+1])
			if !valid || value < 0 || value > 0xffffffff {
				return "offense:invalid-envelope-or-arguments"
			}
			// Only bits 0 and 5 have an owner in this port.
			if value&^(1|32) != 0 {
				return "offense:reqc:" + strconv.FormatInt(value, 16)
			}
		case 0x6461: // da: unsigned downed-target damage percentage
			arity = 1
			if i+arity >= len(fields) {
				return "offense:invalid-envelope-or-arguments"
			}
			value, valid := textdataInt(fields[i+1])
			if !valid || value < 0 || value > 0xffffffff {
				return "offense:invalid-envelope-or-arguments"
			}
			row.Attack.DownAttack = SkillDownAttack{Present: true, Percent: uint32(value)}
		case 0x636e736d: // cnsm: TID3, TID4 (zero is wildcard), count
			arity = 3
			if i+arity >= len(fields) {
				return "offense:invalid-envelope-or-arguments"
			}
			var args [3]uint32
			for j := range args {
				value, valid := textdataInt(fields[i+j+1])
				if !valid || value < 0 || value > 0xffffffff {
					return "offense:invalid-envelope-or-arguments"
				}
				args[j] = uint32(value)
			}
			row.Ammunition = SkillAmmunition{args[0], args[1], args[2]}
		case skillAttackTag:
			arity = 5
		case skillMultiImpactTag:
			arity = 2
		case skillCriticalTag:
			arity = 2
			if !row.CriticalModifier.Present {
				return "offense:critical-arguments" // shared parser must validate the unsigned pair first
			}
		case tagGetv:
			arity = 1
			if i+1 >= len(fields) {
				return "offense:invalid-envelope-or-arguments"
			}
			key, valid := textdataInt(fields[i+1])
			if !valid || key < 0 || key > 0xffffffff {
				return "offense:invalid-envelope-or-arguments"
			}
			abnormalKey := key == int64(abnormal.KeyPoisonDamage) || key == int64(abnormal.KeyPoisonDuration) || key == int64(abnormal.KeyTrapDamage)
			_, known := SkillParameterFromKey(uint32(key))
			if !known && key != 0x4d414154 && !abnormalKey {
				return "offense:getv:" + strconv.FormatInt(key, 16)
			}
		case tagEfr:
			arity = 6
			// Every victim takes every mc impact (58E5F0 loops impacts per
			// target group; action/skillarea.go).
			if i+arity >= len(fields) || row.Attack.ImpactCount == 0 {
				return "offense:invalid-envelope-or-arguments"
			}
			area, valid := actionAreaAt(fields, i)
			if !valid {
				return "offense:invalid-envelope-or-arguments"
			}
			row.OffensiveArea = area
		default:
			return "offense:instruction:" + strconv.FormatInt(tag, 16)
		}
		if i+arity >= len(fields) {
			return "offense:invalid-envelope-or-arguments"
		}
		for _, field := range fields[i+1 : i+arity+1] {
			if _, ok := textdataInt(field); !ok {
				return "offense:invalid-envelope-or-arguments"
			}
		}
		i += arity + 1
	}
	return "offense:unterminated-program"
}
