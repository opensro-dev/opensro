/*
===========================================================================

tiger_chain.go - the Tiger Hunting Competition chain head (QNO_CH_GENARAL_1)

GENARAL_1 (questdata id 57, level 13) opens the Jangan general's tiger
competition; GENARAL_2 and GENARAL_3 already ship in the generated catalog
with GENARAL_1 as their prerequisite, so without this spec the whole chain
is unavailable to every character (garcia694 reports, 2026-10-19). Its
v1.150 script (@SN_QNO_CH_GENARAL_1.lua) survived in Quest.sct, so it was
never a compiled class and never entered either generated catalog; the
spec below authors it from that script plus the shipped v1.150 text.

Evidence: LuaSetStartCodition level 13, start/completion NPC_CH_GENARAL,
LuaSetAchievementLimit 1, three kill missions (tiger champion, tiger,
young tiger). Source precedence follows scripts/data/quest/README.md:
the script supplies structure and the completion limit (1); the v1.150
text wins wherever it names a value, so counts are the SN_CON text's
1/20/20 and the advertised reward is the SN_PAYCON text's 6700 EXP with
2050 SP EXP (the script's 15/15 counts and 12820 EXP are the newer
balance and do not ship; its "three times repeatable" prose loses to
the script's limit 1 the same way). The reward items are the script's
LuaSetAchievedItem entries, confirmed by the retail reward table:
ITEM_ETC_HP_POTION_02 x50 and x13, no gold. The ACHIEVED_NOW symbol
comes from achieved_now_generated.json, not this spec.

===========================================================================
*/
package quest

var tigerChainSpecs = []QuestSpec{
	{
		Codename: "QNO_CH_GENARAL_1", KindByte: 1, MaxCompletions: 1, Objective: ObjectiveParallel,
		Objectives: []MissionSpec{
			{ContentsSymbol: "SN_CON_QNO_CH_GENARAL_1_01", Objective: ObjectiveKill, KillMonsterCodenames: []string{"MOB_CH_TIGER"}, KillCount: 1, KillRanks: []uint8{1}},
			{ContentsSymbol: "SN_CON_QNO_CH_GENARAL_1_02", Objective: ObjectiveKill, KillMonsterCodenames: []string{"MOB_CH_TIGER"}, KillCount: 20, KillRanks: []uint8{0}},
			{ContentsSymbol: "SN_CON_QNO_CH_GENARAL_1_03", Objective: ObjectiveKill, KillMonsterCodenames: []string{"MOB_CH_TIGER_CLON"}, KillCount: 20, KillRanks: []uint8{0}},
		},
		RewardExp: 6700, RewardSkillExp: 2050,
		RewardItems:      []RewardItemLead{{ItemCodename: "ITEM_ETC_HP_POTION_02", Count: 50}, {ItemCodename: "ITEM_ETC_HP_POTION_02", Count: 13}},
		StartNpcCodename: "NPC_CH_GENARAL", EndNpcCodename: "NPC_CH_GENARAL",
		OfferPromptSymbol: "SN_TALK_QNO_CH_GENARAL_1_01", CompletePromptSymbol: "SN_TALK_QNO_CH_GENARAL_1_06",
		InventoryFullSymbol:  "SN_TALK_QNO_CH_GENARAL_1_05",
		AcceptResponseSymbol: "SN_TALK_QNO_CH_GENARAL_1_02", DenyResponseSymbol: "SN_TALK_QNO_CH_GENARAL_1_03", NotAchievedSymbol: "SN_TALK_QNO_CH_GENARAL_1_04",
	},
}
