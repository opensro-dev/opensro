package quest

// The route, item categories, herb count and kill count are v1.150
// SN_TALK/SN_CON_QTUTORIAL_CH. Exact glove grade and equip-check timing are
// explicit reconstruction choices; see the quest work item, not native proof.
func chineseTutorialStages() []QuestStage {
	talk := func(npc, contents, prompt string) QuestStage {
		return QuestStage{ContentsSymbol: "SN_CON_QTUTORIAL_CH_" + contents,
			QuestSpec: QuestSpec{Objective: ObjectiveTalk, EndNpcCodename: npc, CompletePromptSymbol: "SN_TALK_QTUTORIAL_CH_" + prompt}}
	}
	intro := talk("NPC_CH_GENARAL", "01", "06")
	armor := talk("NPC_CH_ARMOR", "02", "10")
	armor.RewardItems = []RewardItemLead{{ItemCodename: "ITEM_CH_M_LIGHT_01_AA_A", Count: 1}}
	items := talk("NPC_CH_GENARAL", "01", "16")
	ring := talk("NPC_CH_ACCESSORY", "03", "18")
	ring.RewardItems = []RewardItemLead{{ItemCodename: "ITEM_CH_RING_01_A", Count: 1}}
	wear := talk("NPC_CH_ACCESSORY", "03", "21")
	wear.EquippedItem = "ITEM_CH_RING_01_A"
	errand := talk("NPC_CH_GENARAL", "01", "26")
	errand.RewardGold = 200
	pharmacy := talk("NPC_CH_POTION", "04", "45")
	herb := talk("NPC_CH_GENARAL", "04", "29")
	herb.Objective, herb.CollectItemCodename, herb.CollectCount = ObjectiveCollect, "ITEM_ETC_HP_POTION_01", 1
	// _46 "Have you bought that herb?" while it is not yet held.
	herb.NotAchievedSymbol = "SN_TALK_QTUTORIAL_CH_46"
	hunt := talk("NPC_CH_GENARAL", "05", "35")
	hunt.Objective, hunt.KillCount = ObjectiveKill, 30
	hunt.KillMonsterCodenames = []string{"MOB_CH_MANGNYANG"}
	// _34 "The quest is not complete." before the thirtieth kill.
	hunt.NotAchievedSymbol = "SN_TALK_QTUTORIAL_CH_34"
	// INFERENCE (no surviving script): _44 "Mangyang hunting is complete.
	// Go see General Sonhyeon." is the hunt stage's ACHIEVED_NOW line.
	hunt.AchievedNowSymbol = "SN_TALK_QTUTORIAL_CH_44"
	return []QuestStage{intro, armor, items, ring, wear, errand, pharmacy, herb, hunt}
}

/*
================
europeanTutorialStages

QTUTORIAL_EU (v1.150 id 210): the European tutorial the client's game guide
names ("Europe Tutorial Progressed Through Lipria", SN_PAYCON_QTUTORIAL_EU)
and whose seven SN_CON_QTUTORIAL_EU_01..07 contents the quest window steps
through. No server build carries its script (the v1.188 and iSRO-R servers
compile only the later QTUTORIAL2_EU chain), so the stages are reconstructed
from its own v1.150 dialogue, in the shape of the Chinese tutorial above.
NPCs, item rewards and the Movia targets are the ones the QNO_EU_TUTORIAL
reconstruction already resolved for the same route.
================
*/
func europeanTutorialStages() []QuestStage {
	talk := func(npc, contents, prompt string) QuestStage {
		return QuestStage{ContentsSymbol: "SN_CON_QTUTORIAL_EU_" + contents,
			QuestSpec: QuestSpec{Objective: ObjectiveTalk, EndNpcCodename: npc, CompletePromptSymbol: "SN_TALK_QTUTORIAL_EU_" + prompt}}
	}
	// _04: "I've taught you the basic action method ... go to Jatomo".
	movement := talk("NPC_EU_ADVICE", "01", "04")
	movement.RewardExp = 60
	// _09: Jatomo's first-visit gift, then item usage.
	items := talk("NPC_EU_ARMOR", "02", "09")
	items.InventoryFullSymbol = "SN_TALK_QTUTORIAL_EU_13"
	items.RewardItems = []RewardItemLead{{ItemCodename: "ITEM_EU_M_HEAVY_01_AA_A", Count: 1}}
	// _19: Bajel's favour and the money for the herb, then shop usage.
	favour := talk("NPC_EU_ACCESSORY", "03", "19")
	favour.InventoryFullSymbol = "SN_TALK_QTUTORIAL_EU_23"
	favour.RewardGold = 200
	favour.RewardItems = []RewardItemLead{{ItemCodename: "ITEM_EU_RING_01_A", Count: 1}}
	// _31: Retaldi explains buying the herb. INFERENCE: her "talk to me
	// again once bought" lines (_32/_33/_65) need a held-but-not-taken item
	// check the stage engine does not have; a collect stage here would
	// consume the herb Balbardo's stage needs. The conversation the
	// contents name (SN_CON_QTUTORIAL_EU_04) is the stage; the herb is
	// checked and taken at Balbardo.
	herb := talk("NPC_EU_POTION", "04", "31")
	herb.RewardExp = 60
	// _41: Balbardo takes the herb and rewards the errand; _66 "I can't
	// talk with you because I'm sick" until it is held.
	delivery := talk("NPC_EU_SMITH", "05", "41")
	delivery.Objective, delivery.CollectItemCodename, delivery.CollectCount = ObjectiveCollect, "ITEM_ETC_HP_POTION_01", 1
	delivery.NotAchievedSymbol = "SN_TALK_QTUTORIAL_EU_66"
	delivery.RewardItems = []RewardItemLead{{ItemCodename: "ITEM_QTUTORIAL_EU_01", Count: 1}}
	// _44: Lipria's welcome back before the battle lesson.
	battle := talk("NPC_EU_ADVICE", "06", "44")
	// _51 at the report; _49 is the ACHIEVED_NOW line, _50 not yet.
	hunt := talk("NPC_EU_ADVICE", "07", "51")
	hunt.Objective, hunt.KillCount = ObjectiveKill, 20
	hunt.KillMonsterCodenames = []string{"MOB_EU_MOVOI", "MOB_EU_MOVOI_CLON"}
	hunt.AchievedNowSymbol = "SN_TALK_QTUTORIAL_EU_49"
	hunt.NotAchievedSymbol = "SN_TALK_QTUTORIAL_EU_50"
	hunt.InventoryFullSymbol = "SN_TALK_QTUTORIAL_EU_54"
	hunt.RewardExp = 350
	hunt.RewardItems = []RewardItemLead{{ItemCodename: "ITEM_QTUTORIAL_EU_2_01", Count: 1}}
	return []QuestStage{movement, items, favour, herb, delivery, battle, hunt}
}
