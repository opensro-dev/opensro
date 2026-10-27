package quest

// v1.150 SN_CON/SN_PAYCON/SN_NC own counts, rewards and prerequisites.
// Newer Quest.sct supplies target/drop/NPC contracts. Its reduced counts and
// increased EXP are deliberately overridden by the shipped client text.
var collectionQuestSpecs = []QuestSpec{
	{
		// Compiled prerequisite for KT armor -> timed KT accessory. The
		// reference server's compiled ARMOR_1 class initializes an ordinary
		// collection mission; v1.150 text
		// independently confirms 20 shells and the reward below.
		Codename: "QNO_WC_ARMOR_1", KindByte: 1, Objective: ObjectiveCollect,
		CollectItemCodename: "ITEM_QNO_WC_ARMOR_1", CollectCount: 20,
		MonsterDrop: &MonsterDropRule{MonsterCodenames: []string{"MOB_OA_REDSCORPION", "MOB_OA_REDSCORPION_CLON"},
			SpeciesChancePercent: []float32{40, 20}},
		RewardExp: 75000, RewardGold: 13500,
		StartNpcCodename: "NPC_WC_ARMOR", EndNpcCodename: "NPC_WC_ARMOR",
		OfferPromptSymbol: "SN_TALK_QNO_WC_ARMOR_1_01", CompletePromptSymbol: "SN_TALK_QNO_WC_ARMOR_1_05",
		// INFERENCE (no surviving script): _06 "gained all the red shells,
		// get them to Yeolbia" is the ACHIEVED_NOW role.
		AchievedNowSymbol:    "SN_TALK_QNO_WC_ARMOR_1_06",
		AcceptResponseSymbol: "SN_TALK_QNO_WC_ARMOR_1_02", DenyResponseSymbol: "SN_TALK_QNO_WC_ARMOR_1_03", NotAchievedSymbol: "SN_TALK_QNO_WC_ARMOR_1_04",
	},
	{
		Codename: "QNO_CH_POTION_3", KindByte: 1, Objective: ObjectiveCollect,
		RequiredQuests:      []string{"QNO_CH_POTION_1"},
		CollectItemCodename: "ITEM_QNO_CH_POTION_3_01", CollectCount: 20,
		MonsterDrop: &MonsterDropRule{MonsterCodenames: []string{"MOB_CH_WATERGHOST", "MOB_CH_WATERGHOST_CLON"}, ChancePercent: 25},
		RewardExp:   6600, RewardSkillExp: 4000,
		RewardItems:      []RewardItemLead{{ItemCodename: "ITEM_ETC_MP_POTION_01", Count: 50}},
		StartNpcCodename: "NPC_CH_POTION", EndNpcCodename: "NPC_CH_POTION",
		OfferPromptSymbol: "SN_TALK_QNO_CH_POTION_3_01", CompletePromptSymbol: "SN_TALK_QNO_CH_POTION_3_06",
		InventoryFullSymbol: "SN_TALK_QNO_CH_POTION_3_05",
	},
	{
		Codename: "QNO_CH_SPECIAL_1", KindByte: 1, Objective: ObjectiveCollect,
		CollectItemCodename: "ITEM_QNO_CH_SPECIAL_1_01", CollectCount: 10,
		MonsterDrop: &MonsterDropRule{MonsterCodenames: []string{"MOB_CH_TIGER", "MOB_CH_TIGER_CLON"}, ChancePercent: 10},
		RewardExp:   15300, RewardGold: 4000, RewardSkillExp: 5000,
		StartNpcCodename: "NPC_CH_SPECIAL", EndNpcCodename: "NPC_CH_SPECIAL",
		OfferPromptSymbol: "SN_TALK_QNO_CH_SPECIAL_1_02", CompletePromptSymbol: "SN_TALK_QNO_CH_SPECIAL_1_06",
	},
	{
		// @SN_QNO_CH_SPECIAL_2.lua (Quest.sct): level 18, NPC_CH_SPECIAL,
		// depends on SPECIAL_1, achievement limit 2 (the _07 "one more
		// time" line is its repeat offer). Source precedence follows
		// scripts/data/quest/README.md: the script supplies structure,
		// item, monster, drop chance and the limit; the v1.150 text wins
		// the values it names, so the count is the text's 100 skins and
		// the advertised reward 42300 EXP, 10000 SP EXP, 10900 gold, no
		// item (the script's 70 skins and 93060 EXP are the newer
		// balance and do not ship). SPECIAL_3
		// already ships in the generated catalog gated on this quest, so
		// the chain is closed until this spec exists (garcia694 reports).
		Codename: "QNO_CH_SPECIAL_2", KindByte: 1, Objective: ObjectiveCollect,
		RequiredQuests: []string{"QNO_CH_SPECIAL_1"}, MaxCompletions: 2,
		CollectItemCodename: "ITEM_QNO_CH_SPECIAL_2_01", CollectCount: 100,
		MonsterDrop: &MonsterDropRule{MonsterCodenames: []string{"MOB_CH_WHITETIGER"}, ChancePercent: 50},
		RewardExp:   42300, RewardGold: 10900, RewardSkillExp: 10000,
		StartNpcCodename: "NPC_CH_SPECIAL", EndNpcCodename: "NPC_CH_SPECIAL",
		OfferPromptSymbol: "SN_TALK_QNO_CH_SPECIAL_2_01", CompletePromptSymbol: "SN_TALK_QNO_CH_SPECIAL_2_06",
		RepeatOfferPromptSymbol: "SN_TALK_QNO_CH_SPECIAL_2_07",
		AcceptResponseSymbol:    "SN_TALK_QNO_CH_SPECIAL_2_02", DenyResponseSymbol: "SN_TALK_QNO_CH_SPECIAL_2_03", NotAchievedSymbol: "SN_TALK_QNO_CH_SPECIAL_2_04",
	},
	{
		Codename: "QNO_CH_GENARAL_BO_2", KindByte: 1, Objective: ObjectiveCollect,
		RequiredQuests:      []string{"QNO_CH_GENARAL_BO_1"},
		CollectItemCodename: "ITEM_QNO_CH_GENARAL_BO_2_01", CollectCount: 20,
		MonsterDrop: &MonsterDropRule{MonsterCodenames: []string{"MOB_CH_BANDITARCHER", "MOB_CH_BANDITARCHER_CLON"}, ChancePercent: 10},
		RewardExp:   28200, RewardGold: 8300, RewardSkillExp: 10000,
		StartNpcCodename: "NPC_CH_GENARAL_BO", EndNpcCodename: "NPC_CH_GENARAL_BO",
		OfferPromptSymbol: "SN_TALK_QNO_CH_GENARAL_BO_2_01", CompletePromptSymbol: "SN_TALK_QNO_CH_GENARAL_BO_2_05",
	},
	{
		Codename: "QNO_CH_FERRY2_1", KindByte: 1, Objective: ObjectiveCollect,
		CollectItemCodename: "ITEM_QNO_CH_FERRY2_1_01", CollectCount: 100,
		MonsterDrop: &MonsterDropRule{MonsterCodenames: []string{"MOB_CH_CHAKJI", "MOB_CH_CHAKJI_CLON"}, ChancePercent: 20},
		RewardExp:   112000, RewardGold: 28500, RewardSkillExp: 25000,
		StartNpcCodename: "NPC_CH_FERRY2", EndNpcCodename: "NPC_CH_FERRY2",
		OfferPromptSymbol: "SN_TALK_QNO_CH_FERRY2_1_01", CompletePromptSymbol: "SN_TALK_QNO_CH_FERRY2_1_05",
	},
}
