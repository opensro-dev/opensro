/*
===========================================================================

extragoods.go - operator NPC shop goods beyond the v1.150 stock

Port-only, not native. An operator adds gold-priced goods to an NPC shop
tab with one setting instead of a code change (#589). The owner approved a
default list for the beta (2026-10-11, a #bug-reports suggestion): the
elixirs at 1,000,000 gold. Beta balance on 2026-10-13: the Luck, Steady,
Astral, Athanasia and Repair magic stones of degree 9 at Jangan's smith for
5,000,000 (the degree 1-3 Luck and Steady stones it sold before are gone),
the other degree 9 magic stones and every degree 9 attribute stone at
Jangan's grocer for 2,000,000, and the vigor potions at every herbalist for
their native price. F7: degree 9 Lucky Powder at every grocer for A_09's
native price.
"off" restores the shipped v1.150 stock exactly.

An entry is "shop tab : item codename : gold price". Each entry takes the
next free slot of its tab, after the authored goods, and is an ordinary
gold Offer built from a one-item template, so quotes, the purchase limit
and selling back follow the authored path. A malformed or unadmitted entry
stops the boot naming the setting: a typo never ships a different shop.

===========================================================================
*/
package commerce

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// EnvShopExtraGoods names the setting: "on", "off" or "tab:item:price,...".
const EnvShopExtraGoods = "SRO_SHOP_EXTRA_GOODS"

// shopExtraGoodsDefault is what "on" means (owner, 2026-10-11): elixirs at
// 1M in the Jangan smith's third tab after its arrows. Owner, 2026-10-13:
// the smith's Luck, Steady, Astral, Athanasia and Repair stones are degree 9
// only (degrees 1-3 removed, agreed with the reporter); 2026-10-18: Astral is
// 50M, Athanasia (Immortal) 25M and Luck, Steady and Repair 5M; the other
// degree 9 stones at Jangan's grocer for 2M; the vigor potions at every
// herbalist. The two Jangan segments are built from the alchemy shop's
// goods tables (alchemyshop.go), which the three-tab regroup reads too.
var shopExtraGoodsDefault = "" +
	janganSmithDefault() +
	janganGrocerDefault() +
	"STORE_CH_POTION_TAB1:ITEM_ETC_ALL_POTION_01:100," +
	"STORE_CH_POTION_TAB1:ITEM_ETC_ALL_POTION_02:180," +
	"STORE_CH_POTION_TAB1:ITEM_ETC_ALL_POTION_03:320," +
	"STORE_CH_POTION_TAB1:ITEM_ETC_ALL_POTION_04:600," +
	"STORE_CH_POTION_TAB1:ITEM_ETC_ALL_POTION_05:900," +
	"STORE_WC_POTION_TAB1:ITEM_ETC_ALL_POTION_01:100," +
	"STORE_WC_POTION_TAB1:ITEM_ETC_ALL_POTION_02:180," +
	"STORE_WC_POTION_TAB1:ITEM_ETC_ALL_POTION_03:320," +
	"STORE_WC_POTION_TAB1:ITEM_ETC_ALL_POTION_04:600," +
	"STORE_WC_POTION_TAB1:ITEM_ETC_ALL_POTION_05:900," +
	"STORE_KT_POTION_TAB1:ITEM_ETC_ALL_POTION_01:100," +
	"STORE_KT_POTION_TAB1:ITEM_ETC_ALL_POTION_02:180," +
	"STORE_KT_POTION_TAB1:ITEM_ETC_ALL_POTION_03:320," +
	"STORE_KT_POTION_TAB1:ITEM_ETC_ALL_POTION_04:600," +
	"STORE_KT_POTION_TAB1:ITEM_ETC_ALL_POTION_05:900," +
	"STORE_EU_POTION_TAB1:ITEM_ETC_ALL_POTION_01:100," +
	"STORE_EU_POTION_TAB1:ITEM_ETC_ALL_POTION_02:180," +
	"STORE_EU_POTION_TAB1:ITEM_ETC_ALL_POTION_03:320," +
	"STORE_EU_POTION_TAB1:ITEM_ETC_ALL_POTION_04:600," +
	"STORE_EU_POTION_TAB1:ITEM_ETC_ALL_POTION_05:900," +
	"STORE_CA_POTION_TAB1:ITEM_ETC_ALL_POTION_01:100," +
	"STORE_CA_POTION_TAB1:ITEM_ETC_ALL_POTION_02:180," +
	"STORE_CA_POTION_TAB1:ITEM_ETC_ALL_POTION_03:320," +
	"STORE_CA_POTION_TAB1:ITEM_ETC_ALL_POTION_04:600," +
	"STORE_CA_POTION_TAB1:ITEM_ETC_ALL_POTION_05:900," +
	// Owner, 2026-10-13 (F7, Lucky Powder option 1): degree 9 Lucky Powder
	// at every grocer's alchemy tab (NPC_*_ACCESSORY, SN_TAB_ALCHEMY) for
	// A_09's native price, 54,469 (refpricepolicyofitem, its Hotan package).
	// Hotan's two grocer tabs already sell it natively. The large powder
	// (B_09) waits for F8 (owner, 2026-10-14).
	"STORE_CH_ACCESSORY_TAB3:ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09:54469," +
	"STORE_WC_ACCESSORY_TAB3:ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09:54469," +
	"STORE_EU_ACCESSORY_TAB3:ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09:54469," +
	"STORE_CA_ACCESSORY_TAB3:ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09:54469"

