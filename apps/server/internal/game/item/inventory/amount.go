package inventory

// ItemAmount is a version-independent authored grant or consumption request.
// The item authority resolves its codename against this world's item catalog.
type ItemAmount struct {
	Codename string
	Count    uint32
	// Plus is the enhancement level of granted equipment, PerfectStats sets
	// every white stat of the item's type to its maximum and PerfectMagic
	// fills the strongest catalog-valid combat blues. Quests leave all three
	// zero, which keeps the granted row exactly as before the fields existed.
	// The blues exclude the consumable alchemy aids (loot.BestEquipmentMagic).
	Plus         uint8
	PerfectStats bool
	PerfectMagic bool
	// Slot, when non-nil, seats this grant directly into that equipment wire
	// socket (0..12), replacing the occupant; nil keeps the bag-grant path.
	// Quests leave it nil.
	Slot *uint8
}
