/*
===========================================================================

hwan_chain.go - the Army / Military Test chains of the Blue Berserk (95 and 100)

Port-only, not native. The quests that turn a Berserker blue: chain 1 at
character level 95 (QNO_{CH,EU}_HWAN_1_x) and chain 2 at level 100
(QNO_{CH,EU}_HWAN_2_x), for the Chinese (General Sonhyeon, Exorcist Miaoryeong,
Hunter Associate Gwakwi, Priest Jeonghye) and the Europeans (General Ratchel,
Sunset Witch, Hunter Associate Adria; the Priest is shared). Mechanics come from
the 1.188-era game data: the steps, items, counts and rolls; the quest rows,
texts and items from the later client (scripts/cap110_quest_tables.py).

	_1_1 / _2_1   the trial in a private arena (hwan_trial.go)
	_1_2 / _2_2   talk to the Exorcist / Witch
	_1_3          Stone Monster's Bell: 50% from the three Ch'in Tomb stone ghosts
	_1_3_1        the Priest purifies it; the bell summons the Old Tombstone
	              Ghost southeast of Jangan, whose death leaves the Soul Shell
	_1_4          carry the Exorcist's charm to the hunter
	_1_4_1        the Niya trap: three-way roll (SRO_HWAN_NIYA_CAPTURE_RATES)
	_2_3          Mountain Essence (from the Goats) used on a Yeoha: Branch of Life
	_2_4          Radiant Crystals (from Ishade) dug at Cleopatra's Gate: Relic of
	              the Sun God (SRO_HWAN_SUN_RELIC)
	_1_5 / _2_5   the soul ritual (hwan_ritual.go)
	_1_6 / _2_6   deliver the Soul Fragment: Blue Berserk level 1 / 2

Chain 2 needs level 100 and chain 1; chain 1 needs level 95. The quest rows
exist only while SRO_CAP110_QUESTS built them; generated.go admits a spec only
when its questdata row exists.

===========================================================================
*/
package quest

import "fmt"

// hwanRace is what differs between the two races' chains.
type hwanRace struct {
	tag                     string // CH or EU
	general, guide, hunter  string // General, Exorcist / Witch, Hunter Associate
	charm, ritualOrb1, orb2 string // the 1_4 carried item, the 1_5 and 2_5 sight items
	trap2                   string // the Annihilation Trap
}

const hwanPriest = "NPC_CH_PRIEST"

var hwanRaces = [2]hwanRace{
	{tag: "CH", general: "NPC_CH_GENARAL", guide: "NPC_CH_SHAMAN", hunter: "NPC_CH_GENARAL_SW",
		charm: "ITEM_QNO_CH_HWAN_1_4_03", ritualOrb1: "ITEM_QNO_CH_HWAN_1_5_01", orb2: "ITEM_QNO_CH_HWAN_2_5_01",
		trap2: "ITEM_QNO_CH_WHAN_2_1_01"},
	{tag: "EU", general: "NPC_EU_GENERAL", guide: "NPC_EU_WITCH", hunter: "NPC_EU_HUNTER",
		charm: "ITEM_QNO_EU_HWAN_1_4_01", ritualOrb1: "ITEM_QNO_EU_HWAN_1_5_01", orb2: "ITEM_QNO_EU_HWAN_2_5_01",
		trap2: "ITEM_QNO_EU_WHAN_2_1_01"},
}

// The quest items both races share (the European chain reuses the Chinese rows).
const (
	hwanBell               = "ITEM_QNO_CH_HWAN_1_3_01"
	hwanPurifiedBell       = "ITEM_QNO_CH_HWAN_1_3_02"
	hwanShell              = "ITEM_QNO_CH_HWAN_1_3_03"
	hwanNormalSpirit       = "ITEM_QNO_CH_HWAN_1_4_01"
	hwanFlutterSpirit      = "ITEM_QNO_CH_HWAN_1_4_02"
	hwanSpiritTrap         = "ITEM_QNO_CH_HWAN_1_4_04"
	hwanSoulFragment       = "ITEM_QNO_CH_HWAN_1_5_02"
	hwanMountainEssence    = "ITEM_QNO_CH_WHAN_2_3_01"
	hwanBranchOfLife       = "ITEM_QNO_CH_WHAN_2_3_02"
	hwanRadiantCrystal     = "ITEM_QNO_CH_WHAN_2_4_01"
	hwanSunRelic           = "ITEM_QNO_CH_WHAN_2_4_02"
	hwanUnderworldFragment = "ITEM_QNO_CH_HWAN_2_5_02"
)

