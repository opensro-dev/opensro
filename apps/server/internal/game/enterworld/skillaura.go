/*
===========================================================================

skillaura.go - persistent area skills and the buffs they hand out

The aura owner is action/skillparty.go; the buff installer is
action/activeeffect.go.

===========================================================================
*/

package enterworld

/*
==================
SkillAura

A persistent area action (CastLifecycle_ProcessPersistent 5830B0): efr kind 2
at +0x290 (587C1F) and the onff pulse at +0x284 (588C6C).

The caster's own persistent buff is the aura. Party members join and leave
as child buffs, and receive the row's SkillBuffModifiers. Every PulseMs the
caster pays PulseMP, cut by BDMD (585262), or the aura retires.
==================
*/
type SkillAura struct {
	Present    bool
	Radius     uint32 // efr +0x08
	Select     uint32 // efr +0x14; bit 0 puts the caster in the set
	MaxTargets uint32 // efr +0x0C; the join walk never reads it
	PulseMs    uint32 // onff word 0
	PulseMP    uint32 // onff word 1
	Eshp       bool   // +0x298: heal the lowest HP ratio each update (5A09F0)
}

/*
==================
SkillBuffModifiers

The parameter blocks CSkillManager_ApplyBuffModifiersToActor (594AC0) installs
for any buff. Both words of each block are read unsigned (fild plus the 2^32
fix-up).

	dru  +0x3E4  595A97  word 0 flat on 0x80 and 0x81, word 1 on 0x82 and 0x83
	odar +0x270  596004  -word 1 as a percent product, per bit pair:
	                     4+1 on 0xAE, 4+2 on 0xAF, 8+1 on 0xB0, 8+2 on 0xB1

The odar parser (588C3B / 588C52) ORs 1|2 into kinds 4, 8 and 0xC and 4|8
into kinds 1, 2 and 3; other kinds keep their own bits.

	ru   +0x250  5958E7  MP recovery rate modifier 0x21
	hr   +0x24C  595825  hit rate 0xB: word 1 on the percent-sum channel,
	                     then word 0 flat (595825..595875)

Two more blocks are parameter writes of 594AC0 (action.buffModifierWrites):

	rhru         word 0 -> parameter 0xAA, word 1 -> 0xAB, flat (0x5962C1):
	             the healing-received scale every heal applies
	             (Dancing of Healing / Vitality)
	dcmp         -word -> parameter 0x8D, flat (0x5963F7): the MP
	             consumption rate player costs are scaled by
	             (Dancing of Mana)

==================
*/
type SkillBuffModifiers struct {
	Dru         bool
	DruWords    [2]uint32
	Odar        bool
	OdarBits    uint32 // after the parser's fix-up
	OdarWord    uint32
	Ru          bool
	RuRate      uint32
	Hr          bool
	HrFlat      uint32
	HrRate      uint32
	Rhru        bool
	RhruWords   [2]uint32
	Dcmp        bool
	DcmpPercent uint32
}

// Present reports a block 594AC0 would install.
func (m SkillBuffModifiers) Present() bool {
	return m.Dru || m.Odar || m.Ru || m.Hr || m.Rhru || m.Dcmp
}

/*
==================
AuraFamily

The two kinds of Bard area aura the owner's rules tell apart, read from the
row's data, never its codename:

	instrument  efr kind 2 + onff + scls bit 0 (the music a dance needs):
	            Guard and Mana Tambour, Hit and Clout March
	dance       efr kind 2 + onff + reqc bit 5 (needs that music):
	            the seven Dancings

Moving and Swing March are timed efr kind 1 buffs (dura + hste), not auras,
and stay AuraFamilyNone: they coexist with both families. The Cleric's eshp
aura has neither bit and is AuraFamilyNone too.
==================
*/
type AuraFamily uint8

const (
	AuraFamilyNone AuraFamily = iota
	AuraFamilyInstrument
	AuraFamilyDance
)

// selectorMusic is scls bit 0, the skill-manager selector a reqc bit 5 row
// asks for (59DDF0).
const selectorMusic = 1

/*
==================
AuraFamily

The family of r's persistent area aura, AuraFamilyNone for every other row.
==================
*/
func (r SkillRow) AuraFamily() AuraFamily {
	if !r.Aura.Present || r.Aura.PulseMs == 0 {
		return AuraFamilyNone
	}
	if r.Reqc.Dance {
		return AuraFamilyDance
	}
	if r.SelectorMask&selectorMusic != 0 {
		return AuraFamilyInstrument
	}
	return AuraFamilyNone
}
