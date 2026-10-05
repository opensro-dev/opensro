/*
===========================================================================

npcservice.go - the NPC service set CGObjNPC_SpawnAndConfigureServices builds

SR_GameServer 4C6350 gives every spawned NPC a set of service option bytes
(CGObjNPC_AddService 4C3F50 into the set at +0x130) by testing its codename
with strstr in a fixed order of else-if chains. Handlers check one option
(CGObj_HasService 484DF0): 3 storage (0x703C), 4 repair (4C7A10), 0xF guild
storage (4C7DA0), 0xC special trade (4C90D0), 0x1C/0x1E teleport (4F2B50).

The select answer carries the set. v1.188 writes it as a count and option
bytes (CGObjNPC_WriteSelectInfo 4A95A0); the v1.150 client reads a u32
(CPSMission_OnNpcSelectResponse0xB45A 764C60) in which option n is bit
1 << (n - 1). Every point both binaries pin agrees: storage 3 = 0x4 (menu
row 3), guild 0xF = 0x4000, gacha 0x11/0x12 = 0x10000/0x20000, job guilds
0x14..0x16 = 0x80000..0x200000, teleport gate 0x1C = 0x8000000, reverse
return 0x1E = 0x20000000, gate pulley 0x1F = 0x40000000 with its u16 tail,
and the smith's 0x20 = 0x80000000, whose row 0x2F sends that very mask.

===========================================================================
*/

package simulation

import "strings"

// The service options 4C6350 registers, named by their handlers and the
// v1.150 talk rows their bits light.
const (
	NpcServiceShop             uint8 = 0x01
	NpcServiceTalk             uint8 = 0x02
	NpcServiceStorage          uint8 = 0x03
	NpcServiceRepair           uint8 = 0x04
	NpcServiceRecallPoint      uint8 = 0x07
	NpcServiceTeleport         uint8 = 0x08
	NpcServiceResurrectPoint   uint8 = 0x09
	NpcServiceStable           uint8 = 0x0b
	NpcServiceSpecialTrade     uint8 = 0x0c
	NpcServiceThiefBuy         uint8 = 0x0d
	NpcServiceGeneral          uint8 = 0x0e
	NpcServiceGuild            uint8 = 0x0f
	NpcServiceGachaMachine     uint8 = 0x11
	NpcServiceGachaOperator    uint8 = 0x12
	NpcServiceJobTrader        uint8 = 0x14
	NpcServiceJobThief         uint8 = 0x15
	NpcServiceJobHunter        uint8 = 0x16
	NpcServiceFortressManager  uint8 = 0x17
	NpcServiceFortressOfficial uint8 = 0x18
	NpcServiceFortressAide     uint8 = 0x19
	NpcServiceFortressSmith    uint8 = 0x1a
	NpcServiceFortressTrainer  uint8 = 0x1b
	NpcServiceTeleportGate     uint8 = 0x1c
	NpcServiceReverseReturn    uint8 = 0x1e
	NpcServiceGatePulley       uint8 = 0x1f
	NpcServiceMagicOption      uint8 = 0x20
	NpcServiceArenaManager     uint8 = 0x21
	NpcServiceArenaExchanger   uint8 = 0x22
	NpcServiceOpenMarket       uint8 = 0x23
	NpcServiceRecallParty      uint8 = 0x27
	NpcServiceSiegeTeleport    uint8 = 0x28
	NpcServiceInstanceExit     uint8 = 0x29

	// npcServiceLastTalkBit is the highest option the v1.150 u32 can carry.
	npcServiceLastTalkBit uint8 = 32
)

// NpcServices is the service set as a bit per option (bit n is option n).
type NpcServices uint64

/*
================
NpcServices.Has
================
*/
func (s NpcServices) Has(option uint8) bool {
	return option < 64 && s&(1<<option) != 0
}

/*
================
NpcServices.With
================
*/
func (s NpcServices) With(options ...uint8) NpcServices {
	for _, option := range options {
		s |= 1 << option
	}
	return s
}

/*
================
NpcServices.TalkFlags

The v1.150 select word: option n is bit 1 << (n - 1); options past 32
(the later arena, market and instance services) have no bit.
================
*/
func (s NpcServices) TalkFlags() uint32 {
	flags := uint32(0)
	for option := uint8(1); option <= npcServiceLastTalkBit; option++ {
		if s.Has(option) {
			flags |= NpcServiceTalkBit(option)
		}
	}
	return flags
}

/*
================
NpcServiceTalkBit
================
*/
func NpcServiceTalkBit(option uint8) uint32 {
	if option == 0 || option > npcServiceLastTalkBit {
		return 0
	}
	return 1 << (option - 1)
}