// maxShopSlot is the last slot a tab can address: the offer slot is a byte.
const maxShopSlot = math.MaxUint8

/*
================
ExtraGood

One configured offer: the authored tab name, the item and its gold price.
================
*/
type ExtraGood struct {
	Tab      string
	Codename string
	Price    uint64
}

/*
================
ParseExtraGoods

"" and "off" are the native stock (nil); "on" is shopExtraGoodsDefault;
otherwise comma-separated tab:item:price entries, price 1..MaxUint32.
================
*/
func ParseExtraGoods(text string) ([]ExtraGood, error) {
	text = strings.TrimSpace(text)
	switch strings.ToLower(text) {
	case "", "off", "0", "false":
		return nil, nil
	case "on", "1", "true":
		text = shopExtraGoodsDefault
	}
	var goods []ExtraGood
	for _, field := range strings.Split(text, ",") {
		parts := strings.Split(strings.TrimSpace(field), ":")
		if len(parts) != 3 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("%s: %q is not tab:item:price", EnvShopExtraGoods, field)
		}
		price, err := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64)
		if err != nil || price == 0 || price > math.MaxUint32 {
			return nil, fmt.Errorf("%s: %q price is not 1..%d", EnvShopExtraGoods, field, uint64(math.MaxUint32))
		}
		goods = append(goods, ExtraGood{Tab: strings.TrimSpace(parts[0]), Codename: strings.TrimSpace(parts[1]), Price: price})
	}
	return goods, nil
}

/*
================
ExtraGoodsFromEnv

The environment's goods: the parsed setting, regrouped onto Jangan's
three alchemy tabs while SRO_JANGAN_ALCHEMY_SHOP is on and the default
list is in force (alchemyshop.go).
================
*/
func ExtraGoodsFromEnv() ([]ExtraGood, error) {
	raw := os.Getenv(EnvShopExtraGoods)
	goods, err := ParseExtraGoods(raw)
	if err != nil {
		return nil, err
	}
	active, err := janganAlchemyActive(raw)
	if err != nil {
		return nil, err
	}
	if active {
		goods = regroupJanganAlchemy(goods)
	}
	return goods, nil
}

/*
================
LoadWithExtraGoods

The shard's shop catalogue: the shipped stock (Load) plus the operator's
extra goods (AddExtraGoods). The GameWorld sells from it and sro-catalogue
publishes it, so the site lists exactly what the shard sells. Nil extra is
the v1.150 stock.
================
*/
func LoadWithExtraGoods(dir string, refs enterworld.ItemRefSource, extra []ExtraGood) (*Catalog, error) {
	c, err := Load(dir, refs)
	if err != nil {
		return nil, err
	}
	if hasJanganAlchemyGoods(extra) {
		if err := c.registerJanganAlchemyTabs(); err != nil {
			return nil, err
		}
	}
	if err := c.AddExtraGoods(extra, refs); err != nil {
		return nil, err
	}
	return c, nil
}

/*
================
AddExtraGoods

Appends each good to its tab at the tab's next free slot. Every check
Load applies to an authored content applies here (packageContent,
merchandiseKind); a failure names the setting and changes nothing.
================
*/
func (c *Catalog) AddExtraGoods(goods []ExtraGood, refs enterworld.ItemRefSource) error {
	if len(goods) == 0 {
		return nil
	}
	if refs == nil {
		return fmt.Errorf("%s: item references unavailable", EnvShopExtraGoods)
	}
	added := map[int32][]Offer{}
	next := map[int32]int{}
	for _, good := range goods {
		tab, known := c.tabIDs[good.Tab]
		if !known {
			return fmt.Errorf("%s: unknown shop tab %s", EnvShopExtraGoods, good.Tab)
		}
		content, err := packageContent(extraTemplate(good.Codename), refs)
		if err != nil {
			return fmt.Errorf("%s: %s: %w", EnvShopExtraGoods, good.Codename, err)
		}
		if !merchandiseKind(content.Ref.TypeIDs) {
			return fmt.Errorf("%s: %s is not an item NPC shops sell", EnvShopExtraGoods, good.Codename)
		}
		if _, seen := next[tab]; !seen {
			next[tab] = 0
			for _, offer := range c.Tabs[tab] {
				next[tab] = max(next[tab], int(offer.Slot)+1)
			}
		}
		if next[tab] > maxShopSlot {
			return fmt.Errorf("%s: shop tab %s has no free slot for %s", EnvShopExtraGoods, good.Tab, good.Codename)
		}
		added[tab] = append(added[tab], Offer{Slot: uint8(next[tab]), Ref: content.Ref, Price: good.Price,
			Currency: PaymentGold, Stack: content.Stack, Contents: []Content{content}})
		next[tab]++
	}
	for tab, offers := range added {
		c.Tabs[tab] = append(c.Tabs[tab], offers...)
	}
	return nil
}

/*
================
extraTemplate

A refscrapofpackageitem row selling one plain item: no plus, variance,
data or magic options, so packageContent applies its own stack and
durability rules exactly as for an authored package.
================
*/
func extraTemplate(codename string) []string {
	row := make([]string, 8+wire.MaxMagicOptionsPerItem)
	for i := range row {
		row[i] = "0"
	}
	row[0], row[3] = "1", codename
	return row
}
