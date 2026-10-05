/*
===========================================================================

structure_attack.go - who may strike a fortress structure, and when

CGObjPC_CanAttackTarget (52BF90) answers for a player attacking a
CICATStruct (TypeID band 0x2C6, CGObj_IsATStruct 482AB0):

  - outside a fortress war (MainProcess_IsSiegeWarActive 62EB50) never: 0x3041
  - with anything but the weapon's basic attack: 0x3006
  - a headquarters (TID4 5) of the attacker's own guild: 0x303E
  - another structure of the guild that holds its fortress: 0x303F

52BF90 also refuses a gate whose state word (+0x44) is set, 0x3042: the
gate pulley opens it. No pulley is ported, so every gate stays shut and
that test always admits.

and for the fort stone, while guard towers stand 0x3040 and during the
countdown after the last falls 0x3046 (the fortress authority's
capture.go).

INFERENCE: the guild-owner tests read the attacker's side through
CGObjPC_GetFortressOrArenaContext, so the owner's allies are refused the
owner's structures as its members are (fortress_allies.go).

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
)

const (
	structureRefusedOutsideWar   uint16 = 0x3041
	structureRefusedSkill        uint16 = 0x3006
	structureRefusedOwnHQ        uint16 = 0x303e
	structureRefusedOwnStructure uint16 = 0x303f

	// TypeID4 of the CICATStruct band (CGObj_IsFortStone 482AF0, gate
	// 482B90, headquarters 482C30).
	structureKindGate         uint8 = 3
	structureKindHeadquarters uint8 = 5
)

/*
================
structureAttackRefusal

The 52BF90 structure branch for skill `skillID` aimed at target; 0
admits. A structure in a world no fortress owns keeps only the war and
basic-attack tests, as 52BF90 skips the owner tests without a record;
the fort stone answers to its capture guards first.
================
*/
func (rt *Runtime) structureAttackRefusal(division string, attacker *enterworld.Character, target monster.Instance, skillID uint32, nowMs int64) uint16 {
	if !rt.Fortresses.WarActive(division) {
		return structureRefusedOutsideWar
	}
	basic, _, why := rt.resolveBasicAttack(attacker)
	if why != "" || basic.ID != skillID {
		return structureRefusedSkill
	}
	record, _, ok := rt.structureFortress(division, target)
	if !ok {
		return 0
	}
	if target.Ref.TypeID4 == structureKindFortStone {
		if code := rt.Fortresses.StoneRefusal(division, record.ID, nowMs); code != 0 {
			return code
		}
	}
	// CGObjSiegeStruct +0x24: the fortress's structures belong to its
	// holder; headquarters are placed by an attacking guild and carry their
	// own (none are placed yet).
	owner := record.Holder()
	if target.Ref.TypeID4 == structureKindHeadquarters || owner == 0 ||
		attacker.GuildID == nil || !rt.fortressDefender(division, record, *attacker.GuildID) {
		return 0
	}
	switch target.Ref.TypeID4 {
	case structureKindHeadquarters:
		return structureRefusedOwnHQ
	case structureKindGate:
		return 0
	}
	return structureRefusedOwnStructure
}
