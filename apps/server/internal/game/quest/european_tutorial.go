package quest

/*
===========================================================================

european_tutorial.go - the European tutorial

QTUTORIAL_EU is the tutorial v1.150 presents: Guide Lipria offers it (_01,
accept _70, deny _71), the game guide's "Europe Tutorial" article tracks it,
and its stages live in europeanTutorialStages. The QNO_EU_TUTORIAL_1..6
chain is an earlier revision of the same route with diverging dialogue and
no guide article; it no longer opens for new characters, but a character
already inside it can still finish it. Quest 7 of that chain has no
localized dialogue/content in this release and is not made into a blank NPC
quest.

===========================================================================
*/

// qnoTutorialSuperseded closes new acceptance of the earlier chain.
const qnoTutorialSuperseded = "superseded by QTUTORIAL_EU, the tutorial v1.150's guide presents"

var europeanTutorialSpecs = []QuestSpec{
	{Codename: "QTUTORIAL_EU", KindByte: 1, Objective: ObjectiveTalk,
		StartNpcCodename: "NPC_EU_ADVICE", EndNpcCodename: "NPC_EU_ADVICE",
		OfferPromptSymbol: "SN_TALK_QTUTORIAL_EU_01", AcceptResponseSymbol: "SN_TALK_QTUTORIAL_EU_70",
		DenyResponseSymbol: "SN_TALK_QTUTORIAL_EU_71", CompletePromptSymbol: "SN_TALK_QTUTORIAL_EU_51",
		Stages: europeanTutorialStages()},
	{Codename: "QNO_EU_TUTORIAL_2", KindByte: 1, Objective: ObjectiveTalk,
		RequiredQuests:   []string{"QNO_EU_TUTORIAL_1"},
		StartNpcCodename: "NPC_EU_ARMOR", EndNpcCodename: "NPC_EU_ARMOR",
		OfferPromptSymbol: "SN_TALK_QNO_EU_TUTORIAL_2_01", CompletePromptSymbol: "SN_TALK_QNO_EU_TUTORIAL_2_06", InventoryFullSymbol: "SN_TALK_QNO_EU_TUTORIAL_2_07",
		RewardItems: []RewardItemLead{{ItemCodename: "ITEM_EU_M_HEAVY_01_AA_A", Count: 1}}},
	{Codename: "QNO_EU_TUTORIAL_3", KindByte: 1, Objective: ObjectiveTalk,
		RequiredQuests:   []string{"QNO_EU_TUTORIAL_2"},
		StartNpcCodename: "NPC_EU_ACCESSORY", EndNpcCodename: "NPC_EU_ACCESSORY",
		OfferPromptSymbol: "SN_TALK_QNO_EU_TUTORIAL_3_01", CompletePromptSymbol: "SN_TALK_QNO_EU_TUTORIAL_3_07", InventoryFullSymbol: "SN_TALK_QNO_EU_TUTORIAL_3_08",
		RewardGold: 200, RewardItems: []RewardItemLead{{ItemCodename: "ITEM_EU_RING_01_A", Count: 1}}},
	{Codename: "QNO_EU_TUTORIAL_4", KindByte: 1, Objective: ObjectiveTalk,
		RequiredQuests:   []string{"QNO_EU_TUTORIAL_3"},
		StartNpcCodename: "NPC_EU_POTION", EndNpcCodename: "NPC_EU_POTION",
		OfferPromptSymbol: "SN_TALK_QNO_EU_TUTORIAL_4_01", CompletePromptSymbol: "SN_TALK_QNO_EU_TUTORIAL_4_05", RewardExp: 60},
	{Codename: "QNO_EU_TUTORIAL_5", KindByte: 1, Objective: ObjectiveCollect,
		RequiredQuests:   []string{"QNO_EU_TUTORIAL_4"},
		StartNpcCodename: "NPC_EU_SMITH", EndNpcCodename: "NPC_EU_SMITH",
		CollectItemCodename: "ITEM_ETC_HP_POTION_01", CollectCount: 1,
		OfferPromptSymbol: "SN_TALK_QNO_EU_TUTORIAL_5_01", CompletePromptSymbol: "SN_TALK_QNO_EU_TUTORIAL_5_07", InventoryFullSymbol: "SN_TALK_QNO_EU_TUTORIAL_5_08",
		RewardItems: []RewardItemLead{{ItemCodename: "ITEM_QTUTORIAL_EU_01", Count: 1}}},
	{Codename: "QNO_EU_TUTORIAL_6", KindByte: 1, Objective: ObjectiveKill,
		RequiredQuests:   []string{"QNO_EU_TUTORIAL_5"},
		StartNpcCodename: "NPC_EU_SMITH", EndNpcCodename: "NPC_EU_ADVICE",
		KillCount: 20, KillMonsterCodenames: []string{"MOB_EU_MOVOI", "MOB_EU_MOVOI_CLON"},
		OfferPromptSymbol: "SN_TALK_QNO_EU_TUTORIAL_5_07", CompletePromptSymbol: "SN_TALK_QNO_EU_TUTORIAL_6_03", InventoryFullSymbol: "SN_TALK_QNO_EU_TUTORIAL_6_04",
		// INFERENCE (no surviving script): _05 is the quest's only "hunt
		// complete, report to Lipria" line, the ACHIEVED_NOW role.
		AchievedNowSymbol: "SN_TALK_QNO_EU_TUTORIAL_6_05",
		RewardExp:         350, RewardItems: []RewardItemLead{{ItemCodename: "ITEM_QTUTORIAL_EU_2_01", Count: 1}}},
}
