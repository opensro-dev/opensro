package enterworld

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

// ENTER-PAYLOAD CORPUS (the totality campaign for the 0x31db cutover): a
// table-driven set of REAL enter-world packet sequences the bootstrap
// emitter builds across every axis the composer supports TODAY - bare vs
// geared rosters, restored inventories with varied plus/variance/durability
// and bag-heavy layouts, masteries/skills seeded vs empty, whisper-block
// lists {0, 2, 20 (the client panel's 0x14 cap)}, event-guide masks,
// COMPLETED/ACTIVE/TRACKED quest sections (real v1.150 questdata ids by
// codename; active SQuestInfo bodies across the observed-legal flag
// combos incl. the full 0x5c record with a 0xFF-sentinel contents node
// and npcpos-valid targetIds; tracked records with and without the
// flags&0x02 optional - 2026-07-29 quest emission wave, the LAST enter
// axis to leave hardcoded-empty), entered-tail event-group lists (incl.
// the id-1 render-option/guide-message leg), per-item MAGIC OPTIONS
// (real v1.150 magicoption.txt ids by MATTR codename+degree; the visible
// '+' rows, the hidden '-@' row, and the reinforce-flag row - 2026-07-29
// emission wave), AVATAR inventories (real v1.150 itemdata avatar-band
// rows with full shipped records), and the china-male / china-female /
// europe-male start profiles. The client parity harness soak drives every vector through the REAL
// WIP entered chain and requires zero contained throws, the production
// adapter commit, and a committed state that deep-equals the per-vector
// wire decode (the twin parser retired 2026-07-29; the wire oracle
// replaced the shadow instrument as the byte-truth).
//
// Item constraint: every item any vector references resolves in testItems()
// AND carries the full live PK2 record from
// testdata/live_bootstrap_asd2_full.json (the enteredfixture precedent) -
// the client's folded item readers reject absent columns loudly, so the
// corpus never invents record minima. The avatar rows carry the full
// SHIPPED itemdata records instead (sharedShippedItems - the same
// production TextdataItems pipeline that authored the live fixture).

type enterCorpusVector struct {
	Label           string                 `json:"label"`
	Notes           []string               `json:"notes,omitempty"`
	CharacterName   string                 `json:"characterName"`
	CharacterID     int64                  `json:"characterId"`
	Gid             uint32                 `json:"gid"`
	RegionID        uint16                 `json:"regionId"`
	Seed            enteredFixtureSeed     `json:"seed"`
	Packets         []enteredFixturePacket `json:"packets"`
	RefItemSnapshot []RefItemRow           `json:"refItemSnapshot"`
	// MagicOptionSnapshot carries the magicoption.txt definition rows the
	// vector's item bodies reference; the harness soak seeds the client's
	// magicOptionDefsByParamId3e4 table from it (the refItemSnapshot
	// posture - the client's REINFORCE leg throws on a missing row).
	MagicOptionSnapshot []MagicOptionRow `json:"magicOptionSnapshot,omitempty"`
}

type enterCorpus struct {
	Comment []string            `json:"comment"`
	Vectors []enterCorpusVector `json:"vectors"`
}

// Quest content pins: v1.150 questdata.txt rows resolved by CODENAME
// (TestEnterCorpusContentPins cross-pins each id against the shipped
// table) and one npcpos.txt target row. DECLARED CHOICES on the record
// bodies: u08/u09/flags/valueA/word/tail values are wire-legal shapes
// from the questInfoWireParity listing pins (the flag combos 0x00 and
// 0x5c are observed-legal; tracked flags 0x01/0x03 exercise both arms of
// the @0x86751d optional gate); the ContentsNode description strings are
// WIRE-carried symbols, authored here as the quests' own shipped
// textquest SN_CON_* keys (v1.150-consistent content - the client
// resolves them through the 0xcec800 manager); the targetId is the
// potion seller's real npcpos row so a tracked minimap pass resolves
// instead of throwing map::at (sub_7e1240 @0x007e126d).
const (
	corpusQuestTutorialChID uint32 = 2    // QTUTORIAL_CH
	corpusQuestSmith1ID     uint32 = 3    // QNO_CH_SMITH_1
	corpusQuestChef1ID      uint32 = 4    // QNO_CH_CHEF_1
	corpusQuestSoldierEa1ID uint32 = 5    // QNO_CH_SOLDIER_EA1_1 (level 3)
	corpusQuestPotion1ID    uint32 = 29   // QSP_ALL_POTION_1 (level 20)
	corpusQuestPotionNpcID  uint32 = 2005 // NPC_CH_POTION (npcpos region 25000)
	corpusQuestPotionSymbol        = "SN_CON_QSP_ALL_POTION_1"
)

// corpusCompletedQuestIds is the early-china completed set (ids 2/3/4 by
// codename, the pre-wave precedent vectors already pinned).
func corpusCompletedQuestIds() []uint32 {
	return []uint32{corpusQuestTutorialChID, corpusQuestSmith1ID, corpusQuestChef1ID}
}

// corpusActiveQuestMinimal is the flags=0 SQuestInfo arm: the 3-byte body
// with NO gated fields - the client record keeps the ctor 0xffffffff
// progress sentinel (UIIT_STT_QUEST_UNLIMITED on the quest pane).
func corpusActiveQuestMinimal() ActiveQuestRecord {
	return ActiveQuestRecord{RefID: corpusQuestSoldierEa1ID, U08: 1, U09: 1, Flags: 0x00}
}

