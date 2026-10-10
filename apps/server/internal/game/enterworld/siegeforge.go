package enterworld

// DefaultSiegeItemForgeGroups returns the exact v1.150
// textdata/siegefortressitemforge.txt projection loaded by native
// sub_7f22d0 case 0x3a into CGlobalDataManager+0x4dc.
//
// The source file has 17 enabled rows, all in group 1. Native sub_5502c0
// classifies RefItem TypeIDs (3,3,3,2) into the group's +0x10 vector; in
// v1.150 those are refs 19569 and 19570. Every other row enters +0x00.
// sub_5d7ad0 only tests the vector sizes to decide whether the fortress
// manager-hire menu exposes blacksmith and trainer employment.
func DefaultSiegeItemForgeGroups() []SiegeItemForgeGroupRow {
	return []SiegeItemForgeGroupRow{
		{
			GroupID: 1,
			SmithItemRefs: []uint32{
				19227, 19228, 19231, 19232, 19233, 19234,
				19572, 19573, 19578, 19579, 19580,
				19605, 19606, 19609, 19610,
			},
			TrainerItemRefs: []uint32{19569, 19570},
			// Columns 4..6 (gold, GP, minutes), in file order.
			Items: []SiegeItemForgeItem{
				{19227, 32417, 590, 10}, {19228, 1500000, 6819, 450}, {19231, 6667, 122, 7},
				{19232, 33334, 477, 11}, {19233, 10000, 182, 3}, {19234, 11667, 213, 4},
				{19569, 5400, 36, 130}, {19570, 5400, 36, 130}, {19572, 63834, 912, 20},
				{19573, 159584, 1774, 48}, {19578, 32417, 590, 10}, {19579, 63834, 912, 20},
				{19580, 159584, 1774, 48}, {19605, 33334, 477, 11}, {19606, 66667, 953, 21},
				{19609, 23334, 425, 8}, {19610, 40000, 572, 12},
			},
		},
	}
}
