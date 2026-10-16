/*
===========================================================================

pettraining.go - the attack-pet tree's state push (port-only, not native)

The attack-pet expansion (SRO_ATTACK_PET_EXPANSION) shows a pet's skills,
row cooldowns and worn gear in the pet window. 0x3FB1 is a port-only
opcode: no v1.150 client or v1.188 server uses it, and only this port's
browser client consumes it. The server sends it on the owner's request and
after every training or gear change; every rule is decided by the server.

Body, little-endian:

	u32 petGid
	u8  freePoints
	u16 attackPercent, u16 defensePercent
	2 x { u32 timerMs, u16 eventsLeft, u8 kindLength, kind bytes }   active rows
	3 x u8 slotted option                                          passive rows
	u8  skillCount
	skillCount x { u8 id, u8 level, u8 effectiveLevel, u8 nextPetLevel,
	               u8 flags (1 action, 2 autocast, 4 learnable, 8 live, 16 slotted),
	               u16 value, u16 nextValue }
	u8  gearCount
	gearCount x { u32 refObjId (0 empty), u8 optionCount, optionCount x u64 }

===========================================================================
*/

package wire

import "encoding/binary"

// OpPetTrainingState is the port-only S->C pet training push.
const OpPetTrainingState uint16 = 0x3FB1

// Skill row flags.
const (
	PetSkillAction    uint8 = 1
	PetSkillQueued    uint8 = 2
	PetSkillLearnable uint8 = 4
	PetSkillLive      uint8 = 8
	PetSkillSlotted   uint8 = 16
)

// PetGearRefFirst and PetGearRefLast bound the port pet gear rows
// (scripts/build/shared/portItemRows.mjs). Those ETC items carry their blue
// options on the wire like equipment; the client mirrors the range.
const (
	PetGearRefFirst = 30100
	PetGearRefLast  = 30399
)

/*
================
IsPetGearRef
================
*/
func IsPetGearRef(refObjID uint32) bool {
	return refObjID >= PetGearRefFirst && refObjID <= PetGearRefLast
}

/*
================
PetRowState / PetSkillRow / PetGearState / PetTrainingState
================
*/
type PetRowState struct {
	TimerMs uint32
	Left    uint16
	Kind    string
}

type PetSkillRow struct {
	ID, Level, EffectiveLevel, NextPetLevel, Flags uint8
	Value, NextValue                               uint16
}

type PetGearState struct {
	RefObjID uint32
	Options  []uint64
}

type PetTrainingState struct {
	PetGID                            uint32
	FreePoints                        uint8
	AttackPercent, DefensePercent     uint16
	Rows                              [2]PetRowState
	Slotted                           [3]uint8
	Skills                            []PetSkillRow
	Gear                              []PetGearState
	MaxHP                             uint32
	MagicPercent, MagicDefensePercent uint16
	MagicPrimary                      bool
	GearStats                         [6]uint32
	CooldownPercent                   uint16
}

/*
================
EncodePetTrainingState
================
*/
func EncodePetTrainingState(s PetTrainingState) []byte {
	out := binary.LittleEndian.AppendUint32(nil, s.PetGID)
	out = append(out, s.FreePoints)
	out = binary.LittleEndian.AppendUint16(out, s.AttackPercent)
	out = binary.LittleEndian.AppendUint16(out, s.DefensePercent)
	for _, r := range s.Rows {
		out = binary.LittleEndian.AppendUint32(out, r.TimerMs)
		out = binary.LittleEndian.AppendUint16(out, r.Left)
		kind := r.Kind
		if len(kind) > 32 {
			kind = kind[:32]
		}
		out = append(out, uint8(len(kind)))
		out = append(out, kind...)
	}
	out = append(out, s.Slotted[:]...)
	out = append(out, uint8(len(s.Skills)))
	for _, row := range s.Skills {
		out = append(out, row.ID, row.Level, row.EffectiveLevel, row.NextPetLevel, row.Flags)
		out = binary.LittleEndian.AppendUint16(out, row.Value)
		out = binary.LittleEndian.AppendUint16(out, row.NextValue)
	}
	out = append(out, uint8(len(s.Gear)))
	for _, g := range s.Gear {
		out = binary.LittleEndian.AppendUint32(out, g.RefObjID)
		out = append(out, uint8(len(g.Options)))
		for _, o := range g.Options {
			out = binary.LittleEndian.AppendUint64(out, o)
		}
	}
	if s.MaxHP != 0 {
		out = append(out, 2) // combat profile plus flat equipment stats and cooldown reduction
		out = binary.LittleEndian.AppendUint32(out, s.MaxHP)
		out = binary.LittleEndian.AppendUint16(out, s.MagicPercent)
		out = binary.LittleEndian.AppendUint16(out, s.MagicDefensePercent)
		if s.MagicPrimary {
			out = append(out, 1)
		} else {
			out = append(out, 0)
		}
		for _, value := range s.GearStats {
			out = binary.LittleEndian.AppendUint32(out, value)
		}
		out = binary.LittleEndian.AppendUint16(out, s.CooldownPercent)
	}
	return out
}
