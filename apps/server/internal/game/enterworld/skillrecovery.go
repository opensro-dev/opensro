/*
===========================================================================

skillrecovery.go - admitting self heals and party-area recovery casts

A recovery row is admitted only when its whole program is understood:
resu/puls/dura/mwhh/efr/getv cannot silently disappear from a cast. Four
shapes qualify: a flat self heal, a party-area heal, a party-area
resurrection and a heal over time. Amounts and recipients are computed by
action/skillheal.go, action/skillcure.go, action/resurrection.go and
action/skillhealtime.go.

===========================================================================
*/

package enterworld

const (
	// Program tags, little-endian as stored in the numeric skilldata cells.
	recoveryTagHeal = 0x6865616c // heal +0x324
	recoveryTagMwhh = 0x6d776868 // mwhh +0x328
	recoveryTagMwmh = 0x6d776d68 // mwmh +0x32C
	recoveryTagResu = 0x72657375 // resu +0x330
	recoveryTagDura = 0x64757261 // dura: the effect's lifetime
	recoveryTagPuls = 0x70756c73 // puls +0x384: the period

	// recoveryEfrActionArea and recoveryEfrAroundCaster are efr words 0 and
	// 1: the action-area slot and a caster-centred area.
	recoveryEfrActionArea   = 1
	recoveryEfrAroundCaster = 1

	// recoveryPartySelect and recoveryPartySelectWithCaster are the efr
	// +0x14 masks skillCureVector maps to TargetSelection_Party (58BEF0);
	// bit 0 pushes the caster first.
	recoveryPartySelect           = 4
	recoveryPartySelectWithCaster = 5

	// recoveryPartyMemberBound is the native party roster bound (eight
	// slots, social/party PartyMaxMembers). An efr target cap at or above
	// it can never cut a party selection, so admission does not depend on
	// how 58BEF0 would apply a smaller cap.
	recoveryPartyMemberBound = 8

	// recoveryInstantHandler is column 68 (RefSkill action handler) of an
	// instant action. Healing Orbit carries 3 (a timed handler) and must
	// not be reduced to a one-shot heal.
	recoveryInstantHandler = "0"

	// recoveryTimedHandler is column 68 of a timed action (5830B0): the
	// heals over time (Mana Cycle, Healing Cycle and their Orbits).
	recoveryTimedHandler = "3"
)

/*
==================
SkillRecovery

SelfFlatPinned admits a self heal whose whole program is one flat heal
block (SkillHeal): no percent words, no weapon term, nothing after it.

PartyHealPinned admits an untargeted instant heal whose program is exactly
efr[1,1,radius,cap,0,4|5] heal[...] with optional mwhh / mwmh weapon terms
and getv HLRU / HLMD: every party member the selection returns is healed by
5A0850. Healing Orbit (dura/puls) and Group Reverse (resu) never match.

PartyResurrectPinned admits an untargeted efr[1,1,radius,cap,0,4|5]
heal[...] resu[...] program: each dead party member in range is proposed a
revival, the heal block being the revival vitals (594780).

HealOverTimePinned admits a timed heal (handler 3) whose program is
[efr[1,1,radius,cap,0,4|5]] dura[ms] puls[ms] heal[...] with the party
heal's optional weapon terms and getv words: each recipient holds the
row's effect for dura and is healed every PulseMs. A targeted row (Mana
Cycle, Healing Cycle) has no efr and heals its target; an untargeted one
(Mana Orbit, Healing Orbit) leads with the party efr and heals the whole
selection.
==================
*/
type SkillRecovery struct {
	SelfFlatPinned       bool
	PartyHealPinned      bool
	PartyResurrectPinned bool
	HealOverTimePinned   bool
	PulseMs              uint32
}

/*
==================
parseSkillRecovery

The envelope every recovery shape shares: an active (column 8 = 2),
untargeted, unlinked row with pinned cost and timing and no periodic,
action-repeat or ground-target columns. A flat HP cost is charged by the
action owner (58E1B6 check, 58312C charge); a percent HP cost stays out.
==================
*/
func parseSkillRecovery(fields []string, row *SkillRow) {
	if parseHealOverTime(fields, row) {
		return
	}
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || row.ChainNext != 0 ||
		row.TargetRequired || !row.Consumption.Pinned || !row.TimingPinned ||
		row.Consumption.HPPercent != 0 {
		return
	}
	// These columns own periodic/action-repeat/ground-target alternatives.
	for _, column := range []int{15, 16, 17, 19, 20, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 56} {
		if fields[column] != "0" {
			return
		}
	}
	lifetime, ok := row.ActionLifecycleMs()
	if !ok || lifetime == 0 {
		return
	}
	if selfFlatRecovery(fields) {
		row.Recovery = SkillRecovery{SelfFlatPinned: true}
		return
	}
	if fields[skilldataColActionHandler] != recoveryInstantHandler {
		return
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return
	}
	switch {
	case partyHealProgram(program):
		row.Recovery = SkillRecovery{PartyHealPinned: true}
	case partyResurrectProgram(program):
		row.Recovery = SkillRecovery{PartyResurrectPinned: true}
	}
}

/*
==================
selfFlatRecovery

5942AB..5942F3 selects the no-target healing branch; 5A0850 applies flat
HP/MP when the percentage words are zero. Requires the entire program to be
exactly heal, followed by zero padding.
==================
*/
func selfFlatRecovery(fields []string) bool {
	if fields[69] != "1751474540" {
		return false
	}
	var values [4]uint32
	for i := range values {
		n, valid := textdataInt(fields[70+i])
		if !valid || n < 0 || n > 0x7fffffff {
			return false
		}
		values[i] = uint32(n)
	}
	for _, field := range fields[74:] {
		if field != "0" {
			return false
		}
	}
	return values[1] == 0 && values[3] == 0 && (values[0] != 0 || values[2] != 0)
}

