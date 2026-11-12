/*
===========================================================================

mallcategories.go - the later client's Item Mall categories (SRO_MALL_CATEGORIES)

Port-only, not native (owner ask 2026-11-12, issue #3257). The v2.355
mall's left rail - Expendables, Avatar, Pet, Premium, Alchemy and the
HOT shop - with sub-category tabs (HOT: Halloween Exclusive, Halloween
Models, Recommended; Premium: Premium, Gears, Others; Alchemy: Astral,
Immortal, Others), item search and the N/H/R goods badges. v1.150's mall
has no HOT shop, one Premium tab and two Alchemy tabs, so the category
structure is authored here as the shipped data would carry it, exactly
like SRO_MALL_EXTRA_GOODS authors its rows (mallextra.go). "off" (the
default) is the shipped mall exactly: LoadMall adds no rows and stamps
no badge.

The layout below is the whole authored structure. The HOT shop is
appended after the five shipped shops, so every shipped shop, tab and
slot address keeps its number and prices and purchases resolve as
before. The Halloween outfits move (never duplicate) from the avatar
tabs SRO_MALL_EXTRA_GOODS lists them in: one package keeps one listing,
so the basket and the purchase quote see each package once. The badges
are per package for the same reason: "hot" on the HOT shop's Halloween
goods, "new" on the Destruction Rondo this layout first lists
(PACKAGE_ITEM_MALL_ARCHEMY_RONDO_02 for ITEM_ETC_ARCHEMY_RONDO_02,
which the alchemy Dissolve already honours), and "recommended" on the
three premium tickets the Recommended list points at. Prices are the
packages' own authored ones; the Rondo's authored package takes the
alchemy stones' 5-silk tier, as a balance the shard owner can retune in
this one place.

Premium's Gears tab and the recommendation list's stock beyond the
premium tickets belong to the epic's later lines (the gears items of
#3257); the tab stands ready for them. The sixteen weapon model
switchers stand in HOT shop > Halloween Models at the later mall's 600
silk, badged hot; their use is action/modelswitcher.go. Their rows
ride the optional later-client import, so a catalogue built without it
lists none of them rather than refusing the boot (Imported below). The
summon scrolls stand in Premium's Others tab below: the three shipped
ITEM_ETC_E050729_SUMMON*_SCROLL rows no native family serves, priced
30 silk for one soldier and 60 for two, badged new; their use is
action/summonscroll.go, behind this same setting.

===========================================================================
*/
package commerce

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// EnvMallCategories names the setting: "on" authors the later mall's
// categories into the catalogue; "off" and unset are the shipped mall.
const EnvMallCategories = "SRO_MALL_CATEGORIES"

// The badges a catalogue row may carry (client contracts/item-mall.ts).
const (
	mallBadgeNew         = "new"
	mallBadgeHot         = "hot"
	mallBadgeRecommended = "recommended"
)

// mallCategoryTab is one authored tab of the layout: its shop, code and
// the label the shipped string table already holds (a client without
// the Experimental categories row reads these; the row reads the later
// names from mallTabCaptionLater).
type mallCategoryTab struct {
	Shop  string
	Code  string
	Group string
	Label string
}

// mallCategoryGood is one authored listing: a package placed in a tab,
// badged when the HOT shop or the new stock calls for it. Item and Silk
// author a package the shipped data lacks (authorMallPackage).
type mallCategoryGood struct {
	Tab     string
	Package string
	Item    string
	Silk    uint32
	Badge   string
	// Imported marks an Item that only the optional later-client
	// import authors: without its row the listing is skipped, not
	// refused, so a build without the archive boots the rest.
	Imported bool
}

