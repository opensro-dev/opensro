/*
===========================================================================

npcpvpshop.go - Samarkand's merchants on the PVP shard

Port-only, not native. On the PVP shard the smith, the armorer, the
accessory merchant and the potion merchant of Samarkand carry the tabs
pvpserver.ShopLayout lays out, after their own, and stand beside the
arrival point; the catalogue (commerce/pvpshop.go) holds the offers
behind the same ids. Off, the roster is exactly as authored.

===========================================================================
*/

package simulation

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/pvpserver"
)

/*
================
applyPvpShop

Appends the layout's tabs that name npc to its last store group.
================
*/
func applyPvpShop(npc *NpcDef, tabs []pvpserver.ShopTab) {
	var own []NpcTalkStoreTab
	for _, tab := range tabs {
		if tab.Npc != npc.Codename {
			continue
		}
		own = append(own, NpcTalkStoreTab{TabID: tab.TabID, LabelSymbol: tab.Label,
			GroupID: tab.GroupID, GroupLabelSymbol: tab.GroupLabel})
	}
	if len(own) == 0 || len(npc.NpcTalkStoreGroups) == 0 {
		return
	}
	// Once, after every native tab: the client numbers tabs across all of
	// the merchant's store groups, so a copy per group would list them twice.
	last := len(npc.NpcTalkStoreGroups) - 1
	npc.NpcTalkStoreGroups[last].Tabs = append(npc.NpcTalkStoreGroups[last].Tabs, own...)
}

/*
================
applyPvpShops

The roster's pass: the layout read once from the textdata the roster was
built from. A layout that cannot be read leaves the merchants native and
says so; the catalogue refuses the same failure at startup.
================
*/
func applyPvpShops(roster []NpcDef, textdataDir string) {
	if !pvpserver.EnabledFromEnv() {
		return
	}
	tabs, err := pvpserver.ShopLayout(textdataDir)
	if err != nil {
		log.WithError(err).Warn("pvp shop: layout unreadable, Samarkand's merchants stay native")
		return
	}
	for i := range roster {
		applyPvpShop(&roster[i], tabs)
		movePvpTrader(&roster[i])
	}
}

/*
================
movePvpTrader

Stands a PVP merchant beside the arrival point (pvpserver.TraderSpot),
so a character created in Samarkand finds every shop in front of it.
The roster is built before any interest index reads a position, and the
client places an NPC where its spawn says, so this one move is enough.
================
*/
func movePvpTrader(npc *NpcDef) {
	region, x, y, z, angle, ok := pvpserver.TraderSpot(npc.Codename)
	if !ok {
		return
	}
	npc.Spawn = Spawn{RegionID: region, X: x, Y: y, Z: z, Angle: angle}
}
