/*
===========================================================================

capture_specs.go - v1.150 ordinary captured-monster quest contracts

Primary quest text owns objectives and rewards. Native handlers supply the
capture mechanism; all references remain version-local codename joins.
Promotion traps are excluded because their quests are absent from v1.150.
The tool quests (Ivy 2, Cerberus 1, Rahid 5, Hidden Treasure 5) share the
supply and countdown owners.

===========================================================================
*/
package quest

const captureTravelBlockMask = 0x60000

var captureQuestSpecs = []QuestSpec{
	{
		// 8BAC50 adds Ivy 1 to the active-quest vector, not completion history.
		Codename: "QNO_EU_IVY_2", RequiredActiveQuests: []string{"QNO_EU_IVY_1"},
		KindByte: 1, Objective: ObjectiveCollect, CollectItemCodename: "ITEM_QNO_EU_IVY_2_02", CollectCount: 20,
		StartNpcCodename: "NPC_EU_ADVENTURER", EndNpcCodename: "NPC_EU_ADVENTURER",
		OfferPromptSymbol: "SN_TALK_QNO_EU_IVY_2_01", AcceptResponseSymbol: "SN_TALK_QNO_EU_IVY_2_02",
		DenyResponseSymbol: "SN_TALK_QNO_EU_IVY_2_03", NotAchievedSymbol: "SN_TALK_QNO_EU_IVY_2_04",
		CompletePromptSymbol: "SN_TALK_QNO_EU_IVY_2_07", InventoryFullSymbol: "SN_TALK_QNO_EU_IVY_2_06",
		// INFERENCE (no surviving script): _08 "Vine Stalk collect is
		// completed. Report to Demetri." is the ACHIEVED_NOW role.
		AchievedNowSymbol: "SN_TALK_QNO_EU_IVY_2_08",
		RewardExp:         60000, RewardGold: 15000, RewardSkillExp: 8000,
		RewardItems: []RewardItemLead{{ItemCodename: "ITEM_QNO_EU_IVY_2_01", Count: 5}},
		MonsterDrop: &MonsterDropRule{ItemCodename: "ITEM_QNO_EU_IVY_2_03",
			MonsterCodenames: []string{"MOB_AM_SOLDIER"}, ChancePercent: 25, MaxHeld: 500},
	},
	{
		// 8AF7E0 defines the capture; v1.150 SN_PAYCON owns its reward.
		Codename: "QNO_EU_EASTEU_14_1", RequiredQuests: []string{"QNO_EU_EASTEU_14"},
		KindByte: 1, Objective: ObjectiveCollect, CollectItemCodename: "ITEM_QNO_EU_EASTEU_14_1_02", CollectCount: 1,
		StartNpcCodename: "NPC_EU_WITCH", EndNpcCodename: "NPC_EU_WITCH", TravelBlockMask: captureTravelBlockMask,
		OfferPromptSymbol: "SN_TALK_QNO_EU_EASTEU_14_1_01", AcceptResponseSymbol: "SN_TALK_QNO_EU_EASTEU_14_1_02",
		DenyResponseSymbol: "SN_TALK_QNO_EU_EASTEU_14_1_03", NotAchievedSymbol: "SN_TALK_QNO_EU_EASTEU_14_1_04",
		CompletePromptSymbol: "SN_TALK_QNO_EU_EASTEU_14_1_07", InventoryFullSymbol: "SN_TALK_QNO_EU_EASTEU_14_1_06",
		RewardExp: 8500, RewardSkillExp: 2100,
		RewardItems: []RewardItemLead{{ItemCodename: "ITEM_QNO_EU_EASTEU_14_1_01", Count: 3}},
	},
	{
		// The primary contents chain links the pirate letter to this capture.
		Codename: "QNO_EU_GENERAL_1", RequiredQuests: []string{"QNO_AM_FERRY1_2"},
		KindByte: 1, Objective: ObjectiveCollect, CollectItemCodename: "ITEM_QNO_EU_GENERAL_1_02", CollectCount: 1,
		StartNpcCodename: "NPC_EU_GENERAL", EndNpcCodename: "NPC_EU_GENERAL", TravelBlockMask: captureTravelBlockMask,
		OfferPromptSymbol: "SN_TALK_QNO_EU_GENERAL_1_01", AcceptResponseSymbol: "SN_TALK_QNO_EU_GENERAL_1_02",
		DenyResponseSymbol: "SN_TALK_QNO_EU_GENERAL_1_03", NotAchievedSymbol: "SN_TALK_QNO_EU_GENERAL_1_04",
		CompletePromptSymbol: "SN_TALK_QNO_EU_GENERAL_1_10", InventoryFullSymbol: "SN_TALK_QNO_EU_GENERAL_1_09",
		RewardItems: []RewardItemLead{{ItemCodename: "ITEM_QNO_EU_GENERAL_1_03", Count: 1}},
	},
	{
		// 8B97C0 installs two simultaneous missions; the capture timer is 30
		// minutes (8BA870), not the 20-minute timer of the other families.
		Codename: "QNO_EU_IVY_1", RequiredQuests: []string{"QNO_CA_HUNTER_1"},
		KindByte: 1, Objective: ObjectiveParallel, TravelBlockMask: captureTravelBlockMask,
		Objectives: []MissionSpec{
			{ContentsSymbol: "SN_CON_QNO_EU_IVY_1_01", Objective: ObjectiveCollect,
				CollectItemCodename: "ITEM_QNO_EU_IVY_1_01", CollectCount: 1},
			{ContentsSymbol: "SN_CON_QNO_EU_IVY_1_02", Objective: ObjectiveKill,
				KillMonsterCodenames: []string{"MOB_QT_02_PUNISHER_CLON"}, KillCount: 15},
		},
		StartNpcCodename: "NPC_EU_GENERAL", EndNpcCodename: "NPC_EU_GENERAL",
		OfferPromptSymbol: "SN_TALK_QNO_EU_IVY_1_01", AcceptResponseSymbol: "SN_TALK_QNO_EU_IVY_1_02",
		DenyResponseSymbol: "SN_TALK_QNO_EU_IVY_1_03", NotAchievedSymbol: "SN_TALK_QNO_EU_IVY_1_04",
		CompletePromptSymbol: "SN_TALK_QNO_EU_IVY_1_06", InventoryFullSymbol: "SN_TALK_QNO_EU_IVY_1_05",
		// INFERENCE (no surviving script): _16 "completed capture and hunt,
		// report to Ratchel" is the ACHIEVED_NOW role. The single-capture
		// quests carry none: their capture success line (captureRules)
		// already is the report banner.
		AchievedNowSymbol: "SN_TALK_QNO_EU_IVY_1_16",
		RewardExp:         24000, RewardSkillExp: 2800,
		RewardItems: []RewardItemLead{{ItemCodename: "ITEM_QNO_EU_IVY_1_02", Count: 5}},
	},
	{
		// Cerberus 1 (CQNO_EU_EASTEU_19, 8B2130): Long Scissors cut Golden
		// Apples, an apple lures a quest Ladon, and its Bloody Orbs (50%)
		// are the objective. v1.150's text asks for 20 (the class says 15)
		// and its popup pays 11,400 EXP and 2,500 skill EXP. The class lists
		// no prerequisite. Words 0x133/0x134/0x136/0x139 name the not
		// achieved, achieved, bag-full and achieved-now lines.
		Codename: "QNO_EU_EASTEU_19",
		KindByte: 1, Objective: ObjectiveCollect, CollectItemCodename: "ITEM_QNO_EU_EASTEU_19_02", CollectCount: 20,
		StartNpcCodename: "NPC_EU_WITCH", EndNpcCodename: "NPC_EU_WITCH",
		OfferPromptSymbol: "SN_TALK_QNO_EU_EASTEU_19_01", AcceptResponseSymbol: "SN_TALK_QNO_EU_EASTEU_19_02",
		DenyResponseSymbol: "SN_TALK_QNO_EU_EASTEU_19_03", NotAchievedSymbol: "SN_TALK_QNO_EU_EASTEU_19_04",
		CompletePromptSymbol: "SN_TALK_QNO_EU_EASTEU_19_05", InventoryFullSymbol: "SN_TALK_QNO_EU_EASTEU_19_14",
		AchievedNowSymbol: "SN_TALK_QNO_EU_EASTEU_19_06",
		RewardExp:         11400, RewardSkillExp: 2500,
		MonsterDrop: &MonsterDropRule{MonsterCodenames: []string{cerberusLadon}, ChancePercent: 50},
	},
	{
		// Rahid 5 (CQNO_RM_OLDWOMAN_5, 8A00D0): giant Guardian Rockies
		// (grade 4, mission +0x10D/+0x111) drop five Essences of Roc Mountain
		// (+0x245) at 100%; each Essence gathers a Pile of Rainbow Grass on
		// the next of seven peaks (8A0AA0), and holding seven Piles is the
		// objective (CMissionChangeItem 91D540).
		// Shiphr's talk (8A03F0) pages through _01.._08 before the offer _09. v1.150's
		// popup pays 2,200,000 EXP and 320,000 skill EXP. Words
		// 0x130..0x134 and 0x139 name the dialogue; the class has no
		// bag-full line.
		Codename: "QNO_RM_OLDWOMAN_5", RequiredQuests: []string{"QNO_RM_OLDWOMAN_4"},
		KindByte: 1, Objective: ObjectiveCollect, CollectItemCodename: rahidPile, CollectCount: rahidPileTarget,
		StartNpcCodename: "NPC_RM_SLAVE2", EndNpcCodename: "NPC_RM_SLAVE2",
		OfferPages: []OfferPage{
			{PromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_5_01", ReplySymbol: "SN_TALK_QNO_RM_OLDWOMAN_5_02"},
			{PromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_5_03", ReplySymbol: "SN_TALK_QNO_RM_OLDWOMAN_5_04"},
			{PromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_5_05", ReplySymbol: "SN_TALK_QNO_RM_OLDWOMAN_5_06"},
			{PromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_5_07", ReplySymbol: "SN_TALK_QNO_RM_OLDWOMAN_5_08"},
		},
		OfferPromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_5_09", AcceptResponseSymbol: "SN_TALK_QNO_RM_OLDWOMAN_5_10",
		DenyResponseSymbol: "SN_TALK_QNO_RM_OLDWOMAN_5_11", NotAchievedSymbol: "SN_TALK_QNO_RM_OLDWOMAN_5_12",
		CompletePromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_5_13", AchievedNowSymbol: "SN_TALK_QNO_RM_OLDWOMAN_5_15",
		RewardExp: 2200000, RewardSkillExp: 320000,
		MonsterDrop: &MonsterDropRule{ItemCodename: rahidEssence, MonsterCodenames: []string{"MOB_RM_ROCKY"},
			MonsterGrades: []uint8{4}, ChancePercent: 100, MaxHeld: 1000, DropCount: 5},
	},
	{
		// Hidden Treasure 5 (CQNO_CA_TREASURE_5, 8C7260): Tricia's Seal Keys
		// call a Treasure Guardian (8C7910) whose King's Treasure Box (+0x241,
		// 100%) is the objective. Holding it, her talk pages through the
		// box's opening (_09.._15) before the thanks _16 pays. Words
		// 0x130..0x134/0x136/0x139 name the dialogue; 0x136 (_05, "You used
		// all Seal Keys") is the class's own bag-full word. v1.150's popup
		// pays 4,100 EXP; the reward row gives the fairy's present.
		Codename: "QNO_CA_TREASURE_5", RequiredQuests: []string{"QNO_CA_TREASURE_4"},
		KindByte: 1, Objective: ObjectiveCollect, CollectItemCodename: treasureBox, CollectCount: 1,
		StartNpcCodename: "NPC_CA_SMITH", EndNpcCodename: "NPC_CA_SMITH",
		OfferPromptSymbol: "SN_TALK_QNO_CA_TREASURE_5_01", AcceptResponseSymbol: "SN_TALK_QNO_CA_TREASURE_5_02",
		DenyResponseSymbol: "SN_TALK_QNO_CA_TREASURE_5_03", NotAchievedSymbol: "SN_TALK_QNO_CA_TREASURE_5_04",
		TalkPages: []OfferPage{
			{PromptSymbol: "SN_TALK_QNO_CA_TREASURE_5_09", ReplySymbol: "SN_TALK_QNO_CA_TREASURE_5_10"},
			{PromptSymbol: "SN_TALK_QNO_CA_TREASURE_5_11", ReplySymbol: "SN_TALK_QNO_CA_TREASURE_5_12"},
			{PromptSymbol: "SN_TALK_QNO_CA_TREASURE_5_13", ReplySymbol: "SN_TALK_QNO_CA_TREASURE_5_15"},
		},
		CompletePromptSymbol: "SN_TALK_QNO_CA_TREASURE_5_16", InventoryFullSymbol: "SN_TALK_QNO_CA_TREASURE_5_05",
		AchievedNowSymbol: "SN_TALK_QNO_CA_TREASURE_5_14",
		RewardExp:         4100,
		RewardItems:       []RewardItemLead{{ItemCodename: "ITEM_QNO_CA_TREASURE_5_03", Count: 1}},
		MonsterDrop:       &MonsterDropRule{MonsterCodenames: []string{treasureGuardian}, ChancePercent: 100},
	},
	{
		// 8C0F00 supplies the capture contract; v1.150 grants one courage scroll.
		Codename: "QNO_CA_GORIA_6", RequiredQuests: []string{"QNO_CA_GORIA_5"},
		KindByte: 1, Objective: ObjectiveCollect, CollectItemCodename: "ITEM_QNO_CA_GORIA_6_02", CollectCount: 1,
		StartNpcCodename: "NPC_CA_HUNTER", EndNpcCodename: "NPC_CA_HUNTER", TravelBlockMask: captureTravelBlockMask,
		OfferPromptSymbol: "SN_TALK_QNO_CA_GORIA_6_01", AcceptResponseSymbol: "SN_TALK_QNO_CA_GORIA_6_02",
		DenyResponseSymbol: "SN_TALK_QNO_CA_GORIA_6_03", NotAchievedSymbol: "SN_TALK_QNO_CA_GORIA_6_04",
		CompletePromptSymbol: "SN_TALK_QNO_CA_GORIA_6_11", InventoryFullSymbol: "SN_TALK_QNO_CA_GORIA_6_10",
		RewardItems: []RewardItemLead{{ItemCodename: "ITEM_QNO_CA_GORIA_6_03", Count: 1}},
	},
}
