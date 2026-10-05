/*
===========================================================================

npccapability.go - the 0xB45A talk word an NPC's select answer carries

The client menu builder (CIFNPCTalk_BuildMenuFromCapabilities 5D9100) turns
the u32 into talk-window rows, and a row's click sends that bit back as the
0x7338 mask. The word is the NPC's service set (npcservice.go) in the
v1.150 bit layout, plus the services other owners grant: the shop of a
refshopgroup association, the talk row of npcchat speech, the teleport
bits of teleportdata (ConfigurePortals) and the Magic POP binding.

===========================================================================
*/

package simulation

// NPC talk capability bits: bit 1 << (option - 1) of the service option.
const (
	NpcTalkFlagShop          uint32 = 0x1     // option 1, row 1
	NpcTalkFlagTalk          uint32 = 0x2     // option 2, row 4
	NpcTalkFlagStorage       uint32 = 0x4     // option 3, rows 3, 0x2D, 0x2E
	NpcTalkFlagRepair        uint32 = 0x8     // option 4, no row; shows the NPC quest tab
	NpcTalkFlagRecallPoint   uint32 = 0x40    // option 7, row 9
	NpcTalkFlagTeleport      uint32 = 0x80    // option 8, row 0xA
	NpcTalkFlagStable        uint32 = 0x400   // option 0xB, no row
	NpcTalkFlagSpecialTrade  uint32 = 0x800   // option 0xC, the shop row sends 0x800
	NpcTalkFlagThiefBuy      uint32 = 0x1000  // option 0xD, no row
	NpcTalkFlagGeneral       uint32 = 0x2000  // option 0xE, no row
	NpcTalkFlagGuild         uint32 = 0x4000  // option 0xF, rows 0x12..0x1D
	NpcTalkFlagGachaMachine  uint32 = 0x10000 // option 0x11, row 0x27
	NpcTalkFlagJobTrader     uint32 = 0x80000
	NpcTalkFlagJobThief      uint32 = 0x100000
	NpcTalkFlagJobHunter     uint32 = 0x200000
	NpcTalkFlagReverseReturn uint32 = 0x20000000 // option 0x1E, rows 0x2B
	NpcTalkFlagGatePulley    uint32 = 0x40000000 // option 0x1F, a u16 tail follows
	NpcTalkFlagMagicOption   uint32 = 0x80000000 // option 0x20, row 0x2F
	// NpcTalkFlagFortressOfficial is the fortress official's row (option
	// 0x18): client 5D8FF0 tests 0x800000 and 5D7AD0 appends action 0x34
	// (SN_FORTRESS_OFFICIAL_WARAPPLY), whose click sends 0x71E1 subtype 6.
	// The port grants it to the official of every fortress in
	// siegefortress.txt.
	NpcTalkFlagFortressOfficial uint32 = 0x800000

	// NpcTalkImplementedFlags is the subset whose request -> authority ->
	// response lifecycle exists in this port; a row the client would draw
	// for any other bit could only be refused. Bits without a row (repair,
	// stable, thief buy, general) carry nothing to refuse. Still closed:
	// the fortress staff other than the official, and gate pulleys.
	NpcTalkImplementedFlags uint32 = NpcTalkFlagShop | NpcTalkFlagTalk | NpcTalkFlagStorage |
		NpcTalkFlagRepair | NpcTalkFlagRecallPoint | NpcTalkFlagTeleport | NpcTalkFlagStable |
		NpcTalkFlagSpecialTrade | NpcTalkFlagThiefBuy | NpcTalkFlagGeneral | NpcTalkFlagGachaMachine |
		NpcTalkFlagJobTrader | NpcTalkFlagJobThief | NpcTalkFlagJobHunter | NpcTalkFlagReverseReturn |
		NpcTalkFlagGuild | NpcTalkFlagMagicOption | NpcTalkFlagFortressOfficial
)

// npcGachaMachines are the NPCs the reference data binds a Magic POP to
// (4C6350 registers option 0x11 when CRefData_FindNpcGachaName finds one).
// INFERENCE: the v1.150 media ships no gacha binding table; the one machine
// the client knows (row 0x27, SN_TALK_CH_GACHA_MACHINE_2) is the binding.
var npcGachaMachines = map[string]bool{"NPC_CH_GACHA_MACHINE": true}

/*
================
ResolveNpcServices

The service set of a roster row: 4C6350's codename chains, the shop a
refshopgroup association grants, the talk row of npcchat speech and the
Magic POP binding. INFERENCE: v1.188 registers option 2 nowhere in 4C6350;
the quest and Lua plane adds conversation, which npcchat speech stands for.
================
*/
func ResolveNpcServices(npc NpcDef) NpcServices {
	services := NpcServicesForCodename(npc.Codename)
	if len(npc.NpcTalkStoreGroups) != 0 {
		services = services.With(NpcServiceShop)
	}
	if npc.BaseSpeechSymbol != "" || npc.QuestSpeechSymbol != "" {
		services = services.With(NpcServiceTalk)
	}
	if npcGachaMachines[npc.Codename] {
		services = services.With(NpcServiceGachaMachine)
	}
	return services
}

/*
================
ResolveNpcTalkFlags

The talk word composed at the data boundary, limited to implemented rows.
================
*/
func ResolveNpcTalkFlags(npc NpcDef) uint32 {
	return npc.Services.TalkFlags() & NpcTalkImplementedFlags
}

/*
================
NpcJobGuild

The job (1 trader, 2 thief, 3 hunter) whose guild an NPC keeps, or 0.
================
*/
func NpcJobGuild(npc NpcDef) uint8 {
	switch {
	case npc.Services.Has(NpcServiceJobTrader):
		return 1
	case npc.Services.Has(NpcServiceJobThief):
		return 2
	case npc.Services.Has(NpcServiceJobHunter):
		return 3
	}
	return 0
}