/*
==================
partyRecoveryArea

The leading efr block of a party-area recovery: the caster-centred action
area with no secondary reduction, a party selection and a cap no party can
exceed.
==================
*/
func partyRecoveryArea(op SkillInstruction) bool {
	a := op.Arguments
	return op.Tag == tagEfr && op.Count == 6 &&
		a[0] == recoveryEfrActionArea && a[1] == recoveryEfrAroundCaster && a[2] != 0 &&
		a[3] >= recoveryPartyMemberBound && a[4] == 0 &&
		(a[5] == recoveryPartySelect || a[5] == recoveryPartySelectWithCaster)
}

/*
==================
recoveryHealBlock

A heal block with valid signed words that heals something.
==================
*/
func recoveryHealBlock(op SkillInstruction) bool {
	if op.Tag != recoveryTagHeal || op.Count != 4 {
		return false
	}
	for _, word := range op.Arguments[:4] {
		if word > 0x7fffffff {
			return false
		}
	}
	return op.Arguments[0] != 0 || op.Arguments[1] != 0 || op.Arguments[2] != 0 || op.Arguments[3] != 0
}

/*
==================
partyHealProgram

efr heal [mwhh] [mwmh] [getv HLRU] [getv HLMD] and nothing else. mwhh and
mwmh add the caster's weapon term to the HP and MP amounts (411080,
action/skillheal.go: Mana Breeze is heal(0,0,mp,0) mwmh), HLRU raises the
percent words (59425E) and HLMD cuts the prepared MP cost
(combat.ApplyMPDecrease); any other instruction refuses the row.
==================
*/
func partyHealProgram(program SkillProgram) bool {
	if program.Len() < 2 || !partyRecoveryArea(program.Instruction(0)) ||
		!recoveryHealBlock(program.Instruction(1)) {
		return false
	}
	return healProgramTail(program, 2)
}

/*
==================
healProgramTail

The instructions a heal block may carry after it, from index first to the
end: [mwhh] [mwmh] [getv HLRU] [getv HLMD], each at most once.
==================
*/
func healProgramTail(program SkillProgram, first int) bool {
	var weaponHP, weaponMP, recoveryUp, mpDecrease bool
	for i := first; i < program.Len(); i++ {
		op := program.Instruction(i)
		parameter, known := SkillParameterFromKey(op.Arguments[0])
		getv := op.Tag == tagGetv && op.Count == 1 && known
		switch {
		case op.Tag == recoveryTagMwhh && op.Count == 1 && !weaponHP:
			weaponHP = true
		case op.Tag == recoveryTagMwmh && op.Count == 1 && !weaponMP:
			weaponMP = true
		case getv && parameter == ParameterHealRecoveryUp && !recoveryUp:
			recoveryUp = true
		case getv && parameter == ParameterHealerMPDecrease && !mpDecrease:
			mpDecrease = true
		default:
			return false
		}
	}
	return true
}

/*
==================
parseHealOverTime

The heal-over-time envelope: an active, unlinked row with pinned cost and
timing, the timed handler, and no periodic, action-repeat or ground-target
columns. Unlike the instant shapes it may be targeted; a targeted row
keeps its target columns (22, 23, 26..28: required, animal, self, ally,
party) and must not lead with an efr, an untargeted one must lead with the
party efr. Returns whether the row was admitted.
==================
*/
func parseHealOverTime(fields []string, row *SkillRow) bool {
	if len(fields) != 118 || fields[0] != "1" || fields[8] != "2" || row.ChainNext != 0 ||
		!row.Consumption.Pinned || !row.TimingPinned || row.Consumption.HPPercent != 0 ||
		fields[skilldataColActionHandler] != recoveryTimedHandler {
		return false
	}
	columns := []int{15, 16, 17, 19, 20, 24, 25, 29, 30, 31, 32, 33, 56}
	if !row.TargetRequired {
		columns = append(columns, 22, 23, 26, 27, 28)
	}
	for _, column := range columns {
		if fields[column] != "0" {
			return false
		}
	}
	if lifetime, ok := row.ActionLifecycleMs(); !ok || lifetime == 0 {
		return false
	}
	program, err := CompileSkillProgram(fields)
	if err != nil {
		return false
	}
	first := 0
	if !row.TargetRequired {
		if program.Len() == 0 || !partyRecoveryArea(program.Instruction(0)) {
			return false
		}
		first = 1
	}
	if program.Len() < first+3 {
		return false
	}
	dura, puls := program.Instruction(first), program.Instruction(first+1)
	if dura.Tag != recoveryTagDura || dura.Count != 1 || puls.Tag != recoveryTagPuls || puls.Count != 1 ||
		puls.Arguments[0] == 0 || dura.Arguments[0] < puls.Arguments[0] || dura.Arguments[0] != row.EffectDurationMs ||
		!recoveryHealBlock(program.Instruction(first+2)) || !healProgramTail(program, first+3) {
		return false
	}
	row.Recovery = SkillRecovery{HealOverTimePinned: true, PulseMs: puls.Arguments[0]}
	return true
}

/*
==================
partyResurrectProgram

Exactly efr heal resu. The resu block is what admits dead members into the
party selection (58BEF0 skips its alive check).
==================
*/
func partyResurrectProgram(program SkillProgram) bool {
	if program.Len() != 3 || !partyRecoveryArea(program.Instruction(0)) ||
		!recoveryHealBlock(program.Instruction(1)) {
		return false
	}
	resu := program.Instruction(2)
	return resu.Tag == recoveryTagResu && resu.Count == 2
}
