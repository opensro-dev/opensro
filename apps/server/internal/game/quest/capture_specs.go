/*
===========================================================================

capture_specs.go - v1.150 ordinary captured-monster quest contracts

Primary quest text owns objectives and rewards. Native handlers supply the
capture mechanism; all references remain version-local codename joins.
Promotion traps are excluded because their quests are absent from v1.150.

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