// corpusActiveQuestFull is the flags=0x5c arm: every gated block on one
// record - progress (a 5-minute remain word per the sub_5c2a30 decode),
// the u10 kind byte 1 (the CIFQuestReward-opening class), two contents
// nodes (one with an objective value array, one with the 0xFF sentinel
// that skips the array entirely), and one npcpos-valid targetId.
func corpusActiveQuestFull() ActiveQuestRecord {
	return ActiveQuestRecord{
		RefID:    corpusQuestPotion1ID,
		U08:      2,
		U09:      1,
		Flags:    0x5c,
		Progress: 0x00500000,
		U10:      1,
		Contents: []ActiveQuestContentsNode{
			{Tag: 0, Kind: 1, Description: corpusQuestPotionSymbol, ObjectiveValues: []uint32{3, 10}},
			{Tag: 1, Kind: 2, Description: corpusQuestPotionSymbol, ObjectiveSentinel: true},
		},
		TargetIds: []uint32{corpusQuestPotionNpcID},
	}
}

// corpusTrackedQuestPlain is the flags&0x02-CLEAR tracked arm: no
// trailing optional u32 (@0x867522 je).
func corpusTrackedQuestPlain() TrackedQuestRecord {
	return TrackedQuestRecord{
		RefID: corpusQuestSoldierEa1ID,
		Flags: 0x01,
		Tail6: []uint8{0, 0, 0, 0, 0, 0},
	}
}

// corpusTrackedQuestWithOptional is the flags&0x02-SET tracked arm: the
// optional u32 rides the tail (@0x867524).
func corpusTrackedQuestWithOptional() TrackedQuestRecord {
	return TrackedQuestRecord{
		RefID:    corpusQuestPotion1ID,
		Flags:    0x03,
		ValueA:   0x11,
		Word:     0x2233,
		Tail6:    []uint8{1, 2, 3, 4, 5, 6},
		Optional: 0x44556677,
	}
}

// Magic-option content pins: v1.150 magicoption.txt rows resolved by
// MATTR_* CODENAME + degree (TestEnterCorpusContentPins cross-pins each id
// against the shipped table so a table swap cannot silently re-point them).
// Declared choices (SRO-standard blue conventions; the v1.188 shard was not
// needed - STR/INT/DUR on a weapon is the standard retail pairing):
//   - MATTR_STR degree 1  = param id 11, paramName "+"  (visible)
//   - MATTR_INT degree 1  = param id  5, paramName "+"  (visible)
//   - MATTR_DUR degree 2  = param id 81, paramName "+"  (visible)
//   - MATTR_DEC_MAXDUR degree 1 = param id 1, paramName "-@" (the '-'
//     name drives the client's visible-count decrement, sub_78b1b0
//     @0x0078b306..)
//   - MATTR_REINFORCE_ITEM degree 1 = param id 66 (name equality sets the
//     client's +0x30 reinforce flag, @0x0078b2f7..)
//
// Magnitudes ride the encoded u64's high u32 (the REINFORCE-leg map value).
const (
	corpusMagicStrParamID       uint32 = 11
	corpusMagicIntParamID       uint32 = 5
	corpusMagicDurParamID       uint32 = 81
	corpusMagicDecMaxDurParamID uint32 = 1
	corpusMagicReinforceParamID uint32 = 66
)

// Avatar content pins: v1.150 itemdata_25000.txt avatar rows (the
// TID [3,1,13,x] band, packed word & 0x780 == 0x680 -
// wire.IsAvatarBandTid), resolved by CODENAME and cross-pinned by
// TestEnterCorpusContentPins. Male pirate set on the china-male base
// character (reqGender 1 rows on a male wearer - the honest pairing even
// though the avatar parse itself never gates). Slots are the client's
// avatar sockets: 0 = hat (TID4 1), 1 = dress (TID4 2).
const (
	corpusAvatarHatCodename         = "ITEM_MALL_AVATAR_M_PIRATE_HAT"
	corpusAvatarHatRefID     uint32 = 24284
	corpusAvatarHatTypeFlags uint16 = 0x0eac
	corpusAvatarSetCodename         = "ITEM_MALL_AVATAR_M_PIRATE"
	corpusAvatarSetRefID     uint32 = 24286
	corpusAvatarSetTypeFlags uint16 = 0x16ac
)

// corpusPirateAvatarInventory is the avatar-wearing vectors' shared record:
// capacity 5 (the composer's historical wire byte) + the two pirate rows.
// Avatar dress has no durability concept (itemdata Dur_L/Dur_U 0), so the
// bodies carry durability 0 honestly.
func corpusPirateAvatarInventory() *AvatarInventory {
	return &AvatarInventory{
		Capacity: 5,
		Rows: []InventoryRow{
			corpusRow(0, corpusAvatarHatRefID, corpusAvatarHatCodename, corpusAvatarHatTypeFlags, 0, "0", 0),
			corpusRow(1, corpusAvatarSetRefID, corpusAvatarSetCodename, corpusAvatarSetTypeFlags, 0, "0", 0),
		},
	}
}

type enterCorpusSpec struct {
	label     string
	notes     []string
	character func() *Character
	// equipItemsEnabled gates the equip roster (MISSION_EQUIP_ITEMS).
	equipItemsEnabled bool
}

// corpusBaseCharacter is the canonical china spearman with a per-vector
// identity (distinct id -> distinct gid, so the harness soak's gid registry
// never aliases two vectors).
func corpusBaseCharacter(id int64, name string) *Character {
	character := chinaSpearman()
	character.ID = id
	character.Name = name
	return character
}

