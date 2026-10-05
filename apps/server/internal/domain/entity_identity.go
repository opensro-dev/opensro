/*
===========================================================================

entity_identity.go - disjoint server-owned world object identities

Explicit ceilings prevent monotonic counters from crossing into a sister
owner's range. Dynamic skill objects live above the existing COS band and
never use the FFFFFFFF reference sentinel as an object identity.

===========================================================================
*/
package domain

const (
	PlayerGIDBase       uint32 = 100000
	NPCGIDBase          uint32 = 200000
	GroundItemGIDBase   uint32 = 300000
	GroundItemGIDLimit  uint32 = 399999
	MonsterGIDBase      uint32 = 400000
	MonsterGIDLimit     uint32 = 0x00BFFFFF
	COSGIDBase          uint32 = 0x00C00000
	COSGIDLimit         uint32 = 0x00FFFFFF
	SkillObjectGIDBase  uint32 = 0x01000000
	SkillObjectGIDLimit uint32 = 0x01ffffff
	AttackPetGIDBase    uint32 = 0x02000000
	PickupPetGIDBase    uint32 = 0x02400000
	// One captured quest monster per owner (capture-escort quests).
	CapturedCOSGIDBase uint32 = 0x02800000
)

// CapturedCOSBand is the captured quest monster's COS band (TypeID 4 = 6).
const CapturedCOSBand = 6

const (
	MaxGroundItemGIDCounter = GroundItemGIDLimit - GroundItemGIDBase
	MaxMonsterGIDCounter    = MonsterGIDLimit - MonsterGIDBase
	MaxCOSOwnerID           = COSGIDLimit - COSGIDBase
)