// ModelSwitcherItems are the sixteen weapon model switchers the later
// mall sells (issue #3257): six Chinese and ten European weapon and
// shield types. action/modelswitcher.go serves their use.
var ModelSwitcherItems = []string{
	"ITEM_ORIGIN_CH_HW_SWORD_SCROLL", "ITEM_ORIGIN_CH_HW_BLADE_SCROLL",
	"ITEM_ORIGIN_CH_HW_TBLADE_SCROLL", "ITEM_ORIGIN_CH_HW_SPEAR_SCROLL",
	"ITEM_ORIGIN_CH_HW_BOW_SCROLL", "ITEM_ORIGIN_CH_HW_SHIELD_SCROLL",
	"ITEM_ORIGIN_EU_HW_DAGGER_SCROLL", "ITEM_ORIGIN_EU_HW_SWORD_SCROLL",
	"ITEM_ORIGIN_EU_HW_TSWORD_SCROLL", "ITEM_ORIGIN_EU_HW_AXE_SCROLL",
	"ITEM_ORIGIN_EU_HW_CROSSBOW_SCROLL", "ITEM_ORIGIN_EU_HW_DARKSTAFF_SCROLL",
	"ITEM_ORIGIN_EU_HW_TSTAFF_SCROLL", "ITEM_ORIGIN_EU_HW_HARP_SCROLL",
	"ITEM_ORIGIN_EU_HW_STAFF_SCROLL", "ITEM_ORIGIN_EU_HW_SHIELD_SCROLL",
}

// modelSwitcherSilk is the later mall's price of every switcher.
const modelSwitcherSilk = 600

var mallCategoryTabs = []mallCategoryTab{
	// The HOT shop, appended after the shipped shops (shop index 5).
	{Shop: "MALL_HOT", Code: "MALL_HOT_EXCLUSIVE", Group: "MALL_HOT_GROUP1", Label: "UIIT_STT_SILKMALL_EVENT"},
	{Shop: "MALL_HOT", Code: "MALL_HOT_MODELS", Group: "MALL_HOT_GROUP1", Label: "UIIT_STT_SILKMALL_AVATA"},
	// Premium gains Gears and Others after its shipped Premium tab.
	{Shop: "MALL_PREMIUM", Code: "MALL_PREMIUM_GEARS", Group: "MALL_PREMIUM_GROUP1", Label: "SN_TAB_ETC"},
	{Shop: "MALL_PREMIUM", Code: "MALL_PREMIUM_OTHERS", Group: "MALL_PREMIUM_GROUP1", Label: "UIIT_STT_SILKMALL_ETC"},
	// Alchemy gains Others after Astral and Athanasia (Immortal).
	{Shop: "MALL_ARCHEMY", Code: "MALL_ARCHEMY_OTHERS", Group: "MALL_ARCHEMY_GROUP1", Label: "UIIT_STT_SILKMALL_ETC"},
}

var mallCategoryGoods = append(mallLayoutGoods, modelSwitcherGoods()...)