func corpusChinaWoman(id int64, name string) *Character {
	return &Character{
		ID:            id,
		Name:          name,
		RaceIndex:     i64(RaceChina),
		Gender:        i64(GenderFemale),
		ModelCodename: "CHAR_CH_WOMAN_ADVENTURER",
		ModelRef:      i64(1920),
	}
}

func corpusEuropeMan(id int64, name string) *Character {
	return &Character{
		ID:            id,
		Name:          name,
		RaceIndex:     i64(RaceEurope),
		Gender:        i64(GenderMale),
		ModelCodename: "CHAR_EU_MAN_ADVENTURER",
		ModelRef:      i64(14726),
	}
}

// corpusRow builds one restored missionInventory row over the codenames
// testItems() resolves. typeFlags matter for codenames the equip roster
// does NOT cover: the pinned snapshot precedence (Node parity,
// TestBuildRefItemSnapshotCoversInventoryAndGold) lets the inventory row's
// own word ride the refItemSnapshot there, and the client's native family
// assertion (soitem.cpp:0xcc "No Equip, No Expendable") refuses a word
// outside both classes - so uncovered rows must carry a family-true word
// (the live persisted rows do: 0x21ac heavy garment, 0x32c weapon).
// Roster-covered codenames keep the roster's itemdata-derived word
// (first writer wins), so their row word never reaches the client.
func corpusRow(slot int64, refObjID uint32, codename string, typeFlags uint16, plus int64, varianceBits string, durability int64) InventoryRow {
	return InventoryRow{
		Slot:         slot,
		RefObjID:     refObjID,
		Codename:     codename,
		TypeFlags:    typeFlags,
		Plus:         plus,
		VarianceBits: varianceBits,
		Durability:   durability,
		StackCount:   1,
	}
}

func corpusTrainedMasteries() []CharacterMastery {
	masteries := DefaultMasteries(RaceKeyChina)
	for index := range masteries {
		masteries[index].Level = int64(index) // 0,1,2,... incl. the level-0 rows
	}
	return masteries
}

func corpusWhisperNames(count int) []string {
	names := make([]string, count)
	for index := range names {
		names[index] = fmt.Sprintf("Blocked%02d", index+1)
	}
	return names
}

