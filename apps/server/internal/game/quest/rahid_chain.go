/*
===========================================================================

rahid_chain.go - the Rahid chain (QNO_RM_OLDWOMAN_2..7) as curated contracts

The v1.188 classes (CQNO_RM_OLDWOMAN_n) override their NPC talk, so the
generator cannot project them. Each spec names the class behaviour it ports;
Rahid 5's peaks live with the other tool quests (capture_specs.go) and
Rahid 7's plain delivery is projected by the generator.

===========================================================================
*/
package quest

var rahidChainSpecs = []QuestSpec{
	rahidTwoSpec,
	{
		// Rahid 3 (89ED10): a two-exchange dialogue with Ahmok in Hotan
		// (CMissionDialog: _05/_06, _07/_08, then _09), then the report to
		// Town Chief Bukhra (_11). Its 0x90 override is the base no-op: no
		// travel block. The popup pays 253,000 EXP.
		Codename: "QNO_RM_OLDWOMAN_3", RequiredQuests: []string{"QNO_RM_OLDWOMAN_2"},
		KindByte: 1, Objective: ObjectiveTalk,
		StartNpcCodename: "NPC_RM_VILLAGECHIEF", EndNpcCodename: "NPC_RM_VILLAGECHIEF",
		OfferPromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_3_01", AcceptResponseSymbol: "SN_TALK_QNO_RM_OLDWOMAN_3_02",
		DenyResponseSymbol: "SN_TALK_QNO_RM_OLDWOMAN_3_03", CompletePromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_3_11",
		AcceptNoticeSymbol: "SN_TALK_QNO_RM_OLDWOMAN_3_12",
		// Bukhra's _04 "Go find Ahmok" until the Hotan exchange is done.
		SideTalks: []SideTalk{{NpcCodename: "NPC_RM_VILLAGECHIEF", PromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_3_04"}},
		Stages: []QuestStage{
			{ContentsSymbol: "SN_CON_QNO_RM_OLDWOMAN_3_01", QuestSpec: QuestSpec{
				Objective: ObjectiveTalk, EndNpcCodename: "NPC_KT_MINISTER",
				TalkPages: []OfferPage{
					{PromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_3_05", ReplySymbol: "SN_TALK_QNO_RM_OLDWOMAN_3_06"},
					{PromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_3_07", ReplySymbol: "SN_TALK_QNO_RM_OLDWOMAN_3_08"},
				},
				CompletePromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_3_09",
				AchievedNowSymbol:    "SN_TALK_QNO_RM_OLDWOMAN_3_13",
			}},
			{ContentsSymbol: "SN_CON_QNO_RM_OLDWOMAN_3_01", QuestSpec: QuestSpec{
				Objective: ObjectiveTalk, EndNpcCodename: "NPC_RM_VILLAGECHIEF",
				CompletePromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_3_11",
				RewardExp:            253000,
			}},
		},
	},
	{
		// Rahid 4 (89F0D0, talk 89F470): three slave dialogue missions; only
		// Shiphr's (+0x6A) completes, with _09 (89FA50 pays at once). The
		// other two slaves speak _05 / _07 once and then answer _06 / _08
		// (89FDA0's pending bit); Bukhra repeats _04 while it runs.
		// Accepting sends the middle notice _10. Its 0x90 override is the
		// base no-op. The popup pays 111,000 EXP.
		Codename: "QNO_RM_OLDWOMAN_4", RequiredQuests: []string{"QNO_RM_OLDWOMAN_3"},
		KindByte: 1, Objective: ObjectiveTalk,
		StartNpcCodename: "NPC_RM_VILLAGECHIEF", EndNpcCodename: "NPC_RM_SLAVE2",
		OfferPromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_4_01", AcceptResponseSymbol: "SN_TALK_QNO_RM_OLDWOMAN_4_02",
		DenyResponseSymbol: "SN_TALK_QNO_RM_OLDWOMAN_4_03", CompletePromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_4_09",
		AcceptNoticeSymbol: "SN_TALK_QNO_RM_OLDWOMAN_4_10",
		SideTalks: []SideTalk{
			{NpcCodename: "NPC_RM_VILLAGECHIEF", PromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_4_04"},
			{NpcCodename: "NPC_RM_SLAVE1", PromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_4_05", RepeatSymbol: "SN_TALK_QNO_RM_OLDWOMAN_4_06"},
			{NpcCodename: "NPC_RM_SLAVE3", PromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_4_07", RepeatSymbol: "SN_TALK_QNO_RM_OLDWOMAN_4_08"},
		},
		RewardExp: 111000,
	},
}

/*
================
rahidTwoSpec

Rahid 2 (CQNO_RM_OLDWOMAN_2, talk 89E670): Bukhra's offer _01 forks. Reply
_02 collects 200 Rocky Feathers (15% from Rocky; v1.150 asks 200, the class
100) for 4,400,000 EXP, 67,000 skill EXP and three Stat Recall Scrolls;
reply _06 waits two game months, 1800 online minutes (0x708), for nothing
(the v1.150 popup's "[Option 2] Wait 2 months None"). Refusing answers _09.
The v1.188 class does not key its reward by reply: CBasicQuest_vfF0
(922D60) looks the row up by quest-user +3, which 89E670 never writes, so
that server pays its single SQL row either way. The v1.150 popup is the
data this client ships, so the waiting branch pays nothing here.
================
*/
var rahidTwoSpec = QuestSpec{
	Codename: "QNO_RM_OLDWOMAN_2", RequiredQuests: []string{"QNO_RM_OLDWOMAN_1"},
	KindByte: 1, Objective: ObjectiveCollect, CollectItemCodename: "ITEM_QNO_RM_OLDWOMAN_2_01", CollectCount: 200,
	MonsterDrop:      &MonsterDropRule{MonsterCodenames: []string{"MOB_RM_ROCKY"}, ChancePercent: 15},
	StartNpcCodename: "NPC_RM_VILLAGECHIEF", EndNpcCodename: "NPC_RM_VILLAGECHIEF",
	OfferPromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_2_01", DenyResponseSymbol: "SN_TALK_QNO_RM_OLDWOMAN_2_09",
	NotAchievedSymbol: "SN_TALK_QNO_RM_OLDWOMAN_2_04", CompletePromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_2_14",
	InventoryFullSymbol: "SN_TALK_QNO_RM_OLDWOMAN_2_05", AchievedNowSymbol: "SN_TALK_QNO_RM_OLDWOMAN_2_12",
	OfferBranches: []OfferBranch{
		{ReplySymbol: "SN_TALK_QNO_RM_OLDWOMAN_2_02", AcceptResponseSymbol: "SN_TALK_QNO_RM_OLDWOMAN_2_03"},
		{ReplySymbol: "SN_TALK_QNO_RM_OLDWOMAN_2_06", AcceptResponseSymbol: "SN_TALK_QNO_RM_OLDWOMAN_2_07",
			NotAchievedSymbol: "SN_TALK_QNO_RM_OLDWOMAN_2_08", CompletePromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_2_15",
			AchievedNowSymbol: "SN_TALK_QNO_RM_OLDWOMAN_2_13", WaitMinutes: 1800, NoReward: true},
	},
	RewardExp: 4400000, RewardSkillExp: 67000,
	RewardItems: []RewardItemLead{{ItemCodename: "ITEM_QNO_RM_OLDWOMAN_2_02", Count: 3}},
}
