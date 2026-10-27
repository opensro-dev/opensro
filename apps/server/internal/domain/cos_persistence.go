/*
===========================================================================

cos_persistence.go - item-owned companion records and detached copies

A summoner item owns its companion statistics even while no actor exists.
Inventory, warehouse and ground records carry that same value graph; live
actor identity is a projection and never substitutes for item ownership.

===========================================================================
*/
package domain

/*
================
COSRental

The server's rental writer writes kind 0 or 5; kind 5 additionally carries a
tag and flag. Absolute deadlines survive offline time. RemainingSeconds is
the native projection cached at the last authority checkpoint.
================
*/
type COSRental struct {
	Kind             uint8  `json:"kind"`
	ID               uint32 `json:"id"`
	RemainingSeconds int32  `json:"remainingSeconds"`
	ExpiresAtUnix    int64  `json:"expiresAtUnix,omitempty"`
	Tag              uint32 `json:"tag,omitempty"`
	Flag             uint8  `json:"flag,omitempty"`
}

/*
================
CloneCOS

Detach every mutable child so inventory planning cannot mutate a live pet
before the owning character transaction commits.
================
*/
func CloneCOS(source *CharacterCOS) *CharacterCOS {
	if source == nil {
		return nil
	}
	copy := *source
	copy.Rentals = cloneSlice(source.Rentals)
	if source.Container != nil {
		bag := *source.Container
		bag.Rows = cloneInventoryRows(source.Container.Rows)
		copy.Container = &bag
	}
	return &copy
}

/*
================
COSItemState

The server derives the item state from the retained record's alive/summoned bits.
State 1 means the item has never acquired a record, not a dormant companion.
================
*/
func COSItemState(pet *CharacterCOS) uint8 {
	if pet == nil {
		return 1
	}
	if pet.StateFlags&1 == 0 {
		return 4
	}
	if pet.Summoned {
		return 2
	}
	return 3
}

/*
================
Companions

The transport/legacy record and every summoner item have one canonical owner.
Dormant records are included so migration, cancellation and revival need not
manufacture a live actor to find durable companion state.
================
*/
func (c *Character) Companions() []*CharacterCOS {
	if c == nil {
		return nil
	}
	var pets []*CharacterCOS
	c.visitCompanions(func(pet *CharacterCOS) bool {
		pets = append(pets, pet)
		return true
	})
	return pets
}

/*
================
visitCompanions

Visits the records Companions returns, in the same order, without building
the slice; visit returns false to stop. Lookups by GID run on every pet,
combat and abnormal tick for every character a division walk touches, and
the slice cost an allocation per call plus a walk of the whole bag (#1820).
The order is the one canonical order: Companions is built from it.
================
*/
func (c *Character) visitCompanions(visit func(*CharacterCOS) bool) {
	if c == nil {
		return
	}
	if c.ActiveCOS != nil && !visit(c.ActiveCOS) {
		return
	}
	for i := range c.MissionInventory {
		if pet := c.MissionInventory[i].Summon; pet != nil && !visit(pet) {
			return
		}
	}
	for _, soldier := range c.Mercenaries {
		if soldier != nil && !visit(soldier) {
			return
		}
	}
	if c.CapturedCOS != nil && !visit(c.CapturedCOS) {
		return
	}
	for _, pet := range c.Brood {
		if !visit(pet) {
			return
		}
	}
}

/*
================
CompanionByGID

Duplicate identities cannot select the first record and silently authorize a
second companion. Dormant records retain data but do not own a world GID.
================
*/
func (c *Character) CompanionByGID(gid uint32) *CharacterCOS {
	if gid == 0 {
		return nil
	}
	var found *CharacterCOS
	duplicate := false
	c.visitCompanions(func(pet *CharacterCOS) bool {
		if pet.Summoned && pet.GID == gid {
			if found != nil {
				duplicate = true
				return false
			}
			found = pet
		}
		return true
	})
	if duplicate {
		return nil
	}
	return found
}

/*
================
RefreshRentalTimes

The native rental writer clamps calendar-time differences to a signed wire dword. The
clock belongs to the authority caller, so offline time cannot pause leases.
================
*/
func (pet *CharacterCOS) RefreshRentalTimes(nowUnix int64) bool {
	if pet == nil {
		return false
	}
	remaining := cosRentalSeconds(pet.RentalExpiresAtUnix, nowUnix)
	changed := remaining != pet.RentalRemainingSeconds
	pet.RentalRemainingSeconds = remaining
	for i := range pet.Rentals {
		remaining = cosRentalSeconds(pet.Rentals[i].ExpiresAtUnix, nowUnix)
		changed = changed || remaining != pet.Rentals[i].RemainingSeconds
		pet.Rentals[i].RemainingSeconds = remaining
	}
	if pet.Container != nil {
		for i := range pet.Container.Rows {
			changed = pet.Container.Rows[i].Summon.RefreshRentalTimes(nowUnix) || changed
		}
	}
	return changed
}

/*
================
cosRentalSeconds
================
*/
func cosRentalSeconds(deadline, now int64) int32 {
	const maximum = int64(1<<31 - 1)
	if deadline <= now {
		return 0
	}
	if now < 0 || deadline-now > maximum {
		return int32(maximum)
	}
	return int32(deadline - now)
}
