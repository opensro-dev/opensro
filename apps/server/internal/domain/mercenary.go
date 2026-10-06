/*
===========================================================================

mercenary.go - character-owned guild soldiers and their summon authority

The scroll is consumed once for a group. Each soldier has a distinct world
identity; dismissing a group never removes the owner's timed-job cooldown.

===========================================================================
*/
package domain

const (
	MercenaryBand                 = 5
	MaxMercenaries                = 10
	MercenaryGIDBase       uint32 = 0x03000000
	MercenarySummonSeconds int64  = 1200
)

/*
================
MercenaryContext

The guild and union read under the same store lock as scroll consumption.
================
*/
type MercenaryContext struct {
	Guild       GuildRecord
	Master      bool
	UnionMaster bool
}

/*
================
MercenaryStore
================
*/
type MercenaryStore interface {
	UpdateMercenaryOwner(division string, character *Character, update func(MercenaryContext) bool) bool
	PurchaseMercenaryAttribute(division string, actorID int64, attribute uint8) (GuildSnapshot, uint8)
}

/*
================
MercenaryCount

5C56C0 indexes ADE900/ADE93C by guild level. Only the leading guild of
an existing union uses the second table.
================
*/
func MercenaryCount(level uint8, unionMaster bool) int {
	if level < 3 || level > 5 {
		return 0
	}
	if unionMaster {
		return [...]int{1, 5, 10}[level-3]
	}
	return [...]int{1, 3, 6}[level-3]
}

/*
================
MercenaryObjectID

Disjoint owner/slot allocation, including slots vacated by a dead soldier.
The native object manager also assigns an independent ID to every actor.
================
*/
func MercenaryObjectID(ownerID int64, slot int) (uint32, bool) {
	if ownerID <= 0 || uint64(ownerID) > uint64(MaxCOSOwnerID) || slot < 0 || slot >= MaxMercenaries {
		return 0, false
	}
	return MercenaryGIDBase + uint32(ownerID)*MaxMercenaries + uint32(slot), true
}

/*
================
OwnsMercenaryID
================
*/
func (c *Character) OwnsMercenaryID(gid uint32) bool {
	if c == nil {
		return false
	}
	first, valid := MercenaryObjectID(c.ID, 0)
	return valid && gid >= first && gid-first < MaxMercenaries
}

/*
================
RemoveMercenary

Erase one durable soldier without renumbering surviving actor identities.
================
*/
func (c *Character) RemoveMercenary(gid uint32) bool {
	for i, pet := range c.Mercenaries {
		if pet != nil && pet.GID == gid {
			kept := make([]*CharacterCOS, 0, len(c.Mercenaries)-1)
			kept = append(kept, c.Mercenaries[:i]...)
			kept = append(kept, c.Mercenaries[i+1:]...)
			c.Mercenaries = kept
			return true
		}
	}
	return false
}

/*
================
MercenaryAttributePrice

5D0E00 uses the same gold fee for an attribute and reset, but less GP for
reset. The native fallback for an unknown byte is 100 million of each.
================
*/
func MercenaryAttributePrice(attribute uint8) (gold int64, gp uint32) {
	switch attribute {
	case 0:
		return 500000, 10000
	case 1, 2, 4, 8, 16:
		return 500000, 50000
	default:
		return 100000000, 100000000
	}
}