var mallLayoutGoods = []mallCategoryGood{
	// Halloween Exclusive: the two attachment pieces; Models: the
	// dresses and hats. All six are SRO_MALL_EXTRA_GOODS packages the
	// layout re-homes (see the banner).
	{Tab: "MALL_HOT_EXCLUSIVE", Package: "PACKAGE_ITEM_MALL_AVATAR_M_HALLOWEEN_ATTACH", Badge: mallBadgeHot},
	{Tab: "MALL_HOT_EXCLUSIVE", Package: "PACKAGE_ITEM_MALL_AVATAR_W_HALLOWEEN_ATTACH", Badge: mallBadgeHot},
	{Tab: "MALL_HOT_MODELS", Package: "PACKAGE_ITEM_MALL_AVATAR_M_HALLOWEEN", Badge: mallBadgeHot},
	{Tab: "MALL_HOT_MODELS", Package: "PACKAGE_ITEM_MALL_AVATAR_W_HALLOWEEN", Badge: mallBadgeHot},
	{Tab: "MALL_HOT_MODELS", Package: "PACKAGE_ITEM_MALL_AVATAR_M_HALLOWEEN_HAT", Badge: mallBadgeHot},
	{Tab: "MALL_HOT_MODELS", Package: "PACKAGE_ITEM_MALL_AVATAR_W_HALLOWEEN_HAT", Badge: mallBadgeHot},
	// The summon scrolls (issue #3257): shipped items no native
	// family serves; one soldier for 30 silk, two for 60, badged new.
	// Their use is action/summonscroll.go behind this same setting.
	{Tab: "MALL_PREMIUM_OTHERS", Package: "PACKAGE_ITEM_ETC_E050729_SUMMON_SCROLL",
		Item: "ITEM_ETC_E050729_SUMMON_SCROLL", Silk: 30, Badge: mallBadgeNew},
	{Tab: "MALL_PREMIUM_OTHERS", Package: "PACKAGE_ITEM_ETC_E050729_SUMMON_ONE_SCROLL",
		Item: "ITEM_ETC_E050729_SUMMON_ONE_SCROLL", Silk: 30, Badge: mallBadgeNew},
	{Tab: "MALL_PREMIUM_OTHERS", Package: "PACKAGE_ITEM_ETC_E050729_SUMMON_TWO_SCROLL",
		Item: "ITEM_ETC_E050729_SUMMON_TWO_SCROLL", Silk: 60, Badge: mallBadgeNew},
	// The Destruction Rondo, first listed by this layout (issue #3257
	// line 2): the item ships in v1.150 and Dissolve honours it; only a
	// mall package was missing. 5 silk, the astral stones' tier.
	{Tab: "MALL_ARCHEMY_OTHERS", Package: "PACKAGE_ITEM_MALL_ARCHEMY_RONDO_02",
		Item: "ITEM_ETC_ARCHEMY_RONDO_02", Silk: 5, Badge: mallBadgeNew},
	// The Recommended list's picks: the premium tickets, badged in
	// their shipped Premium tab (no second listing; the client's
	// Recommended tab gathers badged offers).
	{Package: "PACKAGE_ITEM_MALL_PREMIUM_GLOBAL_GOLDTIME", Badge: mallBadgeRecommended},
	{Package: "PACKAGE_ITEM_MALL_PREMIUM_GLOBAL_GOLDTIME_PLUS", Badge: mallBadgeRecommended},
	{Package: "PACKAGE_ITEM_MALL_PREMIUM_GLOBAL_SILVERTIME", Badge: mallBadgeRecommended},
}

/*
================
modelSwitcherGoods

One Halloween Models listing per switcher, each its own authored
package, PACKAGE_ plus the item codename.
================
*/
func modelSwitcherGoods() []mallCategoryGood {
	goods := make([]mallCategoryGood, 0, len(ModelSwitcherItems))
	for _, item := range ModelSwitcherItems {
		goods = append(goods, mallCategoryGood{Tab: "MALL_HOT_MODELS", Package: "PACKAGE_" + item,
			Item: item, Silk: modelSwitcherSilk, Badge: mallBadgeHot, Imported: true})
	}
	return goods
}

/*
================
MallCategoriesFromEnv

"off" and unset are the shipped mall; "on" authors the categories.
================
*/
func MallCategoriesFromEnv() (bool, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvMallCategories))) {
	case "1", "on", "true":
		return true, nil
	case "", "0", "off", "false":
		return false, nil
	}
	return false, fmt.Errorf("%s=%q: want on or off", EnvMallCategories, os.Getenv(EnvMallCategories))
}

