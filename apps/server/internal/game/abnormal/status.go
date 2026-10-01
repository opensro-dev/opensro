/*
===========================================================================

status.go - the abnormal status table and its parameter sources

Package abnormal ports the native abnormal-state engine of SR_GameServer
(v1.188 research executable): the per-hit roll SkillCombat_RollAbnormalStatus
(590680), the 32-slot owner block initialised by GCharAbnormalState_Initialize
(4A40C0), admission CGObjChar_ApplyAbnormalStateRecord (4A4270), the update
CGObjChar_UpdateAbnormalStates (4A4390) and the per-status callbacks at
block+0xE0C. It has no world dependencies: owners adapt it to monsters and
players through the Owner interface.

===========================================================================
*/
package abnormal

// Status is the native slot index stored at tagSkillStatusEffect+05.
/*
================
Status
================
*/
type Status uint8

const (
	Freeze Status = iota
	Frostbite
	Burn
	ElectricShock
	Poison
	Zombie
	Sleep
	Root
	Slow
	Fear
	Myopia
	Bleeding
	unused12
	Dark
	Stun
	Disease
	Confusion
	Decay
	Weaken
	Impotent
	Division
	Panic
	Combustion
	unused23
	TimeBomb
)

// SlotCount is the block's fixed slot array (4A40C0 initialises 0x20 slots).
const SlotCount = 32

// Bit is g_adwAbnormalStatusBit (C63EC8): identity except slots 2 and 3,
// which swap so burn is mask bit 3 and electric shock mask bit 2.
/*
================
Bit
================
*/
func (s Status) Bit() uint32 {
	switch s {
	case Burn:
		return 8
	case ElectricShock:
		return 4
	}
	return 1 << s
}

// Category is tagSkillStatusEffect+06 (5AA450): 1 for the level-carrying
// element statuses, 2 for grade-carrying statuses, 0 otherwise.
/*
================
Category
================
*/
func (s Status) Category() uint8 {
	switch bit := s.Bit(); {
	case bit&0x3f != 0:
		return 1
	case bit&0x17fefc0 != 0:
		return 2
	}
	return 0
}

// GradeMask selects the bits whose grade byte follows the v1.150 vitals mask
// (client 77A080 reads 017FCFC0; dark carries none on this wire).
const GradeMask uint32 = 0x017fcfc0

/*
==================
Source

Source is one tagRefSkill abnormal parameter block in native field order.
Key is the per-actor probability key the roll passes in ECX (the field
ordinal), Resist the CSkillManager status-resistance index of statuses 6+.
==================
*/
type Source struct {
	Tag    uint32
	Status Status
	Key    uint32
	Resist int8
	Arity  uint8
}

// Sources lists the 23 parameter blocks in 590680 evaluation order.
// tagRefSkill offsets: 30C..320 then 430..474 (448 is stns, not a status).
var Sources = [...]Source{
	{0x667a, Freeze, 0x01000000, -1, 2},
	{0x6662, Frostbite, 0x02000000, -1, 2},
	{0x6573, ElectricShock, 0x03000000, -1, 3},
	{0x6275, Burn, 0x04000000, -1, 3}, // 590B24: one caster key shared across victims and imbues
	{0x7073, Poison, 0x05000000, -1, 3},
	{0x7a62, Zombie, 0x06000000, -1, 2},
	{0x7365, Sleep, 0x07000000, 0, 3},
	{0x7274, Root, 0x08000000, 1, 3},
	{0x736c, Slow, 0x09000000, 2, 3},
	{0x6665, Fear, 0x0a000000, 3, 3},
	{0x6d79, Myopia, 0x0b000000, 4, 4},
	{0x626c, Bleeding, 0x0c000000, 5, 5},
	{0x646e, Dark, 0x0e000000, 6, 4},
	{0x7374, Stun, 0x0f000000, 7, 3},
	{0x6473, Disease, 0x11000000, 8, 4},
	{0x6361, Confusion, 0x12000000, 9, 3},
	{0x63737372, Impotent, 0x13000000, 0xc, 4},
	{0x63736974, Division, 0x14000000, 0xd, 4},
	{0x63737064, Decay, 0x15000000, 0xa, 4},
	{0x63736d64, Weaken, 0x16000000, 0xb, 4},
	{0x63736870, Panic, 0x17000000, 0xe, 6},
	{0x63736d70, Combustion, 0x18000000, 0xf, 6},
	{0x7462, TimeBomb, 0x19000000, 0x10, 4},
}

