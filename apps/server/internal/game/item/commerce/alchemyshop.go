/*
===========================================================================

alchemyshop.go - Jangan's grocer as one alchemy shop (port-only, not native)

#1850 (owner, 2026-10-28, a player's request): the custom alchemy goods the
extra-goods list spreads over two Jangan shops move onto the grocer under
the name "OpenSRO Alchemy - Jangan", on three tabs (elixirs and powders,
attribute stones, alchemy stones), and the elixirs leave the smith. The
switch is SRO_JANGAN_ALCHEMY_SHOP, on by default because it extends the
already-on SRO_SHOP_EXTRA_GOODS default list: it acts only while that list
is in force, so the switch off, the extra goods off, or an operator's own
extra-goods list each keep today's arrangement (the native name, the mixed
alchemy tab, the elixirs at the smith). The goods tables below are the one
source for both shapes: the extra-goods default entries and the three tabs
are built from them, so the two can never drift apart.

The three tabs are synthesized at load, never authored: their ids are the
largest authored tab id + 1, + 2 and + 3 (in tab order), which the roster
projection in simulation derives from the same tables, and their names in
the extra-goods list are reserved names no authored table can carry. The
tab labels are the game's own category strings (the stall network's
"Elixir", the item types' "Attribute stone" and "Alchemy stone").

===========================================================================
*/
package commerce

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// EnvJanganAlchemyShop names the switch: "on" (the default) or "off".
const EnvJanganAlchemyShop = "SRO_JANGAN_ALCHEMY_SHOP"

// The extra-goods tabs the feature regroups, and the reserved tab names
// its three synthesized tabs travel under in an extra-goods list.
const (
	janganSmithTab       = "STORE_CH_SMITH_TAB3"
	janganGrocerTab      = "STORE_CH_ACCESSORY_TAB3"
	janganElixirsTab     = "OPENSRO_JANGAN_ELIXIRS"
	janganAttrStonesTab  = "OPENSRO_JANGAN_ATTRSTONES"
	janganMagicStonesTab = "OPENSRO_JANGAN_MAGICSTONES"
)

/*
================
janganGood

One custom alchemy good and its gold price: the rows of the tables below.
================
*/
type janganGood struct {
	codename string
	price    uint64
}

// The elixir recipes, owner 2026-10-11 at 1,000,000 on the smith's third
// tab; on the grocer they open the elixirs tab.
var janganElixirGoods = []janganGood{
	{"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A", 1000000},
	{"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_SHIELD_A", 1000000},
	{"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_A", 1000000},
	{"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_A", 1000000},
	{"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B", 1000000},
	{"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_SHIELD_B", 1000000},
	{"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_B", 1000000},
	{"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_B", 1000000},
}

// Degree 9 Lucky Powder, owner 2026-10-13 at A_09's native price; it
// closes the elixirs tab (the powders of "elixirs and powders").
var janganPowderGood = janganGood{"ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09", 54469}

// The degree 9 attribute stones, owner 2026-10-13 at 2,000,000 on the
// grocer's alchemy tab; here they are the attribute stones tab.
var janganAttrStoneGoods = []janganGood{
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_PA_09", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_PASTR_09", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_MA_09", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_MAINT_09", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_CRITICAL_09", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_HR_09", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_PD_09", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_PDSTR_09", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_MD_09", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_MDINT_09", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_ER_09", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_BR_09", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_PAR_09", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_MAR_09", 2000000},
}

// The degree 9 magic stones of the grocer's list, owner 2026-10-13 at
// 2,000,000; they open the alchemy stones tab.
var janganGrocerMagicStones = []janganGood{
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_STR_09", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_INT_09", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_DUR_09", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_HR_09", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_EVADE_BLOCK_09", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_EVADE_CRITICAL_09", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_ER_09", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_HP_09", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_MP_09", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_FROSTBITE_09", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_ESHOCK_09", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_BURN_09", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_POISON_09", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_ZOMBIE_09", 2000000},
}

// The smith's five stones (owner 2026-10-13, prices 2026-10-18); they move
// to the grocer's alchemy stones tab behind hers, at the same prices.
var janganSmithMagicStones = []janganGood{
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_09", 5000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_SOLID_09", 5000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_ASTRAL_09", 50000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_09", 25000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_REPAIR_09", 5000000},
}

/*
================
JanganAlchemyShopFromEnv

The switch alone: unset or on is the owner's default-on; anything else
but an off value is a boot error naming the setting.
================
*/
func JanganAlchemyShopFromEnv() (bool, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvJanganAlchemyShop))) {
	case "", "1", "on", "true":
		return true, nil
	case "0", "off", "false":
		return false, nil
	}
	return false, fmt.Errorf("%s=%q: want on or off", EnvJanganAlchemyShop, os.Getenv(EnvJanganAlchemyShop))
}