/*
================
addMallCategoryRows

Writes the layout's shops, tabs and listings into the reference tables
as the shipped data would carry them, after SRO_MALL_EXTRA_GOODS wrote
its rows: a layout package already listed in a mall tab gives up that
listing to its layout tab (the Halloween outfits leave the avatar
tabs), so no package is ever listed twice. Badge-only goods (the
Recommended picks) must already be listed; they gain no row. Returns
each authored package's badge for LoadMall to stamp on its one offer.
A layout package whose data is missing refuses the boot naming the
setting, like a bad extra-goods entry.
================
*/
func addMallCategoryRows(tables map[string][][]string, refs enterworld.ItemRefSource) (map[string]string, error) {
	shops := map[string]bool{}
	for _, row := range tables["refmappingshopgroup"] {
		if len(row) >= 4 && row[2] == mallGroupCode {
			shops[row[3]] = true
		}
	}
	tabs := map[string]bool{}
	for _, row := range tables["refshoptab"] {
		if len(row) >= 4 {
			tabs[row[3]] = true
		}
	}
	authored := map[string]bool{}
	nextPackageID := uint64(0)
	for _, row := range tables["refpackageitem"] {
		if len(row) >= 4 {
			authored[row[3]] = true
		}
		if len(row) >= 3 {
			if id, err := strconv.ParseUint(row[2], 10, 32); err == nil && id >= nextPackageID {
				nextPackageID = id + 1
			}
		}
	}
	// The listings the layout owns: each layout package's mall listing
	// is removed before its own row is written.
	mallTabs := map[string]bool{}
	for code := range tabs {
		if strings.HasPrefix(code, "MALL_") {
			mallTabs[code] = true
		}
	}
	for _, good := range mallCategoryGoods {
		if good.Tab == "" {
			continue
		}
		kept := tables["refshopgoods"][:0]
		for _, row := range tables["refshopgoods"] {
			if len(row) >= 4 && row[3] == good.Package && mallTabs[row[2]] {
				continue
			}
			kept = append(kept, row)
		}
		tables["refshopgoods"] = kept
	}
	// The new shops and their tab groups, appended in row order so the
	// shipped shop indices stand. The HOT shop is new; the Premium and
	// Alchemy tab rows join their shipped shop's group.
	newShop := map[string]bool{}
	for _, tab := range mallCategoryTabs {
		if !shops[tab.Shop] && !newShop[tab.Shop] {
			tables["refmappingshopgroup"] = append(tables["refmappingshopgroup"],
				[]string{"1", "11", mallGroupCode, tab.Shop})
			tables["refmappingshopwithtab"] = append(tables["refmappingshopwithtab"],
				[]string{"1", "11", tab.Shop, tab.Group})
			newShop[tab.Shop] = true
		}
		if tabs[tab.Code] {
			return nil, fmt.Errorf("%s: the shipped mall already has tab %s", EnvMallCategories, tab.Code)
		}
		tabs[tab.Code] = true
		tables["refshoptab"] = append(tables["refshoptab"],
			[]string{"1", "11", "0", tab.Code, tab.Group, tab.Label})
	}
	next := map[string]int{}
	listed := map[string]bool{}
	for _, row := range tables["refshopgoods"] {
		if len(row) < 5 || !tabs[row[2]] {
			continue
		}
		listed[row[3]] = true
		if slot, err := strconv.Atoi(row[4]); err == nil && slot+1 > next[row[2]] {
			next[row[2]] = slot + 1
		}
	}
	badges := map[string]string{}
	for _, good := range mallCategoryGoods {
		if good.Imported && !itemRefPresent(refs, good.Item) {
			continue
		}
		if good.Badge != "" {
			badges[good.Package] = good.Badge
		}
		if good.Tab == "" {
			if !listed[good.Package] {
				return nil, fmt.Errorf("%s: %s is not listed in the mall", EnvMallCategories, good.Package)
			}
			continue
		}
		if good.Item != "" {
			if authored[good.Package] {
				return nil, fmt.Errorf("%s: %s already has package data", EnvMallCategories, good.Package)
			}
			entry := MallExtraGood{Tab: good.Tab, Package: good.Package, Item: good.Item, Silk: good.Silk}
			if err := authorMallPackage(tables, entry, refs, uint32(nextPackageID)); err != nil {
				return nil, err
			}
			authored[good.Package] = true
			nextPackageID++
		} else if !authored[good.Package] {
			return nil, fmt.Errorf("%s: %s has no package data", EnvMallCategories, good.Package)
		}
		tables["refshopgoods"] = append(tables["refshopgoods"],
			[]string{"1", "11", good.Tab, good.Package, strconv.Itoa(next[good.Tab])})
		next[good.Tab]++
		listed[good.Package] = true
	}
	return badges, nil
}

/*
================
itemRefPresent
================
*/
func itemRefPresent(refs enterworld.ItemRefSource, codename string) bool {
	if refs == nil {
		return false
	}
	ref, ok := refs.ItemRefByCodename(codename)
	return ok && ref != nil
}
