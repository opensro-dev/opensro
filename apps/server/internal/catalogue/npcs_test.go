/*
===========================================================================

npcs_test.go - roster rows, services and shop goods

===========================================================================
*/
package catalogue

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestNpcRowsJoinPositionsServicesAndGoods
================
*/
func TestNpcRowsJoinPositionsServicesAndGoods(t *testing.T) {
	smith := simulation.NpcDef{RefObjID: 2003, Codename: "NPC_CH_SMITH", Name: "Blacksmith", AuthoredSpawn: true,
		Services:           simulation.NpcServices(0).With(simulation.NpcServiceShop, simulation.NpcServiceRepair),
		NpcTalkStoreGroups: []simulation.NpcTalkStoreGroup{{Tabs: []simulation.NpcTalkStoreTab{{TabID: 7, LabelSymbol: "SN_TAB_WEAPON"}}}}}
	second := smith
	second.Spawn = simulation.Spawn{RegionID: 25001}
	gate := simulation.NpcDef{RefObjID: 9000, Codename: "GATE", Teleport: &simulation.TeleportGateBounds{}}
	sword := &enterworld.ItemRef{RefObjID: 107}
	shops := &commerce.Catalog{Tabs: map[int32][]commerce.Offer{7: {
		{Ref: sword, Price: 890, Currency: commerce.PaymentGold, Stack: 1},
		{Ref: &enterworld.ItemRef{RefObjID: 9}, Price: 5, Currency: commerce.PaymentHonor, Stack: 10},
	}}}
	npcs := BuildNpcs(NpcSource{Roster: []simulation.NpcDef{smith, gate, second}, Shops: shops})
	if len(npcs) != 1 {
		t.Fatalf("exported %d rows, want the smith once (the gate is not an NPC)", len(npcs))
	}
	npc := npcs[0]
	if len(npc.Positions) != 2 || strings.Join(npc.Services, ",") != "shop,repair" {
		t.Fatalf("smith %+v", npc)
	}
	goods := npc.Tabs[0].Goods
	if len(goods) != 2 || goods[0].Price != 890 || goods[0].Currency != "gold" || goods[0].Stack != 0 ||
		goods[1].Currency != "honor" || goods[1].Stack != 10 {
		t.Fatalf("goods %+v", goods)
	}
}

/*
================
TestTradeListsOnlyFixedRateQuotations

The sale path refuses a quotation whose bounds differ, so it is not listed;
nor is a quotation of a trader v1.150 does not place.
================
*/
func TestTradeListsOnlyFixedRateQuotations(t *testing.T) {
	silk := &enterworld.ItemRef{RefObjID: 2151, NativeFields: enterworld.NewNativeFields(map[string]float64{"price": 348})}
	fixed := commerce.TradeQuotation{Base: 1.1, Lower: 1.1, Upper: 1.1, BaseStock: 100, Step: 1, Stock: 100}
	ranged := fixed
	ranged.Upper = 1.5
	quotes := BuildTrade(map[[2]uint32]commerce.TradeQuotation{{2010, 2151}: fixed, {2011, 2151}: ranged, {26794, 2151}: fixed},
		func(id uint32) *enterworld.ItemRef { return silk }, func(id uint32) bool { return id != 26794 })
	if len(quotes) != 1 || quotes[0].NpcID != 2010 || quotes[0].SellPrice <= quotes[0].BasePrice ||
		quotes[0].Profit != int64(quotes[0].SellPrice)-348 {
		t.Fatalf("quotes %+v", quotes)
	}
	if ThiefRate() != 0.75 {
		t.Fatalf("thief rate %v", ThiefRate())
	}
}

/*
================
TestBonusSourcesListTheReinforcementBuffs

Skills with a reinforcement bonus are listed with it; others are not.
================
*/
func TestBonusSourcesListTheReinforcementBuffs(t *testing.T) {
	rows := []enterworld.SkillRow{{ID: 7092, Codename: "SKILL_MALL_PRE_APRU_4W_01", AlchemyReinforceBonus: 5}, {ID: 3, Codename: "SKILL_CH_SWORD"}}
	sources := bonusSources(AlchemySource{Skills: rows})
	if len(sources) != 1 || sources[0].ID != 7092 || sources[0].Bonus != 5 || sources[0].Kind != "skill" {
		t.Fatalf("sources %+v", sources)
	}
}