/*
================
JanganAlchemyShopActiveFromEnv

Whether the regroup applies in this process (the public rules publish
it): the switch on and the extra-goods default list in force.
================
*/
func JanganAlchemyShopActiveFromEnv() (bool, error) {
	return janganAlchemyActive(os.Getenv(EnvShopExtraGoods))
}

/*
================
janganAlchemyActive

Whether the regroup applies to an extra-goods value: the switch on and
the value the "on" default list (an operator's own list, or "off", is
the whole story and never regrouped). simulation reads the same two
settings for the roster; the tests there pin the same answers.
================
*/
func janganAlchemyActive(extraGoods string) (bool, error) {
	on, err := JanganAlchemyShopFromEnv()
	if err != nil || !on {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(extraGoods)) {
	case "1", "on", "true":
		return true, nil
	}
	return false, nil
}

/*
================
janganEntries

Goods as default-list entries on one tab, "tab:item:price" joined.
================
*/
func janganEntries(tab string, goods []janganGood) string {
	var b strings.Builder
	for _, good := range goods {
		fmt.Fprintf(&b, "%s:%s:%d,", tab, good.codename, good.price)
	}
	return b.String()
}

/*
================
janganSmithDefault

The smith's share of the extra-goods default: the elixirs, then its five
stones.
================
*/
func janganSmithDefault() string {
	return janganEntries(janganSmithTab, janganElixirGoods) +
		janganEntries(janganSmithTab, janganSmithMagicStones)
}

/*
================
janganGrocerDefault

The grocer's share of the extra-goods default: her magic stones, then
the attribute stones (her Lucky Powder rides the powder entries every
grocer shares, at the end of the default list).
================
*/
func janganGrocerDefault() string {
	return janganEntries(janganGrocerTab, janganGrocerMagicStones) +
		janganEntries(janganGrocerTab, janganAttrStoneGoods)
}

/*
================
janganAlchemyTabGoods

The three tabs' custom goods as extra goods on the reserved tab names:
the elixir recipes and the powder, the attribute stones, and the magic
stones (the grocer's, then the smith's at their prices).
================
*/
func janganAlchemyTabGoods() []ExtraGood {
	var goods []ExtraGood
	for _, good := range append(append([]janganGood{}, janganElixirGoods...), janganPowderGood) {
		goods = append(goods, ExtraGood{Tab: janganElixirsTab, Codename: good.codename, Price: good.price})
	}
	for _, good := range janganAttrStoneGoods {
		goods = append(goods, ExtraGood{Tab: janganAttrStonesTab, Codename: good.codename, Price: good.price})
	}
	for _, good := range append(append([]janganGood{}, janganGrocerMagicStones...), janganSmithMagicStones...) {
		goods = append(goods, ExtraGood{Tab: janganMagicStonesTab, Codename: good.codename, Price: good.price})
	}
	return goods
}

/*
================
regroupJanganAlchemy

The default list regrouped: the smith's and grocer's alchemy entries
come out (the grocer's powder with them; the other grocers keep theirs)
and the three tabs' goods go in. The goods sold are the same goods.
================
*/
func regroupJanganAlchemy(goods []ExtraGood) []ExtraGood {
	kept := make([]ExtraGood, 0, len(goods))
	for _, good := range goods {
		if good.Tab == janganSmithTab || good.Tab == janganGrocerTab {
			continue
		}
		kept = append(kept, good)
	}
	return append(kept, janganAlchemyTabGoods()...)
}

/*
================
hasJanganAlchemyGoods

Whether a goods list carries the reserved tab names (only the regrouped
default list does).
================
*/
func hasJanganAlchemyGoods(goods []ExtraGood) bool {
	for _, good := range goods {
		switch good.Tab {
		case janganElixirsTab, janganAttrStonesTab, janganMagicStonesTab:
			return true
		}
	}
	return false
}

/*
================
registerJanganAlchemyTabs

Gives the catalogue the three synthesized tabs: ids above every authored
tab (the roster derives the same ids) and the elixirs tab seeded with
the grocer's authored alchemy offers, renumbered from slot 0 in authored
order, so the native goods keep their prices at the head of the tab.
================
*/
func (c *Catalog) registerJanganAlchemyTabs() error {
	grocer, ok := c.tabIDs[janganGrocerTab]
	if !ok {
		return fmt.Errorf("%s: unknown shop tab %s", EnvJanganAlchemyShop, janganGrocerTab)
	}
	var maxID int32
	for _, id := range c.tabIDs {
		if id > maxID {
			maxID = id
		}
	}
	c.tabIDs[janganElixirsTab] = maxID + 1
	c.tabIDs[janganAttrStonesTab] = maxID + 2
	c.tabIDs[janganMagicStonesTab] = maxID + 3
	authored := append([]Offer(nil), c.Tabs[grocer]...)
	sort.Slice(authored, func(i, j int) bool { return authored[i].Slot < authored[j].Slot })
	for i := range authored {
		authored[i].Slot = uint8(i)
	}
	c.Tabs[maxID+1] = authored
	return nil
}