// hwanFlutterGold is the pay step's gold per Flutter Spirit and its cap.
const (
	hwanFlutterGold    = 10
	hwanFlutterGoldCap = 10000000
)

// hwanQuest names a quest of a race's chain: chain 1 or 2, step like "3_1".
func hwanQuest(race hwanRace, chain int, step string) string {
	return fmt.Sprintf("QNO_%s_HWAN_%d_%s", race.tag, chain, step)
}

// hwanTalk is the talk-symbol prefix of a quest.
func hwanTalk(codename string) string { return "SN_TALK_" + codename + "_" }

func hwanLevel(chain int) uint8 {
	if chain == 2 {
		return 100
	}
	return 95
}

// hwanBase fills the fields every quest of the chains shares.
func hwanBase(race hwanRace, chain int, step, start, end string, previous string) QuestSpec {
	name := hwanQuest(race, chain, step)
	talk := hwanTalk(name)
	spec := QuestSpec{
		Codename: name, KindByte: 1, MaxCompletions: 1, MinLevel: hwanLevel(chain),
		StartNpcCodename: start, EndNpcCodename: end,
		OfferPromptSymbol: talk + "01", AcceptResponseSymbol: talk + "02", DenyResponseSymbol: talk + "03",
		CompletePromptSymbol: talk + "05",
	}
	if previous != "" {
		spec.RequiredQuests = []string{previous}
	}
	return spec
}

// hwanTalkQuest is a dialog quest: the General (or the guide) sends you on.
func hwanTalkQuest(race hwanRace, chain int, step, start, end, previous string) QuestSpec {
	spec := hwanBase(race, chain, step, start, end, previous)
	talk := hwanTalk(spec.Codename)
	spec.Objective = ObjectiveTalk
	spec.NotAchievedSymbol = talk + "04"
	spec.AcceptNoticeSymbol = talk + "06"
	return spec
}

// hwanDelivery is a quest that hands one item over at acceptance and takes it
// at the end NPC.
func hwanDelivery(spec QuestSpec, item string, count uint32) QuestSpec {
	spec.Objective = ObjectiveDelivery
	spec.DeliveryItems = []RewardItemLead{{ItemCodename: item, Count: count}}
	return spec
}

