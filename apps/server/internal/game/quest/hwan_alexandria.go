/*
===========================================================================

hwan_alexandria.go - the Alexandria Blue Berserk quests (QNO_SD_HW_001 to 024)

Port-only, not native. The levels 105 and 110 steps of the Blue Berserk line:
Governor Senmute, Doctor Renenutet and the Generals of Jangan and
Constantinople. The quest rows, texts and items come from the later client
(scripts/cap110_quest_tables.py, cap110_quest_items.py); these specs are the
mechanics, from the 1.188-era game data:

	017 / 021  level 105, the General (Chinese / European) sends you to
	           Senmute; needs the finished 100 chain (QNO_*_HWAN_2_6)
	001        level 105, Senmute sends you to Renenutet
	002 - 008  level 105, collect 100 ostracons of each family (50 of the
	           gods) from the listed monsters of Pharaoh Tomb 2, 100% drops
	018 / 022  level 105, Renenutet hands you the inscription; deliver it to
	           the General: Blue Berserk level 3
	019 / 023  level 110, the General sends you to Senmute again
	009        level 110, Senmute sends you to Renenutet
	010 - 016  level 110, the same ostracons from Pharaoh Tomb 3
	020 / 024  level 110, the advanced inscription: Blue Berserk level 4

Each quest is available once. The rewards are the shard's experience, gold and
skill points; the quest rows exist only while SRO_CAP110_QUESTS built them, so
a shard without them loads none of these specs (generated.go admits an
additional spec only when its questdata row exists).

===========================================================================
*/
package quest

import "fmt"

// hwanQuestCodename formats the three-digit quest number of a codename.
func hwanQuestCodename(number int) string {
	return fmt.Sprintf("QNO_SD_HW_%03d", number)
}

// hwanTombMonsters appends the monster variant of a level: _2 in Pharaoh Tomb 2
// (105) and _3 in Pharaoh Tomb 3 (110).
func hwanTombMonsters(base []string, suffix string) []string {
	out := make([]string, 0, len(base))
	for _, name := range base {
		out = append(out, name+suffix)
	}
	return out
}

// hwanOstraconFamilies lists the seven collection quests by their position
// in the chain (002 to 008 at 105, 010 to 016 at 110).
var hwanOstraconFamilies = []struct {
	monsters []string
	count    uint32
}{
	{[]string{"MOB_SD_LLIGHTINGKHEPRI", "MOB_SD_DEMONBUG"}, 100},
	{[]string{"MOB_SD_MONT", "MOB_SD_KEISAS"}, 100},
	{[]string{"MOB_SD_HERON"}, 100},
	{[]string{"MOB_SD_TELBASTA", "MOB_SD_BASTET", "MOB_SD_BAST"}, 100},
	{[]string{"MOB_SD_SAND_HYENA", "MOB_SD_CHIEF_HYENA"}, 100},
	{[]string{"MOB_SD_MUMMY_RAGE", "MOB_SD_MUMMY_HUBRIS"}, 100},
	{[]string{"MOB_SD_SPHINX", "MOB_SD_NEPHTHYS", "MOB_SD_SEKHMET", "MOB_SD_HORUS", "MOB_SD_OSIRIS"}, 50},
}

// The shard's rewards: experience, gold and skill points per quest group.
type hwanReward struct{ exp, gold, skill int64 }

var (
	hwanDialogReward105     = hwanReward{5412921, 2450, 1100}
	hwanOstraconReward105   = hwanReward{10825843, 4900, 1600}
	hwanLastOstracon105     = hwanReward{54129218, 49437, 3100}
	hwanDialogReward110     = hwanReward{5412921, 2450, 1100}
	hwanOpenerReward110     = hwanReward{6275059, 2559, 1600}
	hwanOstraconReward110   = hwanReward{12550119, 5118, 2100}
	hwanLastOstracon110     = hwanReward{62750595, 25593, 3600}
	hwanInscriptionReward   = hwanReward{5412921, 2450, 1100}
	hwanGeneralChinese      = "NPC_CH_GENARAL"
	hwanGeneralEuropean     = "NPC_EU_GENERAL"
	hwanGovernor            = "NPC_SD_M_AREA_SENMUTE"
	hwanDoctor              = "NPC_SD_M_AREA_LENENUNE"
	hwanChineseLastOfChain  = "QNO_CH_HWAN_2_6"
	hwanEuropeanLastOfChain = "QNO_EU_HWAN_2_6"
)

// hwanTalkSpec is a dialog quest (Senmute or a General sends you on).
func hwanTalkSpec(number int, level uint8, start, end string, reward hwanReward) QuestSpec {
	name := hwanQuestCodename(number)
	talk := "SN_TALK_" + name + "_"
	return QuestSpec{
		Codename: name, KindByte: 1, MaxCompletions: 1, MinLevel: level, Objective: ObjectiveTalk,
		StartNpcCodename: start, EndNpcCodename: end,
		OfferPromptSymbol: talk + "01", AcceptResponseSymbol: talk + "02", DenyResponseSymbol: talk + "03",
		NotAchievedSymbol: talk + "04", CompletePromptSymbol: talk + "05",
		RewardExp: reward.exp, RewardGold: reward.gold, RewardSkillExp: reward.skill,
	}
}

