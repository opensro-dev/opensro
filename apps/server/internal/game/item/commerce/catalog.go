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
	"sort"
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
	// RentMinutes is the item's rental period in minutes (v1.696
	// refrentitem RentTime, issue #4240): 0 = not a rental. The grant
	// stamps the item's expiry from it (grant.go). Only mall packages
	// carry it; NPC templates never name a rent term.
	RentMinutes uint32
}

/*
================
Offer
================
*/
type Offer struct {
	Slot uint8
	Ref  *enterworld.ItemRef
	// Price is in Currency: PaymentGold, PaymentGuildPoint, PaymentHonor or
	// (port-only, SRO_NASRUN) PaymentNasrunToken.
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
	// UIIT_MSG_GUILD_STORE_BUY_LIMIT and client 689CA4 UIIT_MSG_GUILD_LACK_GP).
	PaymentGuildPoint uint8 = 8
	// PaymentHonor is Training Camp honor (GROUP_STORE_*_HONOR packages;
	// client 689CDE UIIT_MSG_TC_LACK_HONOR_POINT).
	PaymentHonor uint8 = 32
	// PaymentNasrunToken is the Devil's Spirit token balance (port-only,
	// not native: SRO_NASRUN, #3456 step 3b; nasrungoods.go). v1.150
	// authors no price row with 64, so Load never admits one: only the
	// token goods carry it.
	PaymentNasrunToken uint8 = 64
	// The Item Mall's devices (mallcatalog.go): silk, gift silk and the
	// silk-point eligibility row.
	PaymentSilk      uint8 = 2
	PaymentGiftSilk  uint8 = 4
	PaymentSilkPoint uint8 = 16
)

/*
================
mallDevice / npcShopDevice

Which catalogue owns a refpricepolicyofitem PaymentDevice. A package
the Silk Mall and a gold shop both list (a degree 10 stone that joined
the mall on its own package, #4658) carries both shops' rows; each
catalogue reads its own and leaves the other's alone. v1.150's own data
never shares a package (NPC goods are priced {1}, {1,32} or {1,8}; the
mall's {2,4,16}), and the client keeps every device a package names in
one price list (CRefPackageItemData_AddPrice 0x812990), so the split is
the inferred native rule: a shop window shows and charges its own
devices. A device neither owns stays refused by both.
================
*/
func mallDevice(device uint64) bool {
	return device == uint64(PaymentSilk) || device == uint64(PaymentGiftSilk) || device == uint64(PaymentSilkPoint)
}

func npcShopDevice(device uint64) bool {
	return device == uint64(PaymentGold) || device == uint64(PaymentGuildPoint) || device == uint64(PaymentHonor)
}

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
	// Unpriced lists, sorted and once each, the packages a shop tab sells
	// that carry no gold, guild point or honor price row. The goods are
	// left out of the tabs; the native load check names each one too
	// (v1.188 RefPackageItem_ValidatePrices 0x6a9840, "(%s) register gold
	// price."), so a data defect is reported instead of a silent gap.
	Unpriced []string
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
	magic := enterworld.NewTextdataMagicOptions(dir)
	if err := magic.Load(); err != nil {
		return nil, fmt.Errorf("commerce: %w", err)
	}
	c := &Catalog{Tabs: map[int32][]Offer{}, Magic: magic, tabIDs: tabIDs}
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
	prices := npcShopPrices(tables["refpricepolicyofitem"], blocked)
	scraps := map[string][][]string{}
	for _, r := range tables["refscrapofpackageitem"] {
		if len(r) < 20 || r[0] != "1" {
			continue
		}
		scraps[r[2]] = append(scraps[r[2]], r)
	}
	c.Unpriced = listedWithoutPrice(tables["refshopgoods"], func(code string) bool {
		return blocked[code] || prices.mall[code] || prices.gold[code] != 0 || prices.guildPoint[code] != 0 || prices.honor[code] != 0
	})
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
		goldPrice, guildPointPrice, honorPrice := prices.gold[r[3]], prices.guildPoint[r[3]], prices.honor[r[3]]
		if blocked[r[3]] || len(rows) == 0 || (goldPrice == 0 && guildPointPrice == 0 && honorPrice == 0) || refs == nil {
			continue
		}
		contents, err := packageContents(rows, refs, rentTerms{})
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

/*
================
npcPrices / npcShopPrices

The NPC shop prices of live refpricepolicyofitem rows, by package: one
gold, guild point and honor amount each. A Silk Mall row is the mall's
(mallDevice): skipped, its package noted in mall; any other row this shop cannot charge (an
unknown device, a zero or unparsable amount, a repeated device) marks
its package in blocked, so the package is never sold at a partial price.
================
*/
type npcPrices struct {
	gold, guildPoint, honor map[string]uint64
	mall                    map[string]bool
}

func npcShopPrices(rows [][]string, blocked map[string]bool) npcPrices {
	p := npcPrices{gold: map[string]uint64{}, guildPoint: map[string]uint64{}, honor: map[string]uint64{}, mall: map[string]bool{}}
	for _, r := range rows {
		if len(r) < 5 || r[0] != "1" {
			continue
		}
		currency, ce := strconv.ParseUint(r[3], 10, 8)
		if ce == nil && mallDevice(currency) {
			p.mall[r[2]] = true
			continue
		}
		n, e := strconv.ParseUint(r[4], 10, 32)
		if e != nil || ce != nil || n == 0 {
			blocked[r[2]] = true
			continue
		}
		switch uint8(currency) {
		case PaymentGold:
			if p.gold[r[2]] != 0 {
				blocked[r[2]] = true
			}
			p.gold[r[2]] = n
		case PaymentGuildPoint:
			if p.guildPoint[r[2]] != 0 {
				blocked[r[2]] = true
			}
			p.guildPoint[r[2]] = n
		case PaymentHonor:
			if p.honor[r[2]] != 0 {
				blocked[r[2]] = true
			}
			p.honor[r[2]] = n
		default:
			blocked[r[2]] = true
		}
	}
	return p
}

/*
================
listedWithoutPrice

The packages live refshopgoods rows list that settled does not cover
(settled: priced in a currency the shop takes, or held back by a sell
condition), sorted and once each: the native load check's report set
(v1.188 RefPackageItem_ValidatePrices 0x6a9840).
================
*/
func listedWithoutPrice(goods [][]string, settled func(code string) bool) []string {
	missing := map[string]bool{}
	for _, r := range goods {
		if len(r) >= 5 && r[0] == "1" && !settled(r[3]) {
			missing[r[3]] = true
		}
	}
	codes := make([]string, 0, len(missing))
	for code := range missing {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}
