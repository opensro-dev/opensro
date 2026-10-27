/*
===========================================================================

catalog.go - authored gold merchandise admission and package definitions

NPC and mall catalogues share template decoding. Their independent currency
and purchase policies remain in the respective catalogue owners.

===========================================================================
*/
package commerce

import (
	"fmt"
	"opensro.online/server/internal/game/enterworld"
	"os"
	"path/filepath"
	"strconv"
)

/*
================
Content
================
*/
type Content struct {
	TradeOwner string
	Ref        *enterworld.ItemRef
	Stack      uint16
	Plus       uint8
	Variance   uint64
	Data       uint32
	Magic      []uint64
}

/*
================
Offer
================
*/
type Offer struct {
	Slot uint8
	Ref  *enterworld.ItemRef
	// Price is in Currency: PaymentGold, PaymentGuildPoint or PaymentHonor.
	// When a package is priced in both gold and an auxiliary currency
	// (GROUP_STORE_*_GUILD or GROUP_STORE_*_HONOR), Price is the gold row,
	// Currency is PaymentGold, and GuildPointPrice / HonorPrice holds the
	// second row.
	Price           uint64
	Currency        uint8
	GuildPointPrice uint64
	HonorPrice      uint64
	Stack           uint16
	Contents        []Content
}

// refpricepolicyofitem payment types. 2/4/16 are the Item Mall's silk
// currencies and stay out of NPC shops.
const (
	PaymentGold uint8 = 1
	// PaymentGuildPoint is Guild Points (GROUP_STORE_*_GUILD packages;
	// the server's purchase checks, client 689BBA
	// UIIT_MSG_GUILD_STORE_BUY_LIMIT and 689CA4 UIIT_MSG_GUILD_LACK_GP).
	PaymentGuildPoint uint8 = 8
	// PaymentHonor is Training Camp honor (GROUP_STORE_*_HONOR packages;
	// client 689CDE UIIT_MSG_TC_LACK_HONOR_POINT).
	PaymentHonor uint8 = 32
)

/*
================
Catalog
================
*/
type Catalog struct {
	TradeQuotations map[[2]uint32]TradeQuotation
	TradeQuests     map[string]uint32
	Tabs            map[int32][]Offer
	Magic           enterworld.MagicOptionSource
	// tabIDs maps an authored tab name to its id (AddExtraGoods).
	tabIDs map[string]int32
}

/*
================
merchandiseKind

The item families an NPC shop sells: equipment (TypeID2 1), expendables
(TypeID2 3) other than 3/3/5, and the pet summoners (3/2/1, the stable's
Pet tab: the Grey Wolf Summon Scroll), which are sold without a record and
whose empty summoner body (state 1) the inventory wire already carries.
================
*/
func merchandiseKind(typeIDs [4]int64) bool {
	switch typeIDs[1] {
	case 1:
		return true
	case 2:
		return typeIDs[2] == 1
	case 3:
		return typeIDs[2] != 5
	}
	return false
}

