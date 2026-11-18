/*
===========================================================================

pvpshop.go - the PVP shard's Samarkand tabs in the catalogue

Port-only, not native. On the PVP shard (pvpserver.EnabledFromEnv) the
catalogue gains the tabs pvpserver.ShopLayout lays out: every good at
pvpserver.ShopPrice gold, gear at its tab's plus (+12), stones plain.
The NPC roster reads the same layout and puts the tabs on the merchants,
so the ids agree by construction. Off, nothing here runs.

===========================================================================
*/

package commerce

import (
	"fmt"
	"strconv"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/pvpserver"
)

// pvpTabPrefix names the synthesized tabs in the tab index; no authored
// tab can carry it.
const pvpTabPrefix = "OPENSRO_PVP_"

// packageColPlus is refscrapofpackageitem's plus column (packageContent).
const packageColPlus = 4

/*
================
RegisterPvpTabs

Adds each tab's goods at slots 0.., priced pvpserver.ShopPrice. Every
check an authored package gets applies (packageContent, merchandiseKind);
an item the data lacks refuses the load.
================
*/
func (c *Catalog) RegisterPvpTabs(tabs []pvpserver.ShopTab, refs enterworld.ItemRefSource) error {
	if refs == nil {
		return fmt.Errorf("%s: item references unavailable", pvpserver.EnvPvpServer)
	}
	for _, tab := range tabs {
		offers := make([]Offer, 0, len(tab.Codenames))
		for slot, codename := range tab.Codenames {
			row := extraTemplate(codename)
			row[packageColPlus] = strconv.Itoa(int(tab.Plus))
			content, err := packageContent(row, refs)
			if err != nil {
				return fmt.Errorf("%s: %s: %w", pvpserver.EnvPvpServer, codename, err)
			}
			if !merchandiseKind(content.Ref.TypeIDs) {
				return fmt.Errorf("%s: %s is not an item NPC shops sell", pvpserver.EnvPvpServer, codename)
			}
			offers = append(offers, Offer{Slot: uint8(slot), Ref: content.Ref, Price: pvpserver.ShopPrice,
				Currency: PaymentGold, Stack: content.Stack, Contents: []Content{content}})
		}
		c.tabIDs[pvpTabPrefix+strconv.Itoa(int(tab.TabID))] = tab.TabID
		c.Tabs[tab.TabID] = offers
	}
	return nil
}