// npcServiceArm is one strstr test of a chain and the options it adds.
type npcServiceArm struct {
	part    string
	options []uint8
}

// npcServiceChains are 4C6350's else-if chains in their native order: within
// a chain the first matching codename wins, every chain runs. The codenames
// later versions added are kept; no v1.150 NPC matches them.
var npcServiceChains = [][]npcServiceArm{
	// 4C6501: storage keepers.
	arms([]string{"WAREHOUSE", "warehouse", "NPC_SD_M_AREA_WAREHOUSE", "NPC_SD_T_AREA_WAREHOUSE2"},
		NpcServiceStorage, NpcServiceShop),
	// Smiths and armourers repair.
	arms([]string{
		"NPC_CH_SMITH", "NPC_CH_ARMOR", "NPC_WC_SMITH", "NPC_WC_ARMOR", "NPC_KT_SMITH", "NPC_KT_ARMOR",
		"NPC_EU_SMITH", "NPC_EU_ARMOR", "NPC_CA_SMITH", "NPC_CA_ARMOR", "NPC_SD_M_AREA_SMITH",
		"NPC_SD_M_AREA_ARMOR", "NPC_SD_T_AREA_SMITH", "NPC_SD_T_AREA_ARMOR",
	}, NpcServiceRepair),
	// Weapon smiths also enchant magic options.
	arms([]string{
		"NPC_CH_SMITH", "NPC_WC_SMITH", "NPC_KT_SMITH", "NPC_EU_SMITH", "NPC_CA_SMITH",
		"NPC_SD_M_AREA_SMITH", "NPC_SD_T_AREA_SMITH",
	}, NpcServiceMagicOption),
	// Stable keepers.
	arms([]string{"NPC_CH_HORSE", "NPC_WC_HORSE", "NPC_KT_HORSE", "NPC_EU_HORSE", "NPC_CA_HORSE",
		"NPC_SD_M_AREA_HORSE"}, NpcServiceShop, NpcServiceStable),
	// Guild managers.
	arms([]string{"NPC_CH_GENARAL_SP", "NPC_WC_GUILD", "NPC_KT_GUILD", "NPC_EU_GUILD", "NPC_CA_GUILD",
		"NPC_SD_M_AREA_GUILD", "NPC_SD_T_AREA_GUILD2"}, NpcServiceGuild, NpcServiceShop),
	// The beginner guides' reverse return.
	arms([]string{"NPC_CH_SOLDIER_EM1", "NPC_EU_ADVICE3"}, NpcServiceReverseReturn),
	arms([]string{"NPC_TD_THIEF_BUY"}, NpcServiceSpecialTrade, NpcServiceThiefBuy),
	arms([]string{"NPC_CH_GENARAL_SW", "NPC_WC_GENARAL_SW", "NPC_KT_MINISTER"}, NpcServiceGeneral),
	arms([]string{"NPC_CH_DOCTOR", "NPC_WC_DOCTOR", "NPC_KT_DESIGNER", "NPC_EU_MERCHANT", "NPC_CA_MERCHANT",
		"NPC_SD_M_AREA_MERCHANT"}, NpcServiceJobTrader),
	arms([]string{"NPC_CH_GENARAL_SW", "NPC_WC_GENARAL_SW", "NPC_KT_MINISTER", "NPC_EU_HUNTER", "NPC_CA_HUNTER",
		"NPC_SD_M_AREA_HUNTER"}, NpcServiceJobHunter),
	arms([]string{"NPC_TD_THIEF_SELL", "NPC_SD_T_AREA_THIEF"}, NpcServiceJobThief),
	// Special goods traders.
	arms([]string{
		"NPC_CH_SPECIAL2", "NPC_WC_SPECIAL2", "NPC_TK_SPECIAL", "NPC_RM_SPECIAL", "NPC_EU_SPECIAL",
		"NPC_CA_SPECIAL", "NPC_SD_M_AREA_SPECIAL", "NPC_SD_M_AREA_SPECIAL2", "NPC_SD_M_AREA_SPECIAL3",
	}, NpcServiceSpecialTrade),
	// Fortress staff.
	arms([]string{"NPC_CH_FORTRESS_OFFICIAL", "NPC_WC_FORTRESS_OFFICIAL", "NPC_KT_FORTRESS_OFFICIAL",
		"NPC_EU_FORTRESS_OFFICIAL", "NPC_CA_FORTRESS_OFFICIAL"}, NpcServiceFortressOfficial, NpcServiceShop),
	arms([]string{
		"NPC_CH_FORTRESS_MANAGER1", "NPC_CH_FORTRESS_MANAGER2", "NPC_WC_FORTRESS_MANAGER1",
		"NPC_WC_FORTRESS_MANAGER2", "NPC_KT_FORTRESS_MANAGER", "NPC_EU_FORTRESS_MANAGER",
		"NPC_CA_FORTRESS_MANAGER1", "NPC_CA_FORTRESS_MANAGER2",
	}, NpcServiceFortressManager),
	arms([]string{
		"NPC_CH_FORTRESS_BATTLEAIDE1", "NPC_CH_FORTRESS_BATTLEAIDE2", "NPC_WC_FORTRESS_BATTLEAIDE1",
		"NPC_WC_FORTRESS_BATTLEAIDE2", "NPC_KT_FORTRESS_BATTLEAIDE", "NPC_EU_FORTRESS_BATTLEAIDE",
		"NPC_CA_FORTRESS_BATTLEAIDE1", "NPC_CA_FORTRESS_BATTLEAIDE2",
	}, NpcServiceFortressAide, NpcServiceShop),
	arms([]string{
		"NPC_CH_FORTRESS_SMITH1", "NPC_CH_FORTRESS_SMITH2", "NPC_WC_FORTRESS_SMITH1", "NPC_WC_FORTRESS_SMITH2",
		"NPC_KT_FORTRESS_SMITH", "NPC_EU_FORTRESS_SMITH", "NPC_CA_FORTRESS_SMITH1", "NPC_CA_FORTRESS_SMITH2",
	}, NpcServiceFortressSmith, NpcServiceRepair),
	arms([]string{
		"NPC_CH_FORTRESS_TRAINER1", "NPC_CH_FORTRESS_TRAINER2", "NPC_WC_FORTRESS_TRAINER1",
		"NPC_WC_FORTRESS_TRAINER2", "NPC_KT_FORTRESS_TRAINER", "NPC_EU_FORTRESS_TRAINER",
		"NPC_CA_FORTRESS_TRAINER1", "NPC_CA_FORTRESS_TRAINER2",
	}, NpcServiceFortressTrainer),
	arms([]string{
		"STRUCTURE_GATE_PULLEY_JA_01", "STRUCTURE_GATE_PULLEY_JA_02", "STRUCTURE_GATE_PULLEY_JA_03",
		"STRUCTURE_GATE_PULLEY_BJ_01", "STRUCTURE_GATE_PULLEY_BJ_02", "STRUCTURE_GATE_PULLEY_HT_01",
		"STRUCTURE_GATE_PULLEY_HT_02", "STRUCTURE_GATE_PULLEY_HT_03",
	}, NpcServiceGatePulley),
	{{"NPC_BATTLE_ARENA_MANAGER", []uint8{NpcServiceArenaManager}},
		{"NPC_BATTLE_ARENA_EXCHANGER", []uint8{NpcServiceArenaExchanger}}},
	arms([]string{"NPC_OPEN_MARKET_JUEL"}, NpcServiceOpenMarket),
	arms([]string{"NPC_FWORLD_RECALL_PARTY"}, NpcServiceRecallParty),
	arms([]string{"NPC_INS_EXIT_TELEPORT"}, NpcServiceInstanceExit),
	arms([]string{"NPC_CH_GACHA_OPERATOR"}, NpcServiceGachaOperator),
}

/*
================
arms

One chain whose every codename adds the same options.
================
*/
func arms(parts []string, options ...uint8) []npcServiceArm {
	chain := make([]npcServiceArm, len(parts))
	for index, part := range parts {
		chain[index] = npcServiceArm{part: part, options: options}
	}
	return chain
}

/*
================
NpcServicesForCodename

4C6350's codename chains. Two arms are not codename tests and are left to
their owners: option 0x11 follows the reference data's Magic POP binding
(CRefData_FindNpcGachaName 4CCB70), and NPC_SIEGE_DUNGEON_TELEPORT (0x28)
is an exact, case-blind compare. The service-off mode (+0x42404 != 0) that registers nothing and
poses agents and machines instead never runs a v1.150 shard.
================
*/
func NpcServicesForCodename(codename string) NpcServices {
	var services NpcServices
	for _, chain := range npcServiceChains {
		for _, arm := range chain {
			if strings.Contains(codename, arm.part) {
				services = services.With(arm.options...)
				break
			}
		}
	}
	if strings.EqualFold(codename, "NPC_SIEGE_DUNGEON_TELEPORT") {
		services = services.With(NpcServiceSiegeTeleport)
	}
	return services
}