// hwanOstraconSpec is a collection quest at Doctor Renenutet.
func hwanOstraconSpec(number int, level uint8, previous string, family int, suffix string, reward hwanReward) QuestSpec {
	name := hwanQuestCodename(number)
	talk := "SN_TALK_" + name + "_"
	item := fmt.Sprintf("ITEM_QNO_SD_HW_%03d_1", number)
	monsters := hwanTombMonsters(hwanOstraconFamilies[family].monsters, suffix)
	return QuestSpec{
		Codename: name, KindByte: 1, MaxCompletions: 1, MinLevel: level, Objective: ObjectiveCollect,
		RequiredQuests:      []string{previous},
		CollectItemCodename: item, CollectCount: hwanOstraconFamilies[family].count,
		// Every listed drop chance is 100%; SRO_HWAN_3_FAST_DROP (hwanknobs.go)
		// makes a kill leave ten instead of one.
		MonsterDrop:      &MonsterDropRule{MonsterCodenames: monsters, ChancePercent: 100, FastDrop: true},
		StartNpcCodename: hwanDoctor, EndNpcCodename: hwanDoctor,
		OfferPromptSymbol: talk + "01", AcceptResponseSymbol: talk + "02", DenyResponseSymbol: talk + "03",
		NotAchievedSymbol: talk + "04", CompletePromptSymbol: talk + "05", AchievedNowSymbol: talk + "06",
		RewardExp: reward.exp, RewardGold: reward.gold, RewardSkillExp: reward.skill,
	}
}

// hwanInscriptionSpec is the delivery that raises the Blue Berserk level.
func hwanInscriptionSpec(number int, level uint8, previous, general string, hwanLevel uint8) QuestSpec {
	name := hwanQuestCodename(number)
	talk := "SN_TALK_" + name + "_"
	return QuestSpec{
		Codename: name, KindByte: 1, MaxCompletions: 1, MinLevel: level, Objective: ObjectiveDelivery,
		RequiredQuests:   []string{previous},
		DeliveryItems:    []RewardItemLead{{ItemCodename: fmt.Sprintf("ITEM_QNO_SD_HW_%03d_1", number), Count: 1}},
		StartNpcCodename: hwanDoctor, EndNpcCodename: general,
		// The texts run offer, refusal, "was it not for this inscription"
		// (the answer to accepting), the hand-over reminder, the full-bag
		// line and the General's thanks.
		OfferPromptSymbol: talk + "01", DenyResponseSymbol: talk + "02", AcceptResponseSymbol: talk + "03",
		AchievedNowSymbol: talk + "04", InventoryFullSymbol: talk + "05", CompletePromptSymbol: talk + "06",
		RewardExp: hwanInscriptionReward.exp, RewardGold: hwanInscriptionReward.gold, RewardSkillExp: hwanInscriptionReward.skill,
		RewardHwanLevel: hwanLevel,
	}
}

/*
================
hwanAlexandriaSpecs

The 24 quests, in chain order.
================
*/
func hwanAlexandriaSpecs() []QuestSpec {
	var specs []QuestSpec
	chineseOpen := hwanTalkSpec(17, 105, hwanGeneralChinese, hwanGovernor, hwanDialogReward105)
	chineseOpen.RequiredQuests = []string{hwanChineseLastOfChain}
	europeanOpen := hwanTalkSpec(21, 105, hwanGeneralEuropean, hwanGovernor, hwanDialogReward105)
	europeanOpen.RequiredQuests = []string{hwanEuropeanLastOfChain}
	specs = append(specs, chineseOpen, europeanOpen)
	opener := hwanTalkSpec(1, 105, hwanGovernor, hwanDoctor, hwanDialogReward105)
	opener.RequiredAnyQuests = []string{hwanQuestCodename(17), hwanQuestCodename(21)}
	specs = append(specs, opener)
	for family := range hwanOstraconFamilies {
		reward := hwanOstraconReward105
		if family == len(hwanOstraconFamilies)-1 {
			reward = hwanLastOstracon105
		}
		number := 2 + family
		specs = append(specs, hwanOstraconSpec(number, 105, hwanQuestCodename(number-1), family, "_2", reward))
	}
	specs = append(specs,
		hwanInscriptionSpec(18, 105, hwanQuestCodename(8), hwanGeneralChinese, 3),
		hwanInscriptionSpec(22, 105, hwanQuestCodename(8), hwanGeneralEuropean, 3))
	chineseAdvanced := hwanTalkSpec(19, 110, hwanGeneralChinese, hwanGovernor, hwanDialogReward110)
	chineseAdvanced.RequiredQuests = []string{hwanQuestCodename(18)}
	europeanAdvanced := hwanTalkSpec(23, 110, hwanGeneralEuropean, hwanGovernor, hwanDialogReward110)
	europeanAdvanced.RequiredQuests = []string{hwanQuestCodename(22)}
	specs = append(specs, chineseAdvanced, europeanAdvanced)
	advancedOpener := hwanTalkSpec(9, 110, hwanGovernor, hwanDoctor, hwanOpenerReward110)
	advancedOpener.RequiredAnyQuests = []string{hwanQuestCodename(19), hwanQuestCodename(23)}
	specs = append(specs, advancedOpener)
	for family := range hwanOstraconFamilies {
		reward := hwanOstraconReward110
		if family == len(hwanOstraconFamilies)-1 {
			reward = hwanLastOstracon110
		}
		number := 10 + family
		specs = append(specs, hwanOstraconSpec(number, 110, hwanQuestCodename(number-1), family, "_3", reward))
	}
	specs = append(specs,
		hwanInscriptionSpec(20, 110, hwanQuestCodename(16), hwanGeneralChinese, 4),
		hwanInscriptionSpec(24, 110, hwanQuestCodename(16), hwanGeneralEuropean, 4))
	return specs
}

// hwanAlexandriaQuestSpecs is the registered table.
var hwanAlexandriaQuestSpecs = hwanAlexandriaSpecs()