func enterCorpusSpecs() []enterCorpusSpec {
	return []enterCorpusSpec{
		{
			label:             "bare-china-spearman-no-equip",
			notes:             []string{"EquipItemsEnabled=false: zero wire items, inventory seeded empty"},
			character:         func() *Character { return corpusBaseCharacter(101, "corpus01") },
			equipItemsEnabled: false,
		},
		{
			label:             "geared-china-spearman-firstboot",
			notes:             []string{"first-ever bootstrap seeds the creation roster (spear + clothes; no injected test weapon)"},
			character:         func() *Character { return corpusBaseCharacter(102, "corpus02") },
			equipItemsEnabled: true,
		},
		{
			label: "geared-worn-plus-and-variance",
			notes: []string{"restored inventory: worn blade plus=7 with top-bit variance, garments with plus, a roster-uncovered heavy garment (row word 0x21ac rides the snapshot)"},
			character: func() *Character {
				character := corpusBaseCharacter(103, "corpus03")
				character.Gold = i64(25000)
				character.MissionInventory = []InventoryRow{
					corpusRow(6, 107, "ITEM_CH_BLADE_01_A", 0x32c, 7, "9223372036854775808", 40),
					corpusRow(1, 11, "ITEM_CH_M_CLOTHES_01_BA_A_DEF", 0x8c, 3, "0", 33),
					// The heavy pants set (CH_M_HEAVY_01) is not in the
					// roster, so THIS row's word reaches the client via the
					// snapshot precedence - it must be the live persisted
					// 0x21ac (TID 3,1,3,4), never a fabricated non-family
					// word the native soitem.cpp:0xcc assertion refuses.
					corpusRow(4, 5049, "ITEM_CH_M_HEAVY_01_LA_A", 0x21ac, 0, "0", 33),
				}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "bag-heavy-variance-durability",
			notes: []string{"six bag rows (slots 13..18) with varied plus/variance/durability next to a worn spear"},
			character: func() *Character {
				character := corpusBaseCharacter(104, "corpus04")
				character.MissionInventory = []InventoryRow{
					corpusRow(6, 3644, "ITEM_CH_SPEAR_01_A_DEF", 0x32c, 0, "0", 56),
					corpusRow(13, 107, "ITEM_CH_BLADE_01_A", 0x32c, 0, "9223372036854775808", 56),
					corpusRow(14, 3633, "ITEM_CH_BLADE_01_A_DEF", 0x32c, 5, "12297829382473034410", 100),
					corpusRow(15, 3644, "ITEM_CH_SPEAR_01_A_DEF", 0x32c, 12, "18446744073709551615", 1),
					corpusRow(16, 11, "ITEM_CH_M_CLOTHES_01_BA_A_DEF", 0x8c, 1, "1", 33),
					corpusRow(17, 12, "ITEM_CH_M_CLOTHES_01_LA_A_DEF", 0x8c, 2, "65535", 20),
					corpusRow(18, 13, "ITEM_CH_M_CLOTHES_01_FA_A_DEF", 0x8c, 3, "4294967296", 10),
				}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "inventory-seeded-empty",
			notes: []string{"restored session with a seeded-but-EMPTY inventory: zero wire items while equip stays enabled"},
			character: func() *Character {
				character := corpusBaseCharacter(105, "corpus05")
				character.MissionInventory = []InventoryRow{}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "masteries-racial-trained",
			notes: []string{"full CH racial mastery set with mixed levels incl. the level-0 creation rows; skills empty"},
			character: func() *Character {
				character := corpusBaseCharacter(106, "corpus06")
				character.Masteries = corpusTrainedMasteries()
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "skills-learned",
			notes: []string{"three learned-skill ids on the wire (value byte 1 each); masteries empty"},
			character: func() *Character {
				character := corpusBaseCharacter(107, "corpus07")
				character.Skills = []uint32{2, 40, 70}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "masteries-and-skills",
			notes: []string{"both learned lists populated on one wire"},
			character: func() *Character {
				character := corpusBaseCharacter(108, "corpus08")
				character.Masteries = corpusTrainedMasteries()
				character.Skills = []uint32{2, 40, 70, 1332}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "whisperblock-2",
			notes: []string{"two blocked-whisperer names in the entered chunk-C string list"},
			character: func() *Character {
				character := corpusBaseCharacter(109, "corpus09")
				character.BlockedWhisperers = corpusWhisperNames(2)
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "whisperblock-20-panel-cap",
			notes: []string{"twenty names - the client panel's 0x14 cap, the mutate-site maximum"},
			character: func() *Character {
				character := corpusBaseCharacter(110, "corpus10")
				character.BlockedWhisperers = corpusWhisperNames(20)
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "eventguide-mask-partial",
			notes: []string{"event-guide mask 0x2a: bit0 CLEAR with higher bits set (exercises the +0x18d9 first-bit-missing arm)"},
			character: func() *Character {
				character := corpusBaseCharacter(111, "corpus11")
				mask := int64(0x2a)
				character.Mission = &MissionRuntime{EventGuideStateMask: &mask}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "eventguide-mask-allbits",
			notes: []string{"event-guide mask 0xffffffff: every trigger already fired"},
			character: func() *Character {
				character := corpusBaseCharacter(112, "corpus12")
				mask := int64(0xffffffff)
				character.Mission = &MissionRuntime{EventGuideStateMask: &mask}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label:             "china-woman-bare",
			notes:             []string{"china female start profile (modelRef 1920, sexSelector1ac 0), no creation weapon"},
			character:         func() *Character { return corpusChinaWoman(113, "corpus13") },
			equipItemsEnabled: false,
		},
		{
			label:             "europe-man-witness-only",
			notes:             []string{"europe start profile (region 0x6b4f, modelRef 14726, countryByte9c 1); EU itemdata rows do not exist in the seed so the roster degrades to the bag witness"},
			character:         func() *Character { return corpusEuropeMan(114, "corpus14") },
			equipItemsEnabled: true,
		},
		{
			label: "stats-rich-level40",
			notes: []string{"level 40 with nonzero exp/skillExp/gold/SP/stat points, low persisted currents, bodyShape 0x23"},
			character: func() *Character {
				character := corpusBaseCharacter(115, "corpus15")
				character.Level = i64(40)
				character.MaxLevel = i64(42)
				character.Experience = i64(123456789)
				character.SkillExp = i64(55555)
				character.Gold = i64(2500000)
				character.SkillPoints = i64(12345)
				character.StatPoints = i64(77)
				character.CurrentHP = i64(120)
				character.CurrentMP = i64(90)
				character.BodyShapeByte = i64(0x23)
				character.Strength = i64(30)
				character.Intellect = i64(25)
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "kitchen-sink",
			notes: []string{
				"geared restored inventory + bag rows + masteries + skills + 2 whisper blocks + mask 0x2a + nonzero stats on ONE wire",
				"2026-07-29 emission wave: the worn blade now carries 2 reinforce magic options (MATTR_STR deg1 id 11 +2, MATTR_DUR deg2 id 81 +29) and a bag spear carries 1 (MATTR_INT deg1 id 5 +2); the pirate avatar set rides the avatar block (slots 0/1)",
				"2026-07-29 quest emission wave: all three quest sections ride the same wire - completed {2,3,4}, active {5 minimal flags=0, 29 full flags=0x5c}, tracked {5 plain, 29 with the flags&0x02 optional}",
			},
			character: func() *Character {
				character := corpusBaseCharacter(116, "corpus16")
				character.Level = i64(17)
				character.Experience = i64(424242)
				character.Gold = i64(31337)
				character.SkillPoints = i64(900)
				character.StatPoints = i64(9)
				character.CurrentHP = i64(150)
				character.CurrentMP = i64(60)
				worn := corpusRow(6, 107, "ITEM_CH_BLADE_01_A", 0x32c, 4, "9223372036854775808", 30)
				worn.MagicOptions = []uint64{
					EncodeMagicOption(corpusMagicStrParamID, 2),
					EncodeMagicOption(corpusMagicDurParamID, 29),
				}
				baggedSpear := corpusRow(13, 3644, "ITEM_CH_SPEAR_01_A_DEF", 0x32c, 2, "255", 56)
				baggedSpear.MagicOptions = []uint64{
					EncodeMagicOption(corpusMagicIntParamID, 2),
				}
				character.MissionInventory = []InventoryRow{
					worn,
					corpusRow(1, 11, "ITEM_CH_M_CLOTHES_01_BA_A_DEF", 0x8c, 1, "0", 33),
					corpusRow(4, 12, "ITEM_CH_M_CLOTHES_01_LA_A_DEF", 0x8c, 0, "0", 33),
					corpusRow(5, 13, "ITEM_CH_M_CLOTHES_01_FA_A_DEF", 0x8c, 0, "0", 33),
					baggedSpear,
					corpusRow(14, 3633, "ITEM_CH_BLADE_01_A_DEF", 0x32c, 0, "0", 100),
				}
				character.AvatarInventory = corpusPirateAvatarInventory()
				character.Masteries = corpusTrainedMasteries()
				character.Skills = []uint32{2, 40}
				character.BlockedWhisperers = corpusWhisperNames(2)
				character.CompletedQuestIds = corpusCompletedQuestIds()
				character.ActiveQuests = []ActiveQuestRecord{corpusActiveQuestMinimal(), corpusActiveQuestFull()}
				character.TrackedQuests = []TrackedQuestRecord{corpusTrackedQuestPlain(), corpusTrackedQuestWithOptional()}
				mask := int64(0x2a)
				character.Mission = &MissionRuntime{EventGuideStateMask: &mask}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "completedquests-early-china",
			notes: []string{"three completed-quest ids in the 0x32B3 quest block's first section - REAL v1.150 questdata.txt rows resolved by codename: 2 QTUTORIAL_CH, 3 QNO_CH_SMITH_1, 4 QNO_CH_CHEF_1; active/tracked stay 0"},
			character: func() *Character {
				character := corpusBaseCharacter(117, "corpus17")
				character.CompletedQuestIds = []uint32{2, 3, 4}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "eventgroup-1-guide-leg",
			notes: []string{"entered-tail event-group list {1}: exercises the client's event-start slot write AND the id-1 render-option pair (3,3)/(4,0x14) + UIIT_MSG_EVENT_START guide message (event ids are POLICY content - see EnterEventGroupIds)"},
			character: func() *Character {
				character := corpusBaseCharacter(118, "corpus18")
				character.EnterEventGroupIds = []uint32{1}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "eventgroup-non1",
			notes: []string{"entered-tail event-group list {5}: the event-start slot write WITHOUT the id-1 render-option/guide-message leg"},
			character: func() *Character {
				character := corpusBaseCharacter(119, "corpus19")
				character.EnterEventGroupIds = []uint32{5}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "completedquests-and-eventgroups",
			notes: []string{"both new sections on ONE wire: completed quests {2,3,4} (v1.150 questdata codenames) + event groups {1,5} (the id-1 leg and a plain slot write in the same loop)"},
			character: func() *Character {
				character := corpusBaseCharacter(120, "corpus20")
				character.CompletedQuestIds = []uint32{2, 3, 4}
				character.EnterEventGroupIds = []uint32{1, 5}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "magicoptions-reinforce-str-int-dur",
			notes: []string{
				"worn blade with 3 REINFORCE-leg magic options (real v1.150 magicoption.txt ids by MATTR codename+degree: STR deg1 id 11 +2, INT deg1 id 5 +2, DUR deg2 id 81 +29; all '+' param names = all visible); the magicOptionSnapshot ships the 3 definition rows",
			},
			character: func() *Character {
				character := corpusBaseCharacter(121, "corpus21")
				worn := corpusRow(6, 107, "ITEM_CH_BLADE_01_A", 0x32c, 5, "0", 40)
				worn.MagicOptions = []uint64{
					EncodeMagicOption(corpusMagicStrParamID, 2),
					EncodeMagicOption(corpusMagicIntParamID, 2),
					EncodeMagicOption(corpusMagicDurParamID, 29),
				}
				character.MissionInventory = []InventoryRow{
					worn,
					corpusRow(1, 11, "ITEM_CH_M_CLOTHES_01_BA_A_DEF", 0x8c, 0, "0", 33),
				}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "magicoptions-hidden-and-reinforce-flag",
			notes: []string{
				"worn blade with the two special magic-option legs on one item: MATTR_DEC_MAXDUR deg1 id 1 (paramName '-@' - the '-' decrements the client's visible count +0x85) and MATTR_REINFORCE_ITEM deg1 id 66 (option name equality sets the client's +0x30 reinforce flag)",
			},
			character: func() *Character {
				character := corpusBaseCharacter(122, "corpus22")
				worn := corpusRow(6, 107, "ITEM_CH_BLADE_01_A", 0x32c, 7, "9223372036854775808", 25)
				worn.MagicOptions = []uint64{
					EncodeMagicOption(corpusMagicDecMaxDurParamID, 12),
					EncodeMagicOption(corpusMagicReinforceParamID, 1),
				}
				character.MissionInventory = []InventoryRow{worn}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "avatar-pirate-set",
			notes: []string{
				"avatar block populated: capacity 5 + 2 rows (slot 0 hat 24284 ITEM_MALL_AVATAR_M_PIRATE_HAT word 0x0eac, slot 1 dress 24286 ITEM_MALL_AVATAR_M_PIRATE word 0x16ac - the (typeFlags & 0x780) == 0x680 avatar band); both refs ride the refItemSnapshot with FULL shipped-itemdata records so the client's per-row CSOItem parse resolves (an unseeded ref desyncs the stream, sub_78c830 L182-183)",
			},
			character: func() *Character {
				character := corpusBaseCharacter(123, "corpus23")
				character.AvatarInventory = corpusPirateAvatarInventory()
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "trackedquests-plain",
			notes: []string{"tracked-only quest block: one section-3 record with flags&0x02 CLEAR (no optional u32, @0x867522 je); completed/active stay 0 - the client's hosted sub_866c60 apply drives the CIFQuest +0x398 tracked id"},
			character: func() *Character {
				character := corpusBaseCharacter(124, "corpus24")
				character.TrackedQuests = []TrackedQuestRecord{corpusTrackedQuestPlain()}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "trackedquests-optional",
			notes: []string{"tracked-only quest block with BOTH optional arms: record 5 flags 0x01 (no optional) then record 29 flags 0x03 (optional u32 rides, @0x867524); the last wire record wins the +0x398 tracked id"},
			character: func() *Character {
				character := corpusBaseCharacter(125, "corpus25")
				character.TrackedQuests = []TrackedQuestRecord{corpusTrackedQuestPlain(), corpusTrackedQuestWithOptional()}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "activequests-minimal",
			notes: []string{"one active SQuestInfo record with flags=0 - the minimal 3-byte body, NO gated fields; the client record keeps the ctor 0xffffffff progress sentinel (sub_788d00 @0x788d6b) and the v1.150 payload+0xc0 zero byte leaves it uncleared"},
			character: func() *Character {
				character := corpusBaseCharacter(126, "corpus26")
				character.ActiveQuests = []ActiveQuestRecord{corpusActiveQuestMinimal()}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "activequests-full-0x5c",
			notes: []string{"one active SQuestInfo record with flags=0x5c - every gated block: progress u32 + u10 + 2 contents nodes (a real objective-value array AND the 0xFF sentinel node that skips the array, @0x785d03) + a targetId that resolves in the shipped npcpos (2005 NPC_CH_POTION)"},
			character: func() *Character {
				character := corpusBaseCharacter(127, "corpus27")
				character.ActiveQuests = []ActiveQuestRecord{corpusActiveQuestFull()}
				return character
			},
			equipItemsEnabled: true,
		},
		{
			label: "quests-all-three-sections",
			notes: []string{"the full sub_8673d0 quest block on ONE wire: completed {2,3,4} + active {5 minimal, 29 full 0x5c} + tracked {29 with optional} - the section ordering and every gated read exercised together"},
			character: func() *Character {
				character := corpusBaseCharacter(128, "corpus28")
				character.CompletedQuestIds = corpusCompletedQuestIds()
				character.ActiveQuests = []ActiveQuestRecord{corpusActiveQuestMinimal(), corpusActiveQuestFull()}
				character.TrackedQuests = []TrackedQuestRecord{corpusTrackedQuestWithOptional()}
				return character
			},
			equipItemsEnabled: true,
		},
	}
}

func buildEnterCorpus(t *testing.T) enterCorpus {
	t.Helper()
	liveRecords := liveItemNativeFieldsByCodename(t)
	// The avatar vectors resolve their rows from the SHIPPED itemdata (the
	// same production TextdataItems pipeline the live server reads), so
	// their refItemSnapshot rows carry the full real 160-column records -
	// never invented minima. Skips when this checkout has no media (the
	// liveItemNativeFieldsByCodename posture).
	shippedItems := sharedShippedItems(t)
	magicOptions := sharedShippedMagicOptions(t)
	vectors := make([]enterCorpusVector, 0, len(enterCorpusSpecs()))
	for _, spec := range enterCorpusSpecs() {
		character := spec.character()
		deps := testDeps(character)
		deps.EquipItemsEnabled = spec.equipItemsEnabled
		deps.MagicOptions = magicOptions
		// Dress the item source with the LIVE itemdata records so every
		// refItemSnapshot row carries the real PK2 columns the client's
		// entered chain derives durability/variance from.
		items := deps.Items.(fakeItems)
		for codename, ref := range items {
			if nativeFields, ok := liveRecords[codename]; ok {
				ref.NativeFields = nativeFields
			}
		}
		// The avatar refs join the source as COPIES of the shipped rows
		// (the shared loader instance must never be mutated).
		for _, codename := range []string{corpusAvatarHatCodename, corpusAvatarSetCodename} {
			shipped, ok := shippedItems.ItemRefByCodename(codename)
			if !ok || shipped == nil {
				t.Fatalf("corpus avatar row %q does not resolve in the shipped itemdata", codename)
			}
			owned := *shipped
			items[codename] = &owned
		}
		result := Build(deps, BootstrapRequest{CharacterName: character.Name})
		if result.NativeResult != 1 {
			t.Fatalf("corpus vector %q: bootstrap failed: %+v", spec.label, result)
		}
		if result.LocalPlayerEntry == nil {
			t.Fatalf("corpus vector %q: bootstrap result lacks localPlayerEntry", spec.label)
		}
		packets := make([]enteredFixturePacket, 0, len(result.Packets))
		for _, packet := range result.Packets {
			body := make([]byte, len(packet.Payload))
			for index, value := range packet.Payload {
				body[index] = byte(value)
			}
			packets = append(packets, enteredFixturePacket{
				NativeOpcode: packet.NativeOpcode,
				PayloadHex:   hex.EncodeToString(body),
			})
		}
		vectors = append(vectors, enterCorpusVector{
			Label:         spec.label,
			Notes:         spec.notes,
			CharacterName: character.Name,
			CharacterID:   character.ID,
			Gid:           ObjectIDForCharacter(character),
			RegionID:      uint16(result.LocalPlayerEntry.StartProfile.RegionID),
			Seed: enteredFixtureSeed{
				MaxHp:          DerivedMaxHP(result.Character),
				MaxMp:          DerivedMaxMP(result.Character),
				Strength:       CharacterStrength(result.Character),
				Intellect:      CharacterIntellect(result.Character),
				ModelRef:       result.LocalPlayerEntry.ModelRef,
				SexSelector1ac: result.LocalPlayerEntry.SexSelector1AC,
				CountryByte9c:  result.LocalPlayerEntry.CountryByte9C,
			},
			Packets:             packets,
			RefItemSnapshot:     result.RefItemSnapshot,
			MagicOptionSnapshot: result.MagicOptionSnapshot,
		})
	}
	return enterCorpus{
		Comment: []string{
			"GENERATED + PINNED by internal/game/enterworld/entercorpus_test.go (TestLocalPlayerEnterCorpusPinned).",
			"Table-driven enter-payload vectors over every axis BuildLocalPlayerEntryPayload supports",
			"today: bare/geared rosters, restored inventories (plus/variance/durability, bag-heavy),",
			"masteries/skills seeded vs empty, whisper-block counts {0,2,20 (panel 0x14 cap)},",
			"event-guide masks, completed/active/tracked quest sections (real v1.150 questdata",
			"ids by codename; active SQuestInfo bodies flags 0x00 and 0x5c incl. the 0xFF-sentinel",
			"contents node and an npcpos-valid targetId; tracked records with and without the",
			"flags&0x02 optional - no enter axis is composer-hardcoded empty anymore),",
			"entered-tail event-group lists (id 1 = the render-option/guide-message leg),",
			"per-item magic options (real v1.150 magicoption.txt ids by MATTR codename+degree;",
			"reinforce leg incl. the hidden '-@' row and the MATTR_REINFORCE_ITEM flag row;",
			"definitions ride each vector's magicOptionSnapshot), avatar inventories (real v1.150",
			"itemdata avatar-band rows, full shipped records on the refItemSnapshot),",
			"and the CH-male/CH-female/EU-male start profiles.",
			"Regenerate: UPDATE_ENTER_CORPUS=1 go test ./internal/game/enterworld/ -run TestLocalPlayerEnterCorpusPinned",
		},
		Vectors: vectors,
	}
}

// TestEnterCorpusContentPins cross-pins the corpus content constants against
// the SHIPPED v1.150 tables (the TestCreationVitalsCrossPin posture): every
// magic-option param id must resolve by MATTR codename + degree to exactly
// the pinned id with the pinned paramName, and every avatar codename must
// resolve to the pinned refObjId whose packed word is the pinned
// avatar-band value. A table swap that re-points a codename fails HERE, not
// as a silent content drift inside the pinned packet bytes.
func TestEnterCorpusContentPins(t *testing.T) {
	magicOptions := sharedShippedMagicOptions(t)
	magicPins := []struct {
		codename  string
		degree    int64
		paramID   uint32
		paramName string
	}{
		{"MATTR_STR", 1, corpusMagicStrParamID, "+"},
		{"MATTR_INT", 1, corpusMagicIntParamID, "+"},
		{"MATTR_DUR", 2, corpusMagicDurParamID, "+"},
		{"MATTR_DEC_MAXDUR", 1, corpusMagicDecMaxDurParamID, "-@"},
		{"MATTR_REINFORCE_ITEM", 1, corpusMagicReinforceParamID, "+"},
	}
	for _, pin := range magicPins {
		row, ok := magicOptions.MagicOptionByCodenameDegree(pin.codename, pin.degree)
		if !ok {
			t.Fatalf("magicoption %s degree %d does not resolve in the shipped table", pin.codename, pin.degree)
		}
		if row.ParamID != pin.paramID || row.ParamName != pin.paramName {
			t.Fatalf("magicoption %s degree %d = id %d paramName %q, pinned id %d paramName %q - re-verify the corpus content constants",
				pin.codename, pin.degree, row.ParamID, row.ParamName, pin.paramID, pin.paramName)
		}
	}

	shippedItems := sharedShippedItems(t)
	avatarPins := []struct {
		codename  string
		refObjID  uint32
		typeFlags uint16
	}{
		{corpusAvatarHatCodename, corpusAvatarHatRefID, corpusAvatarHatTypeFlags},
		{corpusAvatarSetCodename, corpusAvatarSetRefID, corpusAvatarSetTypeFlags},
	}
	for _, pin := range avatarPins {
		row, ok := shippedItems.ItemRefByCodename(pin.codename)
		if !ok || row == nil {
			t.Fatalf("avatar row %q does not resolve in the shipped itemdata", pin.codename)
		}
		if row.RefObjID != pin.refObjID || row.TypeFlags() != pin.typeFlags {
			t.Fatalf("avatar row %q = ref %d word 0x%04x, pinned ref %d word 0x%04x",
				pin.codename, row.RefObjID, row.TypeFlags(), pin.refObjID, pin.typeFlags)
		}
		if !wire.IsAvatarBandTid(pin.typeFlags) {
			t.Fatalf("avatar row %q pinned word 0x%04x is not in the (typeFlags & 0x780) == 0x680 avatar band", pin.codename, pin.typeFlags)
		}
	}

	// Quest content pins: codename -> id over the shipped questdata.txt
	// (col 0 Service, col 1 id, col 2 codename - the sub_810620 parse
	// order the buildQuestDataAsset.mjs contract documents).
	questIDByCodename := map[string]uint32{}
	for _, fields := range readTextdataFile(filepath.Join(realAssetPaths(t).TextdataDir, "questdata.txt")) {
		if len(fields) < 3 || fields[0] != "1" {
			continue
		}
		if id, ok := textdataInt(fields[1]); ok {
			if _, seen := questIDByCodename[fields[2]]; !seen {
				questIDByCodename[fields[2]] = uint32(id)
			}
		}
	}
	questPins := []struct {
		codename string
		id       uint32
	}{
		{"QTUTORIAL_CH", corpusQuestTutorialChID},
		{"QNO_CH_SMITH_1", corpusQuestSmith1ID},
		{"QNO_CH_CHEF_1", corpusQuestChef1ID},
		{"QNO_CH_SOLDIER_EA1_1", corpusQuestSoldierEa1ID},
		{"QSP_ALL_POTION_1", corpusQuestPotion1ID},
	}
	for _, pin := range questPins {
		if got, ok := questIDByCodename[pin.codename]; !ok || got != pin.id {
			t.Fatalf("questdata %s = id %d/%v, pinned %d - re-verify the corpus quest constants", pin.codename, got, ok, pin.id)
		}
	}

	// The full-record targetId must resolve in the shipped npcpos.txt (the
	// client minimap quest pass map::at THROWS on a miss once the quest is
	// tracked, sub_7e1240 @0x007e126d).
	npcPosFound := false
	for _, fields := range readTextdataFile(filepath.Join(realAssetPaths(t).TextdataDir, "npcpos.txt")) {
		if len(fields) >= 1 {
			if id, ok := textdataInt(fields[0]); ok && uint32(id) == corpusQuestPotionNpcID {
				npcPosFound = true
				break
			}
		}
	}
	if !npcPosFound {
		t.Fatalf("npcpos.txt carries no row for the pinned targetId %d (NPC_CH_POTION)", corpusQuestPotionNpcID)
	}
}

// TestLocalPlayerEnterCorpusPinned regenerates the corpus through the REAL
// emitter and requires the checked-in artifact to match byte for byte (the
// TestLocalPlayerEnteredFixturePinned two-sided precedent).
func TestLocalPlayerEnterCorpusPinned(t *testing.T) {
	path := filepath.Join("testdata", "local_player_enter_corpus.json")
	fresh := buildEnterCorpus(t)

	if os.Getenv("UPDATE_ENTER_CORPUS") == "1" {
		blob, err := json.MarshalIndent(fresh, "", "  ")
		if err != nil {
			t.Fatalf("marshal corpus: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, append(blob, '\n'), 0o644); err != nil {
			t.Fatalf("write corpus: %v", err)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("corpus missing (%v) - run with UPDATE_ENTER_CORPUS=1 to generate", err)
	}
	var pinned enterCorpus
	if err := json.Unmarshal(raw, &pinned); err != nil {
		t.Fatalf("corpus parse: %v", err)
	}
	if len(pinned.Vectors) != len(fresh.Vectors) {
		t.Fatalf("corpus has %d vectors, emitter builds %d - regenerate", len(pinned.Vectors), len(fresh.Vectors))
	}
	for index, vector := range fresh.Vectors {
		got := pinned.Vectors[index]
		if got.Label != vector.Label || got.Gid != vector.Gid {
			t.Fatalf("vector[%d] identity = %s/%d, emitter builds %s/%d - regenerate",
				index, got.Label, got.Gid, vector.Label, vector.Gid)
		}
		if len(got.Packets) != len(vector.Packets) {
			t.Fatalf("vector %q has %d packets, emitter builds %d - regenerate", vector.Label, len(got.Packets), len(vector.Packets))
		}
		for packetIndex, packet := range vector.Packets {
			pinnedPacket := got.Packets[packetIndex]
			// Calendar is live server state; the corpus pins the GID and wire
			// shape, while calendar's native vectors pin time decomposition.
			// Do not regenerate a recording with a nondeterministic timestamp.
			if packet.NativeOpcode == OpcodeServerClockGidLatch && pinnedPacket.NativeOpcode == packet.NativeOpcode {
				body, err := hex.DecodeString(packet.PayloadHex)
				if err != nil || len(body) != 8 || body[6] >= 24 || body[7] >= 60 || len(pinnedPacket.PayloadHex) != 16 || packet.PayloadHex[:8] != pinnedPacket.PayloadHex[:8] {
					t.Errorf("invalid live clock latch: %s", packet.PayloadHex)
				}
				continue
			}
			if pinnedPacket.NativeOpcode != packet.NativeOpcode || pinnedPacket.PayloadHex != packet.PayloadHex {
				t.Errorf("vector %q packet[%d] drifted from the pinned corpus:\n got 0x%04X %s\nwant 0x%04X %s\n(regenerate with UPDATE_ENTER_CORPUS=1 and re-run the harness soak)",
					vector.Label, packetIndex, packet.NativeOpcode, packet.PayloadHex, pinnedPacket.NativeOpcode, pinnedPacket.PayloadHex)
			}
		}
		// Sequence-shape sanity: the accumulation contract the client's
		// 0x379d/0x32b3/0x31db chain depends on.
		wantLeading := []uint16{
			OpcodeResetClient,
			OpcodeMyCharacterData,
			OpcodeMyCharacterChunk,
			OpcodeMyCharacterFlush,
			OpcodeServerClockGidLatch,
		}
		for opcodeIndex, want := range wantLeading {
			if got.Packets[opcodeIndex].NativeOpcode != want {
				t.Errorf("vector %q packet[%d] opcode = 0x%04X, want 0x%04X", vector.Label, opcodeIndex, got.Packets[opcodeIndex].NativeOpcode, want)
			}
		}
		if got.Packets[2].PayloadHex == "" {
			t.Errorf("vector %q: the 0x32B3 chunk payload is empty - the entered chain would deserialize nothing", vector.Label)
		}
	}
}
