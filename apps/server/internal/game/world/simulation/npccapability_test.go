/*
===========================================================================

npccapability_test.go - 4C6350's service chains and the v1.150 talk word

Expectations come from the native registrations and the client's menu
builder (5D9100), not from the tables under test.

===========================================================================
*/

package simulation

import "testing"

/*
================
TestServiceChainsRegisterTheNativeOptions
================
*/
func TestServiceChainsRegisterTheNativeOptions(t *testing.T) {
	cases := []struct {
		codename string
		want     []uint8
	}{
		{"NPC_CH_WAREHOUSE_M", []uint8{NpcServiceStorage, NpcServiceShop}},
		{"NPC_EU_SMITH", []uint8{NpcServiceRepair, NpcServiceMagicOption}},
		{"NPC_CH_ARMOR", []uint8{NpcServiceRepair}},
		{"NPC_KT_HORSE", []uint8{NpcServiceShop, NpcServiceStable}},
		{"NPC_CH_GENARAL_SP", []uint8{NpcServiceGuild, NpcServiceShop}},
		{"NPC_EU_ADVICE3", []uint8{NpcServiceReverseReturn}},
		{"NPC_CH_SOLDIER_EM1", []uint8{NpcServiceReverseReturn}},
		{"NPC_TD_THIEF_BUY", []uint8{NpcServiceSpecialTrade, NpcServiceThiefBuy}},
		// The general keeps the hunter guild beside his own option.
		{"NPC_CH_GENARAL_SW", []uint8{NpcServiceGeneral, NpcServiceJobHunter}},
		{"NPC_WC_DOCTOR", []uint8{NpcServiceJobTrader}},
		{"NPC_TD_THIEF_SELL", []uint8{NpcServiceJobThief}},
		{"NPC_RM_SPECIAL", []uint8{NpcServiceSpecialTrade}},
		{"NPC_CH_FORTRESS_SMITH1", []uint8{NpcServiceFortressSmith, NpcServiceRepair}},
		{"NPC_CH_FORTRESS_BATTLEAIDE1", []uint8{NpcServiceFortressAide, NpcServiceShop}},
		{"STRUCTURE_GATE_PULLEY_JA_02", []uint8{NpcServiceGatePulley}},
		{"NPC_BATTLE_ARENA_MANAGER", []uint8{NpcServiceArenaManager}},
		{"npc_siege_dungeon_teleport", []uint8{NpcServiceSiegeTeleport}},
		{"NPC_CH_POTION", nil},
	}
	for _, c := range cases {
		if got, want := NpcServicesForCodename(c.codename), NpcServices(0).With(c.want...); got != want {
			t.Errorf("%s services %#x, want %#x", c.codename, uint64(got), uint64(want))
		}
	}
}

/*
================
TestTalkWordShiftsEachOptionBelowItsNumber

The bits the client tests: option n is 1 << (n - 1); options past 32 have
no bit in the v1.150 word.
================
*/
func TestTalkWordShiftsEachOptionBelowItsNumber(t *testing.T) {
	for option, want := range map[uint8]uint32{
		NpcServiceShop: 0x1, NpcServiceStorage: 0x4, NpcServiceGuild: 0x4000, NpcServiceGachaMachine: 0x10000,
		NpcServiceJobTrader: 0x80000, NpcServiceJobHunter: 0x200000, NpcServiceTeleportGate: 0x8000000,
		NpcServiceReverseReturn: 0x20000000, NpcServiceGatePulley: 0x40000000, NpcServiceMagicOption: 0x80000000,
		NpcServiceArenaManager: 0,
	} {
		if got := NpcServices(0).With(option).TalkFlags(); got != want {
			t.Errorf("option %#x flags %#x, want %#x", option, got, want)
		}
	}
}

