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

The gears magic stones were dropped from the listing (owner decision
2026-11-28, #3257): the v1.696 client has no shop, package or price
row for them. The import keeps their item rows so stones
already bought stay valid on the alchemy path. Premium's Gears tab
stays for nasrunGoods below. The sixteen weapon model switchers stand
in HOT shop > Halloween Models at the later mall's 600 silk, badged
hot; their use is action/modelswitcher.go. Their rows ride the optional
later-client import, so a catalogue built without it lists none of
them rather than refusing the boot (Imported below). The summon
scrolls stand in Premium's Others tab below: the three shipped
ITEM_ETC_E050729_SUMMON*_SCROLL rows no native family serves, priced
30 silk (1 and 5 people use) and 60 (2 people use), badged new;
they summon strong monsters (#4030), and their use is
action/summonscroll.go, behind this same setting.

The Devil's Spirit family was held back on the same evidence
(#3331): its goods serve only the Spirit, and a listing would have
sold items that cannot be used. The owner's call (2026-11-12) was
to port that system first (#3348) and list the family with it. The
system stands now (#3456), so with SRO_NASRUN on the six Spirits,
the extension gear and Sabakun's jewel list in Premium > Gears
(nasrunGoods) at the later mall's prices where it has them; off,
or without their imported rows, the tab is exactly as shipped. The Joon and
Firespirits summon scrolls of HOT shop > Halloween Exclusive have
no row in any source this build reads and wait for one; no rows
are invented.

===========================================================================
*/
package commerce

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/nasrun"
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
	// Nasrun marks a Devil's Spirit good (#3456 step 5): it lists
	// only while SRO_NASRUN stands. The rows ride the import like
	// every other optional row, but a listing without the socket,
	// awakening clock and alchemy behind it would sell an item that
	// cannot be used (#3331), so the runtime switch gates it too.
	Nasrun bool
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

// The Devil's Spirit prices. The v1.696 mall sells each Spirit at
// 150 silk on its MALL_PREMIUM_NASRUN tab (packages 51522-51527,
// one per Spirit) and the extension gear at 50 on
// MALL_PREMIUM_NASRUN_ETC (package 51528): the later mall's own
// refpricepolicyofitem silk rows (#4240 part 4). No later package
// sells Sabakun's jewel - the later mall's remaining Nasrun package
// sells an upgrade scroll for gold, an item this build does not
// import - so the jewel keeps the authored 30 of the Gears tab's
// tier, retunable in this place.
const (
	nasrunSpiritSilk = 150
	nasrunGearSilk   = 50
	nasrunJewelSilk  = 30
)

/*
================
nasrunGoods

The Devil's Spirit in Premium > Gears (#3456 step 5, #4240
part 4): the six A grade Spirits at the later mall's 150 silk, the
extension gear at its 50 and Sabakun's jewel at the authored 30
(above), each its own package, badged new. All ride SRO_NASRUN
twice over: their rows only exist when the import staged them
(Imported), and the mall lists them only while the system stands
(Nasrun) - until the Spirit's socket, awakening clock and alchemy
were ported, a listing would have sold items that cannot be used
(#3331). The same goods also sell on the honor shops' second tabs
for tokens (nasrungoods.go); the mall listing is the silk door the
later mall shows. The S grade _UNIQUE pair is not sold, as on the
token door: the later mall lists it only as a Premium Mall copy,
and that shop is never ported (owner decision on #4240), so the
silk mall never carries it.
================
*/
func nasrunGoods() []mallCategoryGood {
	goods := make([]mallCategoryGood, 0, len(nasrunSpirits)+2)
	for _, spirit := range nasrunSpirits {
		goods = append(goods, mallCategoryGood{Tab: "MALL_PREMIUM_GEARS", Package: "PACKAGE_" + spirit,
			Item: spirit, Silk: nasrunSpiritSilk, Badge: mallBadgeNew, Imported: true, Nasrun: true})
	}
	goods = append(goods,
		mallCategoryGood{Tab: "MALL_PREMIUM_GEARS", Package: "PACKAGE_" + nasrunGear,
			Item: nasrunGear, Silk: nasrunGearSilk, Badge: mallBadgeNew, Imported: true, Nasrun: true},
		mallCategoryGood{Tab: "MALL_PREMIUM_GEARS", Package: "PACKAGE_" + nasrunJewel,
			Item: nasrunJewel, Silk: nasrunJewelSilk, Badge: mallBadgeNew, Imported: true, Nasrun: true})
	return goods
}

// mallCategoryGoods is the layout's whole stock: the authored rows
// below plus the Devil's Spirit, model switcher and pet scroll listings.
var mallCategoryGoods = slices.Concat(mallLayoutGoods, nasrunGoods(), modelSwitcherGoods(), petScrollGoods())

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
	// family serves, monster summons for 1, 2 and 5 people (#4030):
	// 30 silk, 60 for the 2 people scroll, 30 for 5, badged new.
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

// PetScrollItems are the summon scrolls of the pet kinds the mall
// sells: the fox, an attack (growth) pet, and the winter snowman, a
// pickup pet with an event copy of its scroll, both imported from the
// later sources (#3328: scripts/pet_import.py), plus the kangaroo, a
// growth pet, and the pegasus, a vehicle, whose rows the native tree
// already carries (#4240 part 3). Each names its kind in itemdata
// column 119, so the persistent summoner serves it once its rows are
// in the tree.
var PetScrollItems = []string{
	"ITEM_COS_P_FOX_SCROLL",
	"ITEM_COS_P_WINTER_SNOWMAN_SCROLL",
	"ITEM_COS_P_WINTER_SNOWMAN_EVENT_SCROLL",
	"ITEM_COS_P_KANGAROO_SCROLL",
	"ITEM_COS_C_PEGASUS_SCROLL",
}

// The shipped mall's pet tiers: every growth (attack) pet scroll is
// 130 silk (bear, kangaroo, penguin, raven), every silk pickup pet 88
// (cat, brownie, pink pig, rabbit) and every vehicle scroll 1
// (scarabaeus, tiger, ostrich). The v1.696 client sells its winter
// snowman at the same 88 and its whole vehicle tab at the same 1; its
// fox has no real price (a 9999999 placeholder), so the fox takes its
// kind's tier, while its kangaroo is a real 130 and its pegasus a
// real 1.
const (
	growthPetScrollSilk  = 130
	pickupPetScrollSilk  = 88
	vehiclePetScrollSilk = 1
)

/*
================
petScrollGoods

One Pet-shop listing per pet scroll the later mall sells, in the tabs
the shipped mall keeps for its own kinds and at their tier: growth
pets in MALL_PET_A_GROWTH, the silk pickup pets in MALL_PET_B_SILK,
vehicles in MALL_PET_C_VEHICLE. The event copy of the snowman scroll
has no mall row in the v1.696 client, so it is not listed. The
kangaroo's and the pegasus's packages already stand in the tree's own
data (at these same prices), so the layout only lists them; the rows
stand only when the item is in the tree (Imported), so a build
without the fox's and snowman's later source lists neither of those
and still boots.
================
*/
func petScrollGoods() []mallCategoryGood {
	return []mallCategoryGood{
		{Tab: "MALL_PET_A_GROWTH", Package: "PACKAGE_ITEM_COS_P_FOX_SCROLL",
			Item: "ITEM_COS_P_FOX_SCROLL", Silk: growthPetScrollSilk, Badge: mallBadgeNew, Imported: true},
		{Tab: "MALL_PET_B_SILK", Package: "PACKAGE_ITEM_COS_P_WINTER_SNOWMAN_SCROLL",
			Item: "ITEM_COS_P_WINTER_SNOWMAN_SCROLL", Silk: pickupPetScrollSilk, Badge: mallBadgeNew, Imported: true},
		{Tab: "MALL_PET_A_GROWTH", Package: "PACKAGE_ITEM_COS_P_KANGAROO_SCROLL",
			Item: "ITEM_COS_P_KANGAROO_SCROLL", Silk: growthPetScrollSilk, Badge: mallBadgeNew, Imported: true},
		{Tab: "MALL_PET_C_VEHICLE", Package: "PACKAGE_ITEM_COS_C_PEGASUS_SCROLL",
			Item: "ITEM_COS_C_PEGASUS_SCROLL", Silk: vehiclePetScrollSilk, Badge: mallBadgeNew, Imported: true},
	}
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

A good the layout would author (Item set) whose package the data
already carries is sold as the data has it: the layout writes no second
package and keeps the data's price, and only lists it in its layout tab
with its badge. Since #4241 the build imports the v1.696 mall row for
the winter snowman scroll (MALL_PET_B_SILK, 88 silk, the layout's own
tab and price), and authoring it again refused the GameWorld boot
("already has package data"). A later native row wins over the
layout's stand-in. The data's own mall listing was removed with the
others above, so the layout's row is its one listing. The layout
naming one package in two tabbed goods is still refused.
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
	laidOut := map[string]bool{}
	for _, good := range mallCategoryGoods {
		if good.Tab != "" {
			if laidOut[good.Package] {
				return nil, fmt.Errorf("%s: the layout lists %s twice", EnvMallCategories, good.Package)
			}
			laidOut[good.Package] = true
		}
		if good.Imported && !itemRefPresent(refs, good.Item) {
			continue
		}
		if good.Nasrun && !nasrun.Enabled(os.Getenv(nasrun.Env)) {
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
		if good.Item != "" && !authored[good.Package] {
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

Whether the item table holds this codename: the gate an Imported
good lists behind, so a catalogue built without the later-client
import skips its listing instead of refusing the boot.
================
*/
func itemRefPresent(refs enterworld.ItemRefSource, codename string) bool {
	if refs == nil {
		return false
	}
	ref, ok := refs.ItemRefByCodename(codename)
	return ok && ref != nil
}
