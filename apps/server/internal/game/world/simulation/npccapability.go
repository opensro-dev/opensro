package simulation

import "strings"

// NPC talk capability flags: the u32 dword the S->C 0xB45A select grant
// carries and the client menu builder (folded sub_5d9100,
// CIFNPCTalk_BuildMenuFromFlags) turns into talk-window rows.
//
// Retail has no single data column for this word. The v1.188 server composes
// it from three owners: sub_4c6350's codename service registrations,
// refshopgroup's NPC association, and script/session-owned conversation
// options. The port therefore resolves the final word onto NpcDef at media
// load; packet handlers consume that row instead of maintaining a short
// parallel allowlist.
const (
	// NpcTalkFlagShop is the shop row (client action 1). PINNED: the
	// refshopgroup.txt association carries every roster codename we seed,
	// and the v1.188 switch registers option 1 alongside it for
	// warehouse/guild arms.
	NpcTalkFlagShop uint32 = 0x1
	// NpcTalkFlagTalk is the talk-start row (client action 4). The v1.188
	// switch registers option 4 only on the smith/armor list (~214184+).
	NpcTalkFlagTalk uint32 = 0x2
	// NpcTalkFlagStorage is the storage row (client action 3). PINNED:
	// the switch's "WAREHOUSE" substring arm registers option 3
	// (~214134).
	NpcTalkFlagStorage uint32 = 0x4
	// NpcTalkFlagAction0B is the label-less client action 0xb row
	// (repair-adjacent; the fold carries no label). The switch's second
	// smith pass registers option 0x20 on the same list (~214337+).
	NpcTalkFlagAction0B uint32 = 0x20
	// NpcTalkFlagRecallPoint is the teleport-guide row (client action 9)
	// that opens the retail return-point designation agreement.
	NpcTalkFlagRecallPoint uint32 = 0x40
	// NpcTalkFlagGuild is the guild set (client action 0x12 create /
	// management when in guild). The switch's NPC_*_GUILD arm registers
	// the guild token 0xf (~214481+); the client's guild set reads bit
	// 0x4000.
	NpcTalkFlagGuild uint32 = 0x4000
	// NpcTalkFlagGachaMachine is the Magic Pop row: client
	// CIFNPCTalk_BuildMenuFromFlags tests 0x10000 and appends action 0x27.
	// The action click then sends 0x7338 [boundGid][0x10000], whose B338
	// answer opens control 0x8c.
	NpcTalkFlagGachaMachine uint32 = 0x10000
	// NpcTalkFlagJobTrader, Thief and Hunter are the job guild menus
	// (CIFNPCTalk_AppendJobMenuRows for job 1, 2 and 3): sub_4c6350
	// registers options 0x14, 0x15 and 0x16 on the guild NPCs below.
	NpcTalkFlagJobTrader uint32 = 0x80000
	NpcTalkFlagJobThief  uint32 = 0x100000
	NpcTalkFlagJobHunter uint32 = 0x200000
	// NpcTalkFlagFortressOfficial is the fortress official's row: client
	// 5D8FF0 tests 0x800000 and 5D7AD0 appends action 0x34
	// (SN_FORTRESS_OFFICIAL_WARAPPLY), whose click sends 0x71E1 subtype 6.
	// v1.188 registers function option 0x18 on the official; the port grants
	// it to the official of every fortress in siegefortress.txt.
	NpcTalkFlagFortressOfficial uint32 = 0x800000

	// NpcTalkImplementedFlags is the capability subset whose complete
	// request -> authority -> response lifecycle exists in this port. The
	// v1.188 registration table below remains the evidence catalogue, but a
	// retail row must not be advertised to the client until its gameplay
	// owner exists: otherwise CIFNPCTalk renders a button that can only be
	// rejected by HandleNpcAction. Repair-adjacent action 0x0b and guild
	// management therefore stay fail-closed at the media boundary; storage
	// has its owner (action/storage.go).
	NpcTalkImplementedFlags uint32 = NpcTalkFlagShop |
		NpcTalkFlagTalk |
		NpcTalkFlagStorage |
		NpcTalkFlagRecallPoint |
		NpcTalkFlagGachaMachine |
		NpcTalkFlagJobTrader |
		NpcTalkFlagJobThief |
		NpcTalkFlagJobHunter
)

