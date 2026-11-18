/*
===========================================================================

npcpvpshop_test.go - Samarkand's merchants carry the PVP tabs

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/pvpserver"
)

func TestApplyPvpShopAddsTheMerchantsTabs(t *testing.T) {
	smith := NpcDef{Codename: "NPC_CA_SMITH", NpcTalkStoreGroups: []NpcTalkStoreGroup{
		{StoreGroupID: 7530, Tabs: []NpcTalkStoreTab{{TabID: 2037, LabelSymbol: "SN_TAB_WEAPON"}}},
	}}
	tabs := []pvpserver.ShopTab{
		{Npc: "NPC_CA_SMITH", TabID: 6144, GroupID: 2714, Label: "SN_TAB_WEAPON", GroupLabel: "SN_STORE_SMITH_EU_GROUP1"},
		{Npc: "NPC_CA_ARMOR", TabID: 6146, GroupID: 2715, Label: "SN_TAB_HEAVYARMOR_EU"},
	}

	applyPvpShop(&smith, tabs)

	got := smith.NpcTalkStoreGroups[0].Tabs
	if len(got) != 2 || got[0].TabID != 2037 {
		t.Fatalf("tabs %+v: the native tab must stay first and the armorer's tab must not land here", got)
	}
	want := NpcTalkStoreTab{TabID: 6144, LabelSymbol: "SN_TAB_WEAPON", GroupID: 2714, GroupLabelSymbol: "SN_STORE_SMITH_EU_GROUP1"}
	if got[1] != want {
		t.Fatalf("PVP tab %+v, want %+v", got[1], want)
	}
}

func TestApplyPvpShopLeavesOtherMerchantsAlone(t *testing.T) {
	potion := NpcDef{Codename: "NPC_CA_POTION", NpcTalkStoreGroups: []NpcTalkStoreGroup{
		{StoreGroupID: 7532, Tabs: []NpcTalkStoreTab{{TabID: 2049}}},
	}}
	applyPvpShop(&potion, []pvpserver.ShopTab{{Npc: "NPC_CA_SMITH", TabID: 6144}})
	if len(potion.NpcTalkStoreGroups[0].Tabs) != 1 {
		t.Fatalf("tabs %+v", potion.NpcTalkStoreGroups[0].Tabs)
	}
}

func TestApplyPvpShopAddsTheTabsOnce(t *testing.T) {
	armorer := NpcDef{Codename: "NPC_CA_ARMOR", NpcTalkStoreGroups: []NpcTalkStoreGroup{
		{StoreGroupID: 7531, Tabs: []NpcTalkStoreTab{{TabID: 2040}}},
		{StoreGroupID: 7531, Tabs: []NpcTalkStoreTab{{TabID: 2043}}},
	}}
	applyPvpShop(&armorer, []pvpserver.ShopTab{{Npc: "NPC_CA_ARMOR", TabID: 6146, GroupID: 2715}})
	count := 0
	for _, group := range armorer.NpcTalkStoreGroups {
		for _, tab := range group.Tabs {
			if tab.TabID == 6146 {
				count++
			}
		}
	}
	if count != 1 {
		t.Fatalf("the PVP tab appears %d times", count)
	}
}

func TestMovePvpTraderStandsTheMerchantAtItsSpot(t *testing.T) {
	smith := NpcDef{Codename: "NPC_CA_SMITH", Spawn: Spawn{RegionID: 27499, X: 1762, Y: 180, Z: 809}}
	keeper := NpcDef{Codename: "NPC_CA_WAREHOUSE", Spawn: Spawn{RegionID: 27244, X: 563, Y: 180, Z: 1131}}
	before := keeper.Spawn

	movePvpTrader(&smith)
	movePvpTrader(&keeper)

	region, x, y, z, angle, _ := pvpserver.TraderSpot("NPC_CA_SMITH")
	if smith.Spawn != (Spawn{RegionID: region, X: x, Y: y, Z: z, Angle: angle}) {
		t.Fatalf("smith stands at %+v", smith.Spawn)
	}
	if keeper.Spawn != before {
		t.Fatalf("the warehouse keeper moved to %+v", keeper.Spawn)
	}
}
