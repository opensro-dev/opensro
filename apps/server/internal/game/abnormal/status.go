/*
===========================================================================

status.go - the abnormal status table and its parameter sources

Package abnormal ports the native abnormal-state engine: the per-hit
status roll, the 32-slot owner block, status admission, the periodic
update and the per-status callbacks. It has no world dependencies:
owners adapt it to monsters and players through the Owner interface.

===========================================================================
*/
package abnormal

// Status is the native slot index of a status-effect record.
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

// SlotCount is the owner block's fixed slot count.
const SlotCount = 32

// Bit is the native status-mask bit: identity except slots 2 and 3,
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

// Category is the status record's category field: 1 for the level-carrying
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

// GradeMask selects the bits whose grade byte follows the v1.150 client's
// vitals mask (017FCFC0); dark carries none on this wire.
const GradeMask uint32 = 0x017fcfc0

/*
==================
Source

Source is one abnormal parameter block of the skill record, in native
field order. Key is the per-actor probability key the roll uses, Resist
the status resistance index of statuses 6 and above.
==================
*/
type Source struct {
	Tag    uint32
	Status Status
	Key    uint32
	Resist int8
	Arity  uint8
}

// Sources lists the 23 parameter blocks in the order the native roll
// evaluates them.
// The 23 blocks come from two field groups of the skill record; one
// field between them (stns) is not a status.
var Sources = [...]Source{
	{0x667a, Freeze, 0x01000000, -1, 2},
	{0x6662, Frostbite, 0x02000000, -1, 2},
	{0x6573, ElectricShock, 0x03000000, -1, 3},
	{0x6275, Burn, 0x04000000, -1, 3}, // one caster key is shared across victims and imbues
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

// Param is one parsed abnormal block of the skill record.
/*
================
Param
================
*/
type Param struct {
	Present bool
	Args    [6]uint32
}

// Caster modifier keys the status roll reads.
const (
	KeyPoisonDamage   uint32 = 0x52504455 // RPDU
	KeyPoisonDuration uint32 = 0x52505455 // RPTU
	KeyTrapDamage     uint32 = 0x54524141 // TRAA
)

// SkillParams is the skill's complete abnormal authority.
/*
================
SkillParams
================
*/
type SkillParams struct {
	Params [SourceCount]Param
	// Pulse is the 'puls' field: the tick period copied into
	// periodic statuses, 2000 ms when absent.
	Pulse        uint32
	PulsePresent bool
	// Getv records which caster modifiers the program requests
	// (RPDU, RPTU, TRAA).
	PoisonDamageGetv, PoisonDurationGetv, TrapDamageGetv bool
	// Curt is the 'curt' block: one mask and one level for every
	// matching slot. Curl is the 'curl' block: the pill's mask, chance
	// and grade. Rcur is the 'rcur' block: a present limit, zero included.
	CurtMask                        uint32
	CurtLevel                       uint16
	Curt                            bool
	CurlMask, CurlChance, CurlGrade int32
	Curl                            bool
	Rcur                            int32
	RcurSet                         bool
	// EffectArea is the kind-1 efr (0x656672) block of the skill record.
	// Select 4 or 5 forces the party selector unless the shape is already 5 or 6.
	EffectArea EffectArea
	// AdmitDeadParty is the resu tag (0x72657375) of the skill record.
	// The party target selection skips the alive check while it is set.
	// Word 0 is the highest level it revives (none when 0); word 1 is
	// the percent of the last EXP loss it returns.
	AdmitDeadParty bool
	ResuMaxLevel   uint32
	ResuExpPercent uint32
	// Rmut is the rmut tag (0x726D7574) of the skill record: the skill a
	// resurrected player starts on accepting.
	Rmut uint32
}

/*
==================
EffectArea

EffectArea is the efr argument block (kind 1, the action area): shape,
radius, max targets, reduction and select (bit 0 includes the caster in
party selection).
==================
*/
type EffectArea struct {
	Present                         bool
	Kind, Shape, Radius, MaxTargets uint32
	Reduction, Select               uint32
}

// CurePresent reports a curt or curl block. Both absent skips the cure pass.
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

// Stun reports the stun block's presence. Its presence bypasses the
// magical-wall exclusion for other statuses; a wall still excludes Stun
// itself.
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