// jobGuildCodenames: sub_4c6350 matches these by substring (CRT_strstr)
// and registers the guild option for the job beside them.
var jobGuildCodenames = []struct {
	part string
	flag uint32
}{
	{"NPC_CH_DOCTOR", NpcTalkFlagJobTrader}, {"NPC_WC_DOCTOR", NpcTalkFlagJobTrader},
	{"NPC_KT_DESIGNER", NpcTalkFlagJobTrader}, {"NPC_EU_MERCHANT", NpcTalkFlagJobTrader},
	{"NPC_CA_MERCHANT", NpcTalkFlagJobTrader}, {"NPC_SD_M_AREA_MERCHANT", NpcTalkFlagJobTrader},
	{"NPC_CH_GENARAL_SW", NpcTalkFlagJobHunter}, {"NPC_WC_GENARAL_SW", NpcTalkFlagJobHunter},
	{"NPC_KT_MINISTER", NpcTalkFlagJobHunter}, {"NPC_EU_HUNTER", NpcTalkFlagJobHunter},
	{"NPC_CA_HUNTER", NpcTalkFlagJobHunter}, {"NPC_SD_M_AREA_HUNTER", NpcTalkFlagJobHunter},
	{"NPC_TD_THIEF_SELL", NpcTalkFlagJobThief}, {"NPC_SD_T_AREA_THIEF", NpcTalkFlagJobThief},
}

// NpcJobGuild answers the job (1 trader, 2 thief, 3 hunter) whose guild an
// NPC keeps, or 0.
func NpcJobGuild(codename string) uint8 {
	switch npcJobGuildFlag(codename) {
	case NpcTalkFlagJobTrader:
		return 1
	case NpcTalkFlagJobThief:
		return 2
	case NpcTalkFlagJobHunter:
		return 3
	}
	return 0
}

// npcJobGuildFlag is the job guild capability bit of a codename, or 0.
func npcJobGuildFlag(codename string) uint32 {
	for _, row := range jobGuildCodenames {
		if strings.Contains(codename, row.part) {
			return row.flag
		}
	}
	return 0
}

// npcTalkCapabilityByCodename maps a roster NPC codename to its 0xB45A
// capability dword. Values must never carry 0x40000000: that bit makes
// the client expect a u16 job-transport tail the encoder does not write
// and no job-transport plane exists on either side (pinned by the
// encoder's contract and the test suite).
//
// DECISIONS on the PROBABLE bits (per-bit grades in the recon doc §6):
//
//   - Talk 0x2 is NOT implicit for every talkable NPC: it is emitted only
//     where the v1.188 switch registers option 4, which is the smith list
//     and not the warehouse/guild arms. Emitting it there anyway would put
//     a talk-start row on screen that retail's registration never granted
//   - a guess dressed as a capability. If a live 0xB045/0xB45A capture
//     proves the bit, add it to the row then.
//   - Smith 0x20 is included: the switch registers option 0x20 on the
//     same smith list and the client consumes bit 0x20; both sides agree
//     on the bit position even though the row's label is unpinned.
//   - Guild 0x4000 is included: the option-0xf -> bit-0x4000 numeric
//     conversion is PROBABLE rather than pinned, but a guild NPC without
//     the guild set is pointless (the create flow is the reason the
//     codename exists), and the client bit is unambiguous in the folded
//     menu builder.
//
// NPC_EU_WAREHOUSE and NPC_EU_GUILD are seeded ahead of the roster (only
// NPC_EU_SMITH spawns today) so growing the roster is a one-line diff
// with the flags decision already reviewed.
var reconstructedNpcServiceFlagsByCodename = map[string]uint32{
	"NPC_EU_SMITH":         NpcTalkFlagShop | NpcTalkFlagTalk | NpcTalkFlagAction0B, // 0x23
	"NPC_EU_WAREHOUSE":     NpcTalkFlagShop | NpcTalkFlagStorage,                    // 0x05
	"NPC_EU_GUILD":         NpcTalkFlagShop | NpcTalkFlagGuild,                      // 0x4001
	"NPC_CH_GACHA_MACHINE": NpcTalkFlagGachaMachine,
	"NPC_EU_ADVICE3":       NpcTalkFlagTalk, // Recall eligibility comes from teleportdata, not the codename.
}

// NpcTalkCapabilityFlags answers the 0xB45A capability dword for a roster
// codename. ok is false for a codename with no table row - DECISION: the
// select handler then grants SILENTLY (no 0xB45A) instead of emitting
// flags 0. A zero dword would open a talk window whose empty menu claims
// "this NPC offers nothing", a positive assertion we cannot back;
// silence asserts nothing and is exactly the pre-landing behavior the
// client already handles (no window until the row is decided).
func NpcTalkCapabilityFlags(codename string) (uint32, bool) {
	flags, ok := reconstructedNpcServiceFlagsByCodename[codename]
	return flags, ok
}

// ResolveNpcTalkFlags composes the capability word at the data boundary.
// npcchat-backed talk is deliberately independent from sub_4c6350's static
// service switch: the retail Lua/session plane can add ordinary conversation
// rows even when that switch registers no shop service.
func ResolveNpcTalkFlags(npc NpcDef) uint32 {
	flags, _ := NpcTalkCapabilityFlags(npc.Codename)
	flags |= npcJobGuildFlag(npc.Codename)
	if len(npc.NpcTalkStoreGroups) != 0 {
		flags |= NpcTalkFlagShop
	}
	if npc.BaseSpeechSymbol != "" || npc.QuestSpeechSymbol != "" {
		flags |= NpcTalkFlagTalk
	}
	return flags & NpcTalkImplementedFlags
}
