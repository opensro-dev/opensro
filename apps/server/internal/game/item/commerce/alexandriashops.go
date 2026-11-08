/*
===========================================================================

alexandriashops.go - hiding degree-11 goods in Alexandria's own shops

Port-only, not native. The original game sold degree-11 goods in
Alexandria's own NPC shops (opensro-explore's research on #2842,
from the official data): in iSRO-R's SHARD backup the Alexandria
smith's active stock is degree-11 weapons and shields only (its
degree-10 rows are all Service=0), the armourer's tabs are all
degree-11 set armour, and the accessory shop sells degree-11
jewellery with degree-11 alchemy on its third tab; the vSRO server
database and client textdata agree (armour degree-11 only, no
Alexandria smith, degree-11 alchemy active). Our v1.150 baseline
has no Alexandria at all - the port's city is imported vSRO-era
data - so degree-11 goods on the city's tabs are the native stock,
and selling them stays the default here.

An operator who wants degree-11 gear to come from grinding only
sets SRO_ALEXANDRIA_HIDE_D11_GOODS=on: the GameWorld then withholds
every degree-11 offer from the city's own tabs (STORE_SD_*) at
catalogue load, whatever the built tables carry - the Alexandria
variant's shop passes stock those tabs from any cap-110 items the
build imported (#1748, #2842). Flipping the switch needs no data
rebuild; a restart re-reads it. The Khotan stores the city's were
copied from (STORE_KT_*) are not Alexandria tabs and keep their
stock, exactly as the build passes leave them.

===========================================================================
*/
package commerce

import (
	"fmt"
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// EnvAlexandriaHideD11Goods names the switch: "on" hides the goods,
// "off" (the default) is the native stock.
const EnvAlexandriaHideD11Goods = "SRO_ALEXANDRIA_HIDE_D11_GOODS"

// alexandriaStoreTabPrefix names the tabs of the city's own stores.
// Any other city's tab, Khotan's included, is outside the switch.
const alexandriaStoreTabPrefix = "STORE_SD_"

// alexandriaD11Degree is the degree the switch gates.
const alexandriaD11Degree = 11

/*
================
AlexandriaHideD11GoodsFromEnv

The switch alone: unset or off sells the goods (the native stock);
anything else but an on value is a boot error naming the setting, so
a typo cannot silently open or close the shops.
================
*/
func AlexandriaHideD11GoodsFromEnv() (bool, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvAlexandriaHideD11Goods))) {
	case "1", "on", "true":
		return true, nil
	case "", "0", "off", "false":
		return false, nil
	}
	return false, fmt.Errorf("%s=%q: want on or off", EnvAlexandriaHideD11Goods, os.Getenv(EnvAlexandriaHideD11Goods))
}

/*
================
Catalog.WithholdAlexandriaD11

Removes every degree-11 offer from Alexandria's own tabs; the count
is the offers removed. The removed offers' slots stay unused, as
WithholdGoods leaves them: every other offer keeps the tab and slot
the built refshopgoods gives it.
================
*/
func (c *Catalog) WithholdAlexandriaD11() int {
	removed := 0
	for name, tab := range c.tabIDs {
		if !strings.HasPrefix(name, alexandriaStoreTabPrefix) {
			continue
		}
		offers := c.Tabs[tab]
		kept := offers[:0]
		for _, offer := range offers {
			if offerHolds(offer, isAlexandriaD11) {
				removed++
				continue
			}
			kept = append(kept, offer)
		}
		c.Tabs[tab] = kept
	}
	return removed
}

/*
================
isAlexandriaD11

A degree-11 item, by the server's one degree rule (enterworld.ItemDegree):
an equipment piece's degree, or an alchemy stone's class.
================
*/
func isAlexandriaD11(ref *enterworld.ItemRef) bool {
	return ref != nil && enterworld.ItemDegree(ref) >= alexandriaD11Degree
}