// SourceCount is len(Sources).
const SourceCount = len(Sources)

// SourceIndex maps a skill-parameter tag to its Sources index.
/*
================
SourceIndex
================
*/
func SourceIndex(tag uint32) (int, bool) {
	for i, s := range Sources {
		if s.Tag == tag {
			return i, true
		}
	}
	return 0, false
}

// Param is one parsed abnormal block: the pointer at its tagRefSkill slot.
/*
================
Param
================
*/
type Param struct {
	Present bool
	Args    [6]uint32
}

// Caster getv keys read by 590680 through CSkillManager_GetSkillModifier.
const (
	KeyPoisonDamage   uint32 = 0x52504455 // RPDU, tagRefSkill+500
	KeyPoisonDuration uint32 = 0x52505455 // RPTU, tagRefSkill+504
	KeyTrapDamage     uint32 = 0x54524141 // TRAA, tagRefSkill+53C
)

// SkillParams is the skill's complete abnormal authority.
/*
================
SkillParams
================
*/
type SkillParams struct {
	Params [SourceCount]Param
	// Pulse is 'puls' (tagRefSkill+384): the tick period copied into
	// periodic statuses, 2000 ms when absent.
	Pulse        uint32
	PulsePresent bool
	// Getv records which caster modifiers the program requests
	// (RPDU +500, RPTU +504, TRAA +53C).
	PoisonDamageGetv, PoisonDurationGetv, TrapDamageGetv bool
	// Curt is RefSkill+0x40C (tag curt): one mask and one level for every
	// matching slot. Curl is +0x410 (tag curl): the pill's mask, chance and
	// grade. Rcur is +0x414 (tag rcur): a present limit, including zero.
	CurtMask                        uint32
	CurtLevel                       uint16
	Curt                            bool
	CurlMask, CurlChance, CurlGrade int32
	Curl                            bool
	Rcur                            int32
	RcurSet                         bool
	// EffectArea is the kind-1 efr (0x656672) block at RefSkill+0x28C.
	// Select 4 or 5 forces the party selector unless the shape is already 5 or 6.
	EffectArea EffectArea
	// AdmitDeadParty is the resu tag (0x72657375) stored at RefSkill+0x330.
	// TargetSelection_Party skips the alive check when that pointer is set.
	// Word 0 is the highest level it revives (0x3012 above it, none when
	// 0), word 1 the percent of the last EXP loss it returns.
	AdmitDeadParty bool
	ResuMaxLevel   uint32
	ResuExpPercent uint32
	// Rmut is the rmut tag (0x726D7574, RefSkill+0x4AC): the skill a
	// resurrected player starts on accepting (TrsWaitResponse_OnResponse).
	Rmut uint32
}

/*
==================
EffectArea

EffectArea is the efr argument block at RefSkill+0x28C (kind 1, the action
area). Offsets: +4 shape, +8 radius, +0xC max targets, +0x10 reduction,
+0x14 select (bit 0 includes the caster in party selection).
==================
*/
type EffectArea struct {
	Present                         bool
	Kind, Shape, Radius, MaxTargets uint32
	Reduction, Select               uint32
}

// CurePresent reports a curt or curl block. Both null skips 593F50's cure.
/*
================
CurePresent
================
*/
func (p SkillParams) CurePresent() bool {
	return p.Curt || p.Curl
}

// Present reports whether the skill carries any abnormal block.
/*
================
Present
================
*/
func (p SkillParams) Present() bool {
	for _, param := range p.Params {
		if param.Present {
			return true
		}
	}
	return false
}

// Stun reports tagRefSkill+450. Its presence bypasses the magical-wall
// exclusion for other statuses; a wall still excludes Stun itself (591E7C).
/*
================
Stun
================
*/
func (p SkillParams) Stun() bool {
	i, _ := SourceIndex(0x7374)
	return p.Params[i].Present
}

// Param returns the block for one status.
/*
================
Param
================
*/
func (p SkillParams) Param(status Status) (Param, bool) {
	for i, s := range Sources {
		if s.Status == status {
			return p.Params[i], p.Params[i].Present
		}
	}
	return Param{}, false
}
