package inventory

// ItemAmount is a version-independent authored grant or consumption request.
// The item authority resolves its codename against this world's item catalog.
type ItemAmount struct {
	Codename string
	Count    uint32
	// Plus is the enhancement level of granted equipment, PerfectStats sets
	// every white stat to its maximum and PerfectMagic fills the strongest
	// catalog-valid blues; quests leave all three zero (opensro-infra patch).
	Plus         uint8
	PerfectStats bool
	PerfectMagic bool
	// Slot, when non-nil, seats this grant directly into that equipment wire
	// socket (0..12), replacing the occupant; nil keeps the bag-grant path.
	// Quests leave it nil (opensro-infra patch).
	Slot *uint8
}
