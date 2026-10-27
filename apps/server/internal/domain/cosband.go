/*
===========================================================================

cosband.go - the pet families every companion owner tells apart

A COS reference's band is its TypeID4 (TidWord >> 11). The mercenary band
lives with the mercenary rules (mercenary.go).

===========================================================================
*/
package domain

const (
	// GrowthPetBand is the attack (growth) pet family.
	GrowthPetBand = 3
	// PickupPetBand is the grab (pickup) pet family, as the v1.188
	// research server's pickup-pet test classifies it.
	PickupPetBand = 4
)
