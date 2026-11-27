/*
===========================================================================

npcjanganshop.go - Jangan's grocer as one alchemy shop (port-only, not native)

#1850 (owner, 2026-10-28, a player's request): while SRO_JANGAN_ALCHEMY_SHOP
is on (the default) and the extra-goods default list is in force, Jangan's
grocer carries the name "OpenSRO Alchemy - Jangan" and her authored alchemy
tab is replaced by the feature's four tabs (the fourth, the degree 10
stones, #4684), in their own tab group below the native ones:
four is the most tabs a shop group shows (client merchant-branches.ts).
commerce stocks the same four tabs from the same goods tables
(commerce/alchemyshop.go); the tab ids are the largest authored tab id
+ 1 to + 4 in tab order, derived here from the roster's tables and there
from the shop catalogue's, so the two always agree. The off
value of either setting restores the native name and tabs.

The settings are read here, not in commerce, because the roster cannot
import the shop catalogue; the commerce side reads the same pair, refuses
a malformed switch at boot, and the tests in both packages pin the same
answers for every combination.

===========================================================================
*/
package simulation

import (
	"os"
	"strings"
)

// The feature's settings. commerce.EnvJanganAlchemyShop and
// commerce.EnvShopExtraGoods name the same variables.
const (
	envJanganAlchemyShop = "SRO_JANGAN_ALCHEMY_SHOP"
	envShopExtraGoods    = "SRO_SHOP_EXTRA_GOODS"
)

// janganGrocerCodename is the Jangan grocer's authored codename.
const janganGrocerCodename = "NPC_CH_ACCESSORY"

// janganAlchemyName is the grocer's shop name while the feature is on.
const janganAlchemyName = "OpenSRO Alchemy - Jangan"

// The native alchemy tab's label, and the four tabs' own labels: the
// game's category strings (the stall network's "Elixir", the item types'
// "Attribute stone" and "Alchemy stone"), and for the degree 10 stones a
// port label the client spells "10th degree" (v1.150's degree tab names,
// SN_TAB_MAGICTABLET_n, stop at the 6th; client port-shop-copy.ts), under
// a group of the same "Alchemy" label.
const (
	janganNativeAlchemyLabel = "SN_TAB_ALCHEMY"
	janganAlchemyGroupLabel  = "SN_TAB_ALCHEMY"
	janganElixirsLabel       = "UIIT_CTL_WK_ALCHEMY_ELIXIR"
	janganAttrStonesLabel    = "UIIT_STT_ITEM_TYPE_ALCHEMY_ATTRSTONE"
	janganMagicStonesLabel   = "UIIT_STT_ITEM_TYPE_ALCHEMY_MAGICSTONE"
	janganD10StonesLabel     = "UIIT_PORT_ALCHEMY_D10_STONES"
)

// The synthesized tabs' ids above the largest authored tab id, in tab
// order; commerce/alchemyshop.go numbers the catalogue's the same way.
const (
	janganElixirsOffset     = 1
	janganAttrStonesOffset  = 2
	janganMagicStonesOffset = 3
	janganD10StonesOffset   = 4
)

// janganAlchemyGroupOffset puts the tabs' group above the largest authored
// tab-group id.
const janganAlchemyGroupOffset = 1

/*
================
janganAlchemyShopActive

Whether the regroup applies: the feature's switch on (unset is on) and
the extra-goods default list in force. A malformed switch reads as off;
the GameWorld refuses that value at boot through the commerce load, so
the roster's reading never serves.
================
*/
func janganAlchemyShopActive() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envJanganAlchemyShop))) {
	case "", "1", "on", "true":
	case "0", "off", "false":
		return false
	default:
		return false
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envShopExtraGoods))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
applyJanganAlchemyShop

The grocer's row under the feature: the shop name, and every store
group's native alchemy tab replaced by the four synthesized tabs (in
tab order, one new group above the largest authored group id).
================
*/
func applyJanganAlchemyShop(npc *NpcDef, maxTabID int32, maxGroupID int32) {
	npc.Name = janganAlchemyName
	synthesized := []NpcTalkStoreTab{
		{TabID: maxTabID + janganElixirsOffset, LabelSymbol: janganElixirsLabel,
			GroupID: maxGroupID + janganAlchemyGroupOffset, GroupLabelSymbol: janganAlchemyGroupLabel},
		{TabID: maxTabID + janganAttrStonesOffset, LabelSymbol: janganAttrStonesLabel,
			GroupID: maxGroupID + janganAlchemyGroupOffset, GroupLabelSymbol: janganAlchemyGroupLabel},
		{TabID: maxTabID + janganMagicStonesOffset, LabelSymbol: janganMagicStonesLabel,
			GroupID: maxGroupID + janganAlchemyGroupOffset, GroupLabelSymbol: janganAlchemyGroupLabel},
		{TabID: maxTabID + janganD10StonesOffset, LabelSymbol: janganD10StonesLabel,
			GroupID: maxGroupID + janganAlchemyGroupOffset, GroupLabelSymbol: janganAlchemyGroupLabel},
	}
	for gi := range npc.NpcTalkStoreGroups {
		tabs := make([]NpcTalkStoreTab, 0, len(npc.NpcTalkStoreGroups[gi].Tabs)+len(synthesized))
		for _, tab := range npc.NpcTalkStoreGroups[gi].Tabs {
			if tab.LabelSymbol != janganNativeAlchemyLabel {
				tabs = append(tabs, tab)
			}
		}
		npc.NpcTalkStoreGroups[gi].Tabs = append(tabs, synthesized...)
	}
}
