/*
===========================================================================

package.go - shared authored item templates for NPC and Item Mall packages

Decode template fields before any transaction can grant an item. Package
admission remains owned by the catalogue, and inventory owns slot placement.

===========================================================================
*/
package commerce

import (
	"fmt"
	"math"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"strconv"
)

const maxPackageContents = 96

/*
================
packageContents
================
*/
func packageContents(rows [][]string, refs enterworld.ItemRefSource) ([]Content, error) {
	if refs == nil || len(rows) == 0 || len(rows) > maxPackageContents {
		return nil, fmt.Errorf("commerce: invalid package contents")
	}
	contents := make([]Content, 0, len(rows))
	for _, row := range rows {
		content, err := packageContent(row, refs)
		if err != nil {
			return nil, err
		}
		contents = append(contents, content)
	}
	return contents, nil
}

/*
================
packageContent

Equipment, ordinary expendables and unopened COS summoners carry different
Data meanings. Reject a template that the shared item body cannot preserve.
================
*/
func packageContent(row []string, refs enterworld.ItemRefSource) (Content, error) {
	if len(row) < 20 {
		return Content{}, fmt.Errorf("commerce: truncated item template")
	}
	ref, found := refs.ItemRefByCodename(row[3])
	if !found || ref == nil || ref.TypeIDs[0] != 3 {
		return Content{}, fmt.Errorf("commerce: unknown item %s", row[3])
	}
	plus, e1 := strconv.ParseUint(row[4], 10, 8)
	variance, e2 := strconv.ParseUint(row[5], 10, 64)
	data, e3 := strconv.ParseUint(row[6], 10, 32)
	count, e4 := strconv.ParseUint(row[7], 10, 8)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || count > wire.MaxMagicOptionsPerItem {
		return Content{}, fmt.Errorf("commerce: invalid template fields for %s", row[3])
	}
	magic := []uint64{}
	for index := 0; index < wire.MaxMagicOptionsPerItem; index++ {
		value, err := strconv.ParseUint(row[8+index], 10, 64)
		if err != nil || index >= int(count) && value != 0 {
			return Content{}, fmt.Errorf("commerce: invalid template magic for %s", row[3])
		}
		if index < int(count) {
			magic = append(magic, value)
		}
	}
	stack := uint16(1)
	flags := ref.TypeFlags()
	switch {
	case wire.IsEquipmentBand(flags):
		if data == 0 {
			if ref.VarianceIntMin1c0 == nil || *ref.VarianceIntMin1c0 < 0 || *ref.VarianceIntMin1c0 > math.MaxUint32 {
				return Content{}, fmt.Errorf("commerce: missing durability for %s", row[3])
			}
			data = uint64(*ref.VarianceIntMin1c0)
		}
	case wire.IsEtcBand(flags):
		capacity := ref.NativeFields.Get("maxStack")
		if math.IsNaN(capacity) || math.IsInf(capacity, 0) || capacity < 1 || capacity > math.MaxUint16 || capacity != math.Trunc(capacity) || data > math.MaxUint16 || variance != 0 || plus != 0 && !wire.EtcCarriesPlusByte(flags) || count != 0 && !wire.UsesIndexedMagicParams(flags) {
			return Content{}, fmt.Errorf("commerce: invalid expendable template %s", row[3])
		}
		stack = uint16(capacity)
	case wire.IsCosSummoner(flags):
		if data != 0 || plus != 0 || variance != 0 || count != 0 {
			return Content{}, fmt.Errorf("commerce: summoner template has an unexpected record: %s", row[3])
		}
	default:
		return Content{}, fmt.Errorf("commerce: unsupported item template %s", row[3])
	}
	return Content{Ref: ref, Stack: stack, Plus: uint8(plus), Variance: variance, Data: uint32(data), Magic: magic}, nil
}

/*
================
Hydrate

Finish the catalogue before publishing it. A missing item definition must not
turn one authored package into a different, partially delivered package.
================
*/
func (catalog *MallCatalog) Hydrate(refs enterworld.ItemRefSource) error {
	for index := range catalog.Offers {
		offer := &catalog.Offers[index]
		contents, err := packageContents(offer.Scraps, refs)
		if err != nil {
			return fmt.Errorf("mall: %s: %w", offer.Code, err)
		}
		offer.Contents = contents
		offer.PurchaseLimit = 5
		if len(contents) == 1 && wire.IsEtcBand(contents[0].Ref.TypeFlags()) && contents[0].Data == 0 {
			offer.PurchaseLimit = contents[0].Stack
		}
		offer.ItemIDs = make([]uint32, len(contents))
		for index, content := range contents {
			offer.ItemIDs[index] = content.Ref.RefObjID
		}
	}
	return nil
}
