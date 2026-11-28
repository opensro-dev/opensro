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
source for both shapes: the extra-goods default entries and the shop's tabs
are built from them, so the two can never drift apart.

A fourth tab (#4684, requested 2026-11-27, awaiting the owner's
approval) sells the degree 10 stones at the degree 9 prices: the 14
attribute stones, then the 14 grocer magic stones and the Luck, Steady,
Astral and Athanasia stones (no degree 10 Repair stone exists). The degree 10 Steady stone the smith's list
carries moves to it, so the alchemy stones tab is degree 9 alone. The
tab exists only in the regrouped shape: the switch off sells no degree
10 stone beyond today's.

The four tabs are synthesized at load, never authored: their ids are the
largest authored tab id + 1 to + 4 (in tab order), which the roster
projection in simulation derives from the same tables, and their names in
the extra-goods list are reserved names no authored table can carry. The
first three tab labels are the game's own category strings (the stall
network's "Elixir", the item types' "Attribute stone" and "Alchemy
stone"); v1.150 has no degree 10 tab name (its SN_TAB_MAGICTABLET_n rows
stop at "6th degree"), so the fourth carries a port label the client
spells "10th degree" (port-shop-copy.ts).

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
// its four synthesized tabs travel under in an extra-goods list.
const (
	janganSmithTab       = "STORE_CH_SMITH_TAB3"
	janganGrocerTab      = "STORE_CH_ACCESSORY_TAB3"
	janganElixirsTab     = "OPENSRO_JANGAN_ELIXIRS"
	janganAttrStonesTab  = "OPENSRO_JANGAN_ATTRSTONES"
	janganMagicStonesTab = "OPENSRO_JANGAN_MAGICSTONES"
	janganD10StonesTab   = "OPENSRO_JANGAN_D10STONES"
)

// The synthesized tabs' ids above the largest authored tab id, in tab
// order. The roster projection (simulation/npcjanganshop.go) uses the
// same offsets.
const (
	janganElixirsOffset     = 1
	janganAttrStonesOffset  = 2
	janganMagicStonesOffset = 3
	janganD10StonesOffset   = 4
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
// tab; on the grocer they open the elixirs tab. Only the B grade is sold: the
// A grade's itemdata names archemy_reinforce_recipe_a.ddj, which the v1.150
// client does not ship, so its rows had no image.
var janganElixirGoods = []janganGood{
	{"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B", 1000000},
	{"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_SHIELD_B", 1000000},
	{"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_B", 1000000},
	{"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_B", 1000000},
}

// The Lucky Powders that close the elixirs tab (the powders of "elixirs
// and powders"), each at its own itemdata price. Degree 9: owner
// 2026-10-13. Degrees 10 and 11: owner 2026-11-25, after players found no
// shop selling them - no native gold shop carries a degree 10 or 11 powder,
// so degree 10-11 gear had no reinforcement elixir to buy.
var janganPowderGoods = []janganGood{
	{"ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09", 54469},
	{"ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_10", 63656},
	{"ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_11", 70875},
}

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

// The smith's stones (owner 2026-10-13, prices 2026-10-18; the degree 10
// Steady stone owner 2026-11-25 at the degree 9 price); they move
// to the grocer's alchemy stones tab behind hers, at the same prices,
// but for the degree 10 Steady stone, which the degree 10 tab sells.
var janganSmithMagicStones = []janganGood{
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_09", 5000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_SOLID_09", 5000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_SOLID_10", 5000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_ASTRAL_09", 50000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_09", 25000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_REPAIR_09", 5000000},
}

