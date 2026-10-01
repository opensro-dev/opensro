/*
===========================================================================

mallcatalog.go - authored v1.150 mall shop addresses and package prices

The GROUP_MALL mapping owns shop indices. Tab order comes from reference data,
not category names, UI ordering or a flattened merchant catalogue.

===========================================================================
*/

package commerce

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"opensro.online/server/internal/game/enterworld"
)

const mallGroupCode = "GROUP_MALL"

/*
================
MallAddress

Client 7058E0 resolves these indices; server 526610 validates the registered
shop before dispatching its ordinary package purchase machinery.
================
*/
type MallAddress struct {
	Group uint16 `json:"group"`
	Shop  uint8  `json:"shop"`
	Tab   uint8  `json:"tab"`
	Slot  uint8  `json:"slot"`
}

/*
================
MallTab
================
*/
type MallTab struct {
	Shop     uint8  `json:"shop"`
	Tab      uint8  `json:"tab"`
	Category string `json:"category"`
	Label    string `json:"label"`
	Code     string `json:"-"`
}

/*
================
MallPackage

Currency bits 2, 4 and 16 are Silk, Gift Silk and optional points respectively
(client 6BD020 / 6BFD00). Zero amounts still preserve currency eligibility.
================
*/
type MallPackage struct {
	CurrencyMask uint8     `json:"currencyMask"`
	Contents     []Content `json:"-"`
	ItemIDs      []uint32  `json:"itemIds"`
	MallAddress
	PurchaseLimit uint16     `json:"purchaseLimit"`
	PackageID     uint32     `json:"packageId"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	Icon          string     `json:"icon"`
	Silk          uint32     `json:"silk"`
	GiftSilk      uint32     `json:"giftSilk"`
	AllowsPoints  bool       `json:"allowsPoints"`
	Code          string     `json:"-"`
	Scraps        [][]string `json:"-"`
}

/*
================
MallCatalog
================
*/
type MallCatalog struct {
	Tabs   []MallTab
	Offers []MallPackage
}

/*
================
mallTable
================
*/
func mallTable(dir, name string) ([][]string, error) {
	path := filepath.Join(dir, name+".txt")
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("mall: %s: %w", name, err)
	}
	rows := enterworld.ReadTextdataFile(path)
	if rows == nil && info.Size() > 2 {
		return nil, fmt.Errorf("mall: cannot decode %s", name)
	}
	return rows, nil
}

/*
================
LoadMall

Prices are resolved by package codename; the wire package ID is checked again
at purchase time to reject stale slot mappings. All active mall rows must have
a package and a price, so a partial catalogue cannot silently ship.
================
*/
func LoadMall(dir string) (*MallCatalog, error) {
	tables := map[string][][]string{}
	for _, name := range []string{
		"refshopgroup", "refmappingshopgroup", "refmappingshopwithtab", "refshoptab",
		"refshopgoods", "refpackageitem", "refpricepolicyofitem", "refscrapofpackageitem",
		"refconditiontosellpackageitem", "refrewardpolicytosellpackageitem",
	} {
		rows, err := mallTable(dir, name)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if len(row) > 0 && row[0] == "1" {
				tables[name] = append(tables[name], row)
			}
		}
	}
	return buildMallCatalog(tables)
}

/*
================
buildMallCatalog

Reject ambiguous shop/tab mappings before publishing addresses. A missing
reference must never shift the indices of the remaining purchase targets.
================
*/
func buildMallCatalog(tables map[string][][]string) (*MallCatalog, error) {
	var group uint64
	for _, row := range tables["refshopgroup"] {
		if len(row) >= 4 && row[3] == mallGroupCode {
			value, err := strconv.ParseUint(row[2], 10, 16)
			if err != nil || value == 0 || group != 0 {
				return nil, fmt.Errorf("mall: invalid or repeated GROUP_MALL")
			}
			group = value
		}
	}
	if group == 0 {
		return nil, fmt.Errorf("mall: missing GROUP_MALL")
	}
	catalog := &MallCatalog{}
	shops := []string{}
	seenShops := map[string]bool{}
	for _, row := range tables["refmappingshopgroup"] {
		if len(row) >= 4 && row[2] == mallGroupCode {
			if row[3] == "" || seenShops[row[3]] {
				return nil, fmt.Errorf("mall: invalid or repeated shop mapping")
			}
			seenShops[row[3]] = true
			shops = append(shops, row[3])
		}
	}
	if len(shops) == 0 || len(shops) > 256 {
		return nil, fmt.Errorf("mall: invalid shop count")
	}
	for index, shop := range shops {
		tabIndex := 0
		seenGroups := map[string]bool{}
		seenTabs := map[string]bool{}
		for _, mapping := range tables["refmappingshopwithtab"] {
			if len(mapping) < 4 || mapping[2] != shop {
				continue
			}
			if mapping[3] == "" || seenGroups[mapping[3]] {
				return nil, fmt.Errorf("mall: invalid or repeated tab group for %s", shop)
			}
			seenGroups[mapping[3]] = true
			previous := tabIndex
			for _, row := range tables["refshoptab"] {
				if len(row) < 6 || row[4] != mapping[3] {
					continue
				}
				if row[3] == "" || seenTabs[row[3]] {
					return nil, fmt.Errorf("mall: invalid or repeated tab for %s", shop)
				}
				seenTabs[row[3]] = true
				if tabIndex > 255 {
					return nil, fmt.Errorf("mall: too many tabs for %s", shop)
				}
				catalog.Tabs = append(catalog.Tabs, MallTab{Shop: uint8(index), Tab: uint8(tabIndex), Category: shop, Label: row[5], Code: row[3]})
				tabIndex++
			}
			if tabIndex == previous {
				return nil, fmt.Errorf("mall: unresolved tab group for %s", shop)
			}
		}
		if tabIndex == 0 {
			return nil, fmt.Errorf("mall: shop without tabs: %s", shop)
		}
	}
	for _, tab := range catalog.Tabs {
		seen := map[uint8]bool{}
		for _, row := range tables["refshopgoods"] {
			if len(row) < 5 || row[2] != tab.Code {
				continue
			}
			slot, err := strconv.ParseUint(row[4], 10, 8)
			if err != nil || seen[uint8(slot)] {
				return nil, fmt.Errorf("mall: invalid or repeated slot in %s", tab.Code)
			}
			seen[uint8(slot)] = true
			offer, err := mallPackage(tables, row[3])
			if err != nil {
				return nil, err
			}
			offer.MallAddress = MallAddress{Group: uint16(group), Shop: tab.Shop, Tab: tab.Tab, Slot: uint8(slot)}
			catalog.Offers = append(catalog.Offers, offer)
		}
	}
	return catalog, nil
}

/*
================
mallPackage
================
*/
func mallPackage(tables map[string][][]string, code string) (MallPackage, error) {
	offer := MallPackage{Code: code}
	// The v1.150 mall has no conditional or reward policies. Fail publication
	// if future data adds one, rather than selling it without its restrictions.
	for _, name := range []string{"refconditiontosellpackageitem", "refrewardpolicytosellpackageitem"} {
		for _, row := range tables[name] {
			if len(row) > 2 && row[2] == code {
				return offer, fmt.Errorf("mall: unsupported %s policy for %s", name, code)
			}
		}
	}
	for _, row := range tables["refpackageitem"] {
		if len(row) < 9 || row[3] != code {
			continue
		}
		id, err := strconv.ParseUint(row[2], 10, 32)
		if err != nil || id == 0 || offer.PackageID != 0 {
			return offer, fmt.Errorf("mall: invalid or repeated package %s", code)
		}
		offer.PackageID = uint32(id)
		offer.Name, offer.Description, offer.Icon = row[6], row[7], row[8]
	}
	seen := map[uint64]bool{}
	for _, row := range tables["refpricepolicyofitem"] {
		if len(row) < 5 || row[2] != code {
			continue
		}
		currency, currencyError := strconv.ParseUint(row[3], 10, 8)
		amount, amountError := strconv.ParseUint(row[4], 10, 32)
		if currencyError != nil || amountError != nil || seen[currency] {
			return offer, fmt.Errorf("mall: invalid or repeated price for %s", code)
		}
		seen[currency] = true
		offer.CurrencyMask |= uint8(currency)
		switch currency {
		case 2:
			offer.Silk = uint32(amount)
		case 4:
			offer.GiftSilk = uint32(amount)
		case 16:
			// v1.150 uses this row as eligibility for a player-selected
			// contribution. An authored fixed charge needs its own rule.
			if amount != 0 {
				return offer, fmt.Errorf("mall: unsupported fixed point charge for %s", code)
			}
			offer.AllowsPoints = true
		default:
			return offer, fmt.Errorf("mall: unexpected currency %d for %s", currency, code)
		}
	}
	for _, row := range tables["refscrapofpackageitem"] {
		if len(row) >= 20 && row[2] == code {
			offer.Scraps = append(offer.Scraps, append([]string(nil), row...))
		}
	}
	if offer.PackageID == 0 || len(seen) == 0 || len(offer.Scraps) == 0 {
		return offer, fmt.Errorf("mall: incomplete package %s", code)
	}
	return offer, nil
}