/*
================
TestResolvedTalkWordAdvertisesOnlyOwnedRows

A smith offers shop, talk, repair and the avatar magic grant. A guide
offers reverse return; fortress pulleys still have no response owner.
================
*/
func TestResolvedTalkWordAdvertisesOnlyOwnedRows(t *testing.T) {
	smith := NpcDef{Codename: "NPC_EU_SMITH", BaseSpeechSymbol: "SN_NPC_EU_SMITH_BS",
		NpcTalkStoreGroups: []NpcTalkStoreGroup{{StoreGroupID: 7495}}}
	smith.Services = ResolveNpcServices(smith)
	if got := ResolveNpcTalkFlags(smith); got != NpcTalkFlagShop|NpcTalkFlagTalk|NpcTalkFlagRepair|NpcTalkFlagMagicOption {
		t.Fatalf("smith flags %#x", got)
	}
	guide := NpcDef{Codename: "NPC_EU_ADVICE3", BaseSpeechSymbol: "SN_NPC_EU_ADVICE_BS"}
	guide.Services = ResolveNpcServices(guide)
	if got := ResolveNpcTalkFlags(guide); got != NpcTalkFlagTalk|NpcTalkFlagReverseReturn {
		t.Fatalf("guide flags %#x", got)
	}
	pulley := NpcDef{Codename: "STRUCTURE_GATE_PULLEY_JA_01"}
	pulley.Services = ResolveNpcServices(pulley)
	if ResolveNpcTalkFlags(pulley)&NpcTalkFlagGatePulley != 0 {
		t.Fatal("a pulley advertised the bit whose u16 tail is not written")
	}
}

/*
================
TestEveryStorageKeeperOffersStorage

4C6501's substring arm: every shipped storage keeper resolves shop|storage.
================
*/
func TestEveryStorageKeeperOffersStorage(t *testing.T) {
	for _, codename := range []string{
		"NPC_EU_WAREHOUSE", "NPC_CA_WAREHOUSE", "NPC_CH_WAREHOUSE_M", "NPC_CH_WAREHOUSE_W",
		"NPC_WC_WAREHOUSE_M", "NPC_WC_WAREHOUSE_W", "NPC_KT_WAREHOUSE",
	} {
		npc := NpcDef{Codename: codename}
		npc.Services = ResolveNpcServices(npc)
		if got := ResolveNpcTalkFlags(npc); got != NpcTalkFlagShop|NpcTalkFlagStorage {
			t.Errorf("%s resolves %#x, want 0x05", codename, got)
		}
	}
}

/*
================
TestJobGuildFollowsTheServiceSet
================
*/
func TestJobGuildFollowsTheServiceSet(t *testing.T) {
	for codename, want := range map[string]uint8{
		"NPC_CH_DOCTOR": 1, "NPC_TD_THIEF_SELL": 2, "NPC_KT_MINISTER": 3, "NPC_EU_SMITH": 0,
	} {
		if got := NpcJobGuild(NpcDef{Services: NpcServicesForCodename(codename)}); got != want {
			t.Errorf("%s job %d, want %d", codename, got, want)
		}
	}
}

/*
================
TestTownSpecialtyTradersSellTradeGoods

v1.150's Jangan, Donwhang and Hotan specialty traders resolve the special
trade bit next to the shop their store group grants, as SPECIAL2 does.
================
*/
func TestTownSpecialtyTradersSellTradeGoods(t *testing.T) {
	for _, codename := range []string{"NPC_CH_SPECIAL", "NPC_WC_SPECIAL", "NPC_KT_SPECIAL", "NPC_CH_SPECIAL2"} {
		npc := NpcDef{Codename: codename, NpcTalkStoreGroups: []NpcTalkStoreGroup{{StoreGroupID: 786}}}
		npc.Services = ResolveNpcServices(npc)
		if got := ResolveNpcTalkFlags(npc); got != NpcTalkFlagShop|NpcTalkFlagSpecialTrade {
			t.Errorf("%s resolves %#x, want shop|special trade", codename, got)
		}
	}
}
