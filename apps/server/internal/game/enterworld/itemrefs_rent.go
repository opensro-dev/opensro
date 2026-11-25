/*
===========================================================================

itemrefs_rent.go - marking the rental item references after the textdata load

The built tree's refrentitem (written only where later mall goods are
listed, scripts/later_mall_goods.py) names the rented item behind every
rent term; a delivered instance of one carries a deletion time (#4240).
This file marks the references at load: the wire and the mall grant
read the mark, nothing else does.

===========================================================================
*/
package enterworld

import (
	"path/filepath"
	"strconv"

	"opensro.online/server/internal/game/item/wire"
)

// rentTermItemID is a refrentitem row's rented-item cell: the term
// code sits at 1 and the rented item's RefObjID at 2 (the period
// columns are the mall catalog's, mallcatalog.go).
const rentTermItemID = 2

/*
================
markRentItems

The built tree's refrentitem (written only where later mall goods are
listed, scripts/later_mall_goods.py) names every rented item; those
references are rentals, and a delivered instance of one carries a
deletion time (#4240). No table (the native tree): nothing is.
================
*/
func (t *TextdataItems) markRentItems() {
	var rented []uint32
	// The wire measures every row of these references with the rent
	// leg, so it learns the same set the client's references teach it.
	defer func() { wire.SetRentRefs(rented) }()
	for _, fields := range readTextdataFile(filepath.Join(t.dir, "refrentitem.txt")) {
		if len(fields) <= rentTermItemID || fields[0] != "1" {
			continue
		}
		id, err := strconv.ParseUint(fields[rentTermItemID], 10, 32)
		if err != nil {
			continue
		}
		if ref, ok := t.byID[uint32(id)]; ok && ref != nil {
			ref.Rent = true
			rented = append(rented, uint32(id))
		}
	}
}