/*
================
hwanChainSpecs

The quests of both races and both chains that need no arena: the talks, the
bell pair, the charm, the trap, the Branch and the Relic, and the two
deliveries. The trials and the rituals are hwanArenaSpecs.
================
*/
func hwanChainSpecs() []QuestSpec {
	var specs []QuestSpec
	for _, race := range hwanRaces {
		// ---- chain 1 ----
		step2 := hwanTalkQuest(race, 1, "2", race.general, race.guide, hwanQuest(race, 1, "1"))
		if race.tag == "EU" {
			// The European client text lacks the refusal line _03; the Chinese one is the same words.
			step2.DenyResponseSymbol = hwanTalk(hwanQuest(hwanRaces[0], 1, "2")) + "03"
		}
		specs = append(specs, step2)

		bell := hwanBase(race, 1, "3", race.guide, hwanPriest, hwanQuest(race, 1, "2"))
		bell.Objective = ObjectiveCollect
		bell.CollectItemCodename, bell.CollectCount = hwanBell, 1
		bell.NotAchievedSymbol = hwanTalk(bell.Codename) + "04"
		bell.AcceptNoticeSymbol = hwanTalk(bell.Codename) + "06"
		bell.AchievedNowSymbol = hwanTalk(bell.Codename) + "07"
		bell.MonsterDrop = &MonsterDropRule{
			MonsterCodenames: []string{"MOB_TQ_TOMBSTONEGHOST", "MOB_TQ_TOMBSTONEGHOST_CLON", "MOB_TQ_TOMBSTONEGHOST_CLON2"},
			ChancePercent:    50}
		specs = append(specs, bell)

		shellName := hwanQuest(race, 1, "3_1")
		shellTalk := hwanTalk(shellName)
		shell := hwanBase(race, 1, "3_1", hwanPriest, race.guide, hwanQuest(race, 1, "3"))
		shell.Objective = ObjectiveCollect
		shell.CollectItemCodename, shell.CollectCount = hwanShell, 1
		// "Imbue with the power of the Buddha" accepts; the purified bell is the supply
		// (hwanSupplies); the Old Tombstone Ghost it summons leaves the shell.
		shell.OfferAcceptRowSymbol = shellTalk + "02"
		shell.AcceptResponseSymbol, shell.DenyResponseSymbol = shellTalk+"03", shellTalk+"04"
		shell.NotAchievedSymbol, shell.InventoryFullSymbol = shellTalk+"05", shellTalk+"06"
		shell.CompletePromptSymbol = shellTalk + "07"
		shell.AcceptNoticeSymbol, shell.AchievedNowSymbol = shellTalk+"08", shellTalk+"09"
		shell.MonsterDrop = &MonsterDropRule{MonsterCodenames: []string{"MOB_QT_CH_TOMBSTONE_01"}, ChancePercent: 100}
		specs = append(specs, shell)

		charmName := hwanQuest(race, 1, "4")
		charmTalk := hwanTalk(charmName)
		charm := hwanDelivery(hwanBase(race, 1, "4", race.guide, race.hunter, hwanQuest(race, 1, "3_1")), race.charm, 1)
		charm.InventoryFullSymbol, charm.CompletePromptSymbol = charmTalk+"04", charmTalk+"05"
		charm.AcceptNoticeSymbol, charm.NotAchievedSymbol = charmTalk+"06", charmTalk+"07"
		specs = append(specs, charm)

		trapName := hwanQuest(race, 1, "4_1")
		trapTalk := hwanTalk(trapName)
		trap := hwanBase(race, 1, "4_1", race.hunter, race.guide, hwanQuest(race, 1, "4"))
		trap.Objective = ObjectiveCollect
		trap.CollectItemCodename, trap.CollectCount = hwanNormalSpirit, 1
		trap.OfferPromptSymbol, trap.AcceptResponseSymbol = trapTalk+"01", trapTalk+"02"
		trap.DenyResponseSymbol = trapTalk + "10"
		trap.NotAchievedSymbol, trap.CompletePromptSymbol = trapTalk+"12", trapTalk+"06"
		trap.AcceptNoticeSymbol, trap.AchievedNowSymbol = trapTalk+"07", trapTalk+"08"
		trap.InventoryFullSymbol = hwanTalk(charmName) + "04"
		trap.HeldItemGold = []HeldItemGold{{ItemCodename: hwanFlutterSpirit, GoldEach: hwanFlutterGold, GoldCap: hwanFlutterGoldCap}}
		trap.ClearOnEnd = []string{hwanFlutterSpirit}
		specs = append(specs, trap)

		fragment := hwanDelivery(hwanBase(race, 1, "6", race.guide, race.general, hwanQuest(race, 1, "5")), hwanSoulFragment, 1)
		fragmentTalk := hwanTalk(fragment.Codename)
		fragment.OfferPromptSymbol, fragment.AcceptResponseSymbol = fragmentTalk+"01", fragmentTalk+"02"
		fragment.DenyResponseSymbol, fragment.NotAchievedSymbol = fragmentTalk+"03", fragmentTalk+"04"
		fragment.InventoryFullSymbol, fragment.CompletePromptSymbol = fragmentTalk+"05", fragmentTalk+"06"
		fragment.AcceptNoticeSymbol, fragment.CompleteNoticeSymbol = fragmentTalk+"07", fragmentTalk+"08"
		fragment.RewardHwanLevel = 1
		specs = append(specs, fragment)

		// ---- chain 2 ----
		specs = append(specs, hwanTalkQuest(race, 2, "2", race.general, race.guide, hwanQuest(race, 2, "1")))

		branchName := hwanQuest(race, 2, "3")
		branchTalk := hwanTalk(branchName)
		branch := hwanBase(race, 2, "3", race.guide, race.guide, hwanQuest(race, 2, "2"))
		branch.Objective = ObjectiveCollect
		branch.CollectItemCodename, branch.CollectCount = hwanBranchOfLife, 1
		branch.NotAchievedSymbol = branchTalk + "04"
		branch.AcceptNoticeSymbol, branch.AchievedNowSymbol = branchTalk+"06", branchTalk+"08"
		// The supply is the essence the Goats leave (100% each); it gates nothing.
		branch.MonsterDrop = &MonsterDropRule{ItemCodename: hwanMountainEssence,
			MonsterCodenames: []string{"MOB_RM_GOAT", "MOB_RM_GOAT_CLON"}, ChancePercent: 100, MaxHeld: hwanSupplyHeld}
		branch.ClearOnEnd = []string{hwanMountainEssence}
		specs = append(specs, branch)

		relicName := hwanQuest(race, 2, "4")
		relicTalk := hwanTalk(relicName)
		relic := hwanBase(race, 2, "4", race.guide, race.guide, hwanQuest(race, 2, "3"))
		relic.Objective = ObjectiveCollect
		relic.CollectItemCodename, relic.CollectCount = hwanSunRelic, 1
		relic.NotAchievedSymbol = relicTalk + "04"
		relic.AcceptNoticeSymbol, relic.AchievedNowSymbol = relicTalk+"06", relicTalk+"08"
		relic.MonsterDrop = &MonsterDropRule{ItemCodename: hwanRadiantCrystal,
			MonsterCodenames: []string{"MOB_KK_ISHADE", "MOB_KK_ISHADE_CLON"}, ChancePercent: 100, MaxHeld: hwanSupplyHeld}
		relic.ClearOnEnd = []string{hwanRadiantCrystal}
		specs = append(specs, relic)

		underworld := hwanDelivery(hwanBase(race, 2, "6", race.guide, race.general, hwanQuest(race, 2, "5")), hwanUnderworldFragment, 1)
		underworldTalk := hwanTalk(underworld.Codename)
		underworld.OfferPromptSymbol, underworld.AcceptResponseSymbol = underworldTalk+"01", underworldTalk+"02"
		underworld.DenyResponseSymbol, underworld.NotAchievedSymbol = underworldTalk+"03", underworldTalk+"04"
		underworld.InventoryFullSymbol, underworld.CompletePromptSymbol = underworldTalk+"05", underworldTalk+"06"
		underworld.AcceptNoticeSymbol, underworld.CompleteNoticeSymbol = underworldTalk+"07", underworldTalk+"08"
		underworld.RewardHwanLevel = 2
		specs = append(specs, underworld)
	}
	return specs
}