/*
================
Load

Admits authored gold and honor packages and preserves every item template.
Conditional and silk policies still require their own authorities.
================
*/
func Load(dir string, refs enterworld.ItemRefSource) (*Catalog, error) {
	tables := map[string][][]string{}
	for _, name := range []string{"refshoptab", "refshopgoods", "refscrapofpackageitem", "refpricepolicyofitem", "refconditiontosellpackageitem", "refrewardpolicytosellpackageitem"} {
		path := filepath.Join(dir, name+".txt")
		rows := enterworld.ReadTextdataFile(path)
		info, err := os.Stat(path)
		if err != nil || rows == nil && info.Size() > 2 {
			return nil, fmt.Errorf("commerce: missing %s", name)
		}
		tables[name] = rows
	}
	tabIDs := map[string]int32{}
	c := &Catalog{Tabs: map[int32][]Offer{}, Magic: enterworld.NewTextdataMagicOptions(dir), tabIDs: tabIDs}
	for _, r := range tables["refshoptab"] {
		if len(r) >= 6 && r[0] == "1" {
			n, e := strconv.ParseInt(r[2], 10, 32)
			if e != nil {
				return nil, e
			}
			tabIDs[r[3]] = int32(n)
		}
	}
	blocked := map[string]bool{}
	for _, name := range []string{"refconditiontosellpackageitem", "refrewardpolicytosellpackageitem"} {
		for _, r := range tables[name] {
			if len(r) > 2 && r[0] == "1" {
				blocked[r[2]] = true
			}
		}
	}
	goldPrices, guildPointPrices, honorPrices := map[string]uint64{}, map[string]uint64{}, map[string]uint64{}
	for _, r := range tables["refpricepolicyofitem"] {
		if len(r) < 5 || r[0] != "1" {
			continue
		}
		n, e := strconv.ParseUint(r[4], 10, 32)
		currency, ce := strconv.ParseUint(r[3], 10, 8)
		if e != nil || ce != nil || n == 0 {
			blocked[r[2]] = true
			continue
		}
		switch uint8(currency) {
		case PaymentGold:
			if goldPrices[r[2]] != 0 {
				blocked[r[2]] = true
			}
			goldPrices[r[2]] = n
		case PaymentGuildPoint:
			if guildPointPrices[r[2]] != 0 {
				blocked[r[2]] = true
			}
			guildPointPrices[r[2]] = n
		case PaymentHonor:
			if honorPrices[r[2]] != 0 {
				blocked[r[2]] = true
			}
			honorPrices[r[2]] = n
		default:
			blocked[r[2]] = true
		}
	}
	scraps := map[string][][]string{}
	for _, r := range tables["refscrapofpackageitem"] {
		if len(r) < 20 || r[0] != "1" {
			continue
		}
		scraps[r[2]] = append(scraps[r[2]], r)
	}
	seen := map[string]bool{}
	for _, r := range tables["refshopgoods"] {
		if len(r) < 5 || r[0] != "1" {
			continue
		}
		tab, ok := tabIDs[r[2]]
		if !ok {
			return nil, fmt.Errorf("commerce: missing tab %s", r[2])
		}
		slot, e := strconv.ParseUint(r[4], 10, 8)
		if e != nil {
			return nil, e
		}
		key := r[2] + ":" + r[4]
		if seen[key] {
			return nil, fmt.Errorf("commerce: duplicate slot %s", key)
		}
		seen[key] = true
		rows := scraps[r[3]]
		goldPrice, guildPointPrice, honorPrice := goldPrices[r[3]], guildPointPrices[r[3]], honorPrices[r[3]]
		if blocked[r[3]] || len(rows) == 0 || (goldPrice == 0 && guildPointPrice == 0 && honorPrice == 0) || refs == nil {
			continue
		}
		contents, err := packageContents(rows, refs)
		if err != nil {
			continue
		}
		admitted := true
		for _, content := range contents {
			if !merchandiseKind(content.Ref.TypeIDs) {
				admitted = false
				break
			}
		}
		if !admitted {
			continue
		}
		first := contents[0]
		price, currency := goldPrice, PaymentGold
		if goldPrice == 0 {
			if honorPrice > 0 {
				price, currency = honorPrice, PaymentHonor
			} else {
				price, currency = guildPointPrice, PaymentGuildPoint
			}
		}
		c.Tabs[tab] = append(c.Tabs[tab], Offer{
			Slot:            uint8(slot),
			Ref:             first.Ref,
			Price:           price,
			Currency:        currency,
			GuildPointPrice: guildPointPrice,
			HonorPrice:      honorPrice,
			Stack:           first.Stack,
			Contents:        contents,
		})

	}
	if err := c.loadTradeCatalog(dir); err != nil {
		return nil, err
	}
	return c, nil
}