// The degree 10 stones tab (#4684, awaiting the owner's approval): every
// degree 10 stone the item table prices, at its degree 9 twin's price above
// (attribute and grocer magic stones 2,000,000; Luck and Steady 5,000,000;
// Astral 50,000,000; Athanasia 25,000,000). The table's other degree 10 magic stone rows
// (ABYSS, DEVIL, GUARD, HEAT, IRONWALL, SHARP, STRONGMIND, STUPOR) are
// priced 1, which no shop good is (their textdataname rows also leave the
// English name empty), and no degree 10 Repair stone exists.
var janganD10StoneGoods = []janganGood{
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_PA_10", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_PASTR_10", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_MA_10", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_MAINT_10", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_CRITICAL_10", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_HR_10", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_PD_10", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_PDSTR_10", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_MD_10", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_MDINT_10", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_ER_10", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_BR_10", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_PAR_10", 2000000},
	{"ITEM_ETC_ARCHEMY_ATTRSTONE_MAR_10", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_STR_10", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_INT_10", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_DUR_10", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_HR_10", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_EVADE_BLOCK_10", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_EVADE_CRITICAL_10", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_ER_10", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_HP_10", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_MP_10", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_FROSTBITE_10", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_ESHOCK_10", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_BURN_10", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_POISON_10", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_ZOMBIE_10", 2000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_10", 5000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_SOLID_10", 5000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_ASTRAL_10", 50000000},
	{"ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_10", 25000000},
}

/*
================
JanganAlchemyShopFromEnv

The switch alone: unset or on is the owner's default-on; anything else
but an off value is a boot error naming the setting.
================
*/
func JanganAlchemyShopFromEnv() (bool, error) {
	return switchFromEnv(EnvJanganAlchemyShop, true)
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

The four tabs' custom goods as extra goods on the reserved tab names:
the elixir recipes and the powders, the attribute stones, the magic
stones (the grocer's, then the smith's at their prices) and the degree
10 stones. A smith's stone the degree 10 tab sells (Steady 10) is sold
there alone.
================
*/
func janganAlchemyTabGoods() []ExtraGood {
	onD10Tab := make(map[string]bool, len(janganD10StoneGoods))
	for _, good := range janganD10StoneGoods {
		onD10Tab[good.codename] = true
	}
	var goods []ExtraGood
	for _, good := range append(append([]janganGood{}, janganElixirGoods...), janganPowderGoods...) {
		goods = append(goods, ExtraGood{Tab: janganElixirsTab, Codename: good.codename, Price: good.price})
	}
	for _, good := range janganAttrStoneGoods {
		goods = append(goods, ExtraGood{Tab: janganAttrStonesTab, Codename: good.codename, Price: good.price})
	}
	for _, good := range append(append([]janganGood{}, janganGrocerMagicStones...), janganSmithMagicStones...) {
		if onD10Tab[good.codename] {
			continue
		}
		goods = append(goods, ExtraGood{Tab: janganMagicStonesTab, Codename: good.codename, Price: good.price})
	}
	for _, good := range janganD10StoneGoods {
		goods = append(goods, ExtraGood{Tab: janganD10StonesTab, Codename: good.codename, Price: good.price})
	}
	return goods
}

/*
================
regroupJanganAlchemy

The default list regrouped: the smith's and grocer's alchemy entries
come out (the grocer's powder with them; the other grocers keep theirs)
and the four tabs' goods go in: the same goods, Steady 10 moved to the
degree 10 tab, and the degree 10 stones that tab adds.
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
		case janganElixirsTab, janganAttrStonesTab, janganMagicStonesTab, janganD10StonesTab:
			return true
		}
	}
	return false
}

/*
================
registerJanganAlchemyTabs

Gives the catalogue the four synthesized tabs: ids above every authored
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
	c.tabIDs[janganElixirsTab] = maxID + janganElixirsOffset
	c.tabIDs[janganAttrStonesTab] = maxID + janganAttrStonesOffset
	c.tabIDs[janganMagicStonesTab] = maxID + janganMagicStonesOffset
	c.tabIDs[janganD10StonesTab] = maxID + janganD10StonesOffset
	authored := append([]Offer(nil), c.Tabs[grocer]...)
	sort.Slice(authored, func(i, j int) bool { return authored[i].Slot < authored[j].Slot })
	for i := range authored {
		authored[i].Slot = uint8(i)
	}
	c.Tabs[maxID+janganElixirsOffset] = authored
	return nil
}