// hwanSupplyHeld bounds the supply items one quest lets a character hold: the
// display count of the Branch and Relic quests (100).
const hwanSupplyHeld = 100

// hwanChainQuestSpecs is the registered table: the chain quests and the arena quests.
var hwanChainQuestSpecs = append(hwanChainSpecs(), hwanArenaSpecs()...)

/*
================
hwanSupplies

The tools the Blue Berserk quests hand out and hand out again: the purified
bell of _1_3_1 (one, whenever none is held and the shell is not yet found) and
the Niya traps of _1_4_1 (a hundred, whenever none is left: the hunter's
"Need more traps?"). Both are granted at acceptance and again on a visit with
none held, with no daily allowance.
================
*/
var hwanSupplies = hwanSupplyRows()

func hwanSupplyRows() []captureSupply {
	var rows []captureSupply
	for _, race := range hwanRaces {
		bell := hwanQuest(race, 1, "3_1")
		bellTalk := hwanTalk(bell)
		trap := hwanQuest(race, 1, "4_1")
		trapTalk := hwanTalk(trap)
		rows = append(rows,
			captureSupply{quest: bell, item: hwanPurifiedBell, count: 1, unlimited: true,
				title: bellTalk + "02", prompt: bellTalk + "01", success: bellTalk + "03", full: bellTalk + "06"},
			captureSupply{quest: trap, item: hwanSpiritTrap, count: hwanTrapSupply, unlimited: true,
				title: trapTalk + "04", prompt: trapTalk + "04", success: trapTalk + "02", full: hwanTalk(hwanQuest(race, 1, "4")) + "04"})
	}
	return rows
}

// hwanTrapSupply is the number of Traps for Spirit one hand-out holds (a full stack).
const hwanTrapSupply = 100
