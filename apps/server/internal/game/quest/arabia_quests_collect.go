/*
===========================================================================

arabia_quests_collect.go - the Baghdad collects and deliveries

Port-only, not native; the companion table to arabia_quests.go, from
the same later-client evidence plus the quest items the items import
(scripts/arabia_quest_items.py, #4134) brings into the build:

  - the collects are one SN_CON line each ("Collect N <goods>(%d)"),
    one ITEM_QNO_AR_* item each and the quest.txt target kinds the
    goods come from (in the Baghdad dungeon the kinds stand in world
    112, everywhere else in the default world),
  - the deliveries are one "Deliver <goods>" line each; the start NPC
    hands the goods over at acceptance (their SN_TALK accept line
    says so) and the end NPC takes them. Their talk set runs one line
    longer than a hunt's: _04 is the full-bag line, _05 the reminder,
    _06 the end NPC's greeting and _07 the banner,
  - a delivery follows the collect that gathered its goods (the
    questcontentsdata next links, inverted into RequiredQuests),
  - the Karem essence collects (QNO_AR_061 to 069) are the unlimited
    ones (the SN_PAYCON format line reads "unlimited repetitions"),
    so they carry Repeatable with no completion limit; the three
    "Find" collects at the top of the level band allow 5.

No drop table of either source names an Arabia row (the iSRO-R
_RefMonster_AssignedItemDrop, _RefMonster_AssignedItemRndDrop,
_RefDropItemAssign and _RefDropItemGroup reads are all empty for
Arabia kinds), so the drop chance has no native number to copy. It is
recorded here as an inference: 100%, the chance every sibling hunt's
proof-goods effectively drop at, at collect counts (40 to 500) in the
same band as the hunt counts beside them (390 to 650).

Held back with the talk/hunt gaps of arabia_quests.go: QNO_AR_012,
QNO_AR_014 and QNO_AR_018, whose missing talk line (_01, _05 and _06)
is a Korean-only stub row in the later text - present in the archive,
but with no English cell, so the quest-table import leaves it out and
the line never ships - and QNO_AR_017, which stands behind
QNO_AR_012. QNO_AR_066 and QNO_AR_068 stay out too: their target
kinds (MOB_AR_TELBASTA2_ARMOR, MOB_AR_MAD_GENERAL_CLON) are placed by
no nest and shipped in no character row. So does QNO_AR_091: its NPC,
NPC_AR_SPECIAL_SOLDIER_11 (v1.696 characterdata 40523), has no npcpos
placement in the later client, so no one could hand it in. The NPC
names follow the character table (arabia_quests.go: the later contents
table misspells the farmers and Yamain).

KindByte is 1, as the whole Arabia table: these quests complete at
their NPC, not through the reward window.

===========================================================================
*/
package quest

/*
================
arabiaCollect

One collect quest at one NPC: the goods drop from the listed kinds.
completions is the SN_PAYCON limit; the unlimited essence collects go
through arabiaEssence instead.
================
*/
func arabiaCollect(code string, completions uint32, npc string, reward arabiaReward, item string, count uint32, monsters ...string) QuestSpec {
	talk := "SN_TALK_" + code + "_"
	return QuestSpec{
		Codename: code, KindByte: 1, MaxCompletions: completions, Objective: ObjectiveCollect,
		CollectItemCodename: item, CollectCount: count,
		MonsterDrop:      &MonsterDropRule{ItemCodename: item, MonsterCodenames: monsters, ChancePercent: 100},
		StartNpcCodename: npc, EndNpcCodename: npc,
		OfferPromptSymbol: talk + "01", AcceptResponseSymbol: talk + "02", DenyResponseSymbol: talk + "03",
		NotAchievedSymbol: talk + "04", CompletePromptSymbol: talk + "05", AchievedNowSymbol: talk + "06",
		RewardExp: reward.exp, RewardGold: reward.gold, RewardSkillExp: reward.skill,
	}
}

/*
================
arabiaEssence

One of Karem's unlimited collects in the Baghdad dungeon.
================
*/
func arabiaEssence(code string, reward arabiaReward, item string, count uint32, monsters ...string) QuestSpec {
	spec := arabiaCollect(code, 0, "NPC_AR_KAREM", reward, item, count, monsters...)
	spec.Repeatable = true
	return spec
}

/*
================
arabiaDeliver

One delivery: the start NPC grants the goods at acceptance, the end
NPC surrenders them. required is the collect that gathered the goods
before (empty for the two farm deliveries that open their lines).
================
*/
func arabiaDeliver(code string, start, end string, reward arabiaReward, item string, required ...string) QuestSpec {
	talk := "SN_TALK_" + code + "_"
	return QuestSpec{
		Codename: code, KindByte: 1, MaxCompletions: 1, Objective: ObjectiveDelivery,
		RequiredQuests:   required,
		DeliveryItems:    []RewardItemLead{{ItemCodename: item, Count: 1}},
		StartNpcCodename: start, EndNpcCodename: end,
		OfferPromptSymbol: talk + "01", AcceptResponseSymbol: talk + "02", DenyResponseSymbol: talk + "03",
		InventoryFullSymbol: talk + "04", NotAchievedSymbol: talk + "05",
		CompletePromptSymbol: talk + "06", AchievedNowSymbol: talk + "07",
		RewardExp: reward.exp, RewardGold: reward.gold, RewardSkillExp: reward.skill,
	}
}

/*
================
arabiaCollectSpecs

The 38 quests, in codename order: 25 collects (the last seven of
them Karem's unlimited essence collects) and 13 deliveries.
================
*/
func arabiaCollectSpecs() []QuestSpec {
	return []QuestSpec{
		arabiaDeliver("QNO_AR_006", "NPC_AR_SOLDIER_2", "NPC_AR_FARMER3", arabiaReward{47050, 41456232, 806}, "ITEM_QNO_AR_006_01"),
		arabiaDeliver("QNO_AR_007", "NPC_AR_KHALED", "NPC_AR_FARMER1", arabiaReward{47050, 41456232, 806}, "ITEM_QNO_AR_007_01"),
		arabiaCollect("QNO_AR_011", 2, "NPC_AR_FARMER1", arabiaReward{185848, 82912465, 1611}, "ITEM_QNO_AR_011_01", 180, "MOB_AR_BARUSWOLF_ADONIS1_CLONE"),
		arabiaCollect("QNO_AR_013", 2, "NPC_AR_FARMER3", arabiaReward{185848, 82912465, 1611}, "ITEM_QNO_AR_013_01", 90, "MOB_AR_UNEG"),
		arabiaCollect("QNO_AR_015", 2, "NPC_AR_FARMER2", arabiaReward{185848, 82912465, 1611}, "ITEM_QNO_AR_015_01", 40, "MOB_AR_MANGYANG_PLUNDER2_CLONE"),
		arabiaDeliver("QNO_AR_016", "NPC_AR_FARMER1", "NPC_AR_SPECIAL", arabiaReward{47050, 41456232, 806}, "ITEM_QNO_AR_016_01", "QNO_AR_011"),
		arabiaCollect("QNO_AR_022", 2, "NPC_AR_SPECIAL_SOLDIER_2", arabiaReward{232260, 135250958, 2887}, "ITEM_QNO_AR_022_01", 240, "MOB_AR_BLOOD_HYENA_FASTWOLF"),
		arabiaCollect("QNO_AR_023", 2, "NPC_AR_SPECIAL_SOLDIER_2", arabiaReward{232260, 135250958, 2887}, "ITEM_QNO_AR_023_01", 360, "MOB_AR_WENEG"),
		arabiaDeliver("QNO_AR_024", "NPC_AR_SPECIAL_SOLDIER_3", "NPC_AR_SOLDIER_1", arabiaReward{47400, 52597595, 1123}, "ITEM_QNO_AR_024_01", "QNO_AR_022"),
		arabiaDeliver("QNO_AR_025", "NPC_AR_SPECIAL_SOLDIER_3", "NPC_AR_MALAK", arabiaReward{47400, 52597595, 1123}, "ITEM_QNO_AR_025_01", "QNO_AR_023"),
		arabiaCollect("QNO_AR_032", 1, "NPC_AR_SPECIAL_SOLDIER_5", arabiaReward{232260, 135250958, 2887}, "ITEM_QNO_AR_032_01", 240, "MOB_AR_MUJIGI_KOOLD"),
		arabiaCollect("QNO_AR_033", 1, "NPC_AR_SPECIAL_SOLDIER_5", arabiaReward{239000, 110472983, 2575}, "ITEM_QNO_AR_033_01", 250, "MOB_AR_BASTET_MAD"),
		arabiaCollect("QNO_AR_034", 1, "NPC_AR_SPECIAL_SOLDIER_5", arabiaReward{478000, 220945965, 5151}, "ITEM_QNO_AR_034_01", 500, "MOB_AR_BLACKROBBERARCHER_MAD", "MOB_AR_BLACKROBBER_MAD"),
		arabiaCollect("QNO_AR_035", 1, "NPC_AR_SPECIAL_SOLDIER_5", arabiaReward{239000, 110472983, 2575}, "ITEM_QNO_AR_035_01", 250, "MOB_AR_BLOOD_HYENA_WILDWOLF"),
		arabiaCollect("QNO_AR_036", 1, "NPC_AR_SPECIAL_SOLDIER_5", arabiaReward{239000, 110472983, 2575}, "ITEM_QNO_AR_036_01", 120, "MOB_AR_DARKKHEPRI_EVIL"),
		arabiaDeliver("QNO_AR_037", "NPC_AR_SPECIAL_SOLDIER_6", "NPC_AR_FARMER2", arabiaReward{47800, 52597595, 1169}, "ITEM_QNO_AR_037_01", "QNO_AR_033"),
		arabiaDeliver("QNO_AR_038", "NPC_AR_SPECIAL_SOLDIER_6", "NPC_AR_MAHMUD", arabiaReward{47800, 71602859, 1169}, "ITEM_QNO_AR_038_01", "QNO_AR_034"),
		arabiaDeliver("QNO_AR_039", "NPC_AR_SPECIAL_SOLDIER_6", "NPC_AR_YAMAIN", arabiaReward{47800, 71602859, 1169}, "ITEM_QNO_AR_039_01", "QNO_AR_035"),
		arabiaCollect("QNO_AR_044", 1, "NPC_AR_SPECIAL_SOLDIER_7", arabiaReward{478000, 220945965, 5151}, "ITEM_QNO_AR_044_01", 500, "MOB_AR_BASTET_MAD_THIEF", "MOB_AR_BLACKROBBERARCHER_THIEF"),
		arabiaCollect("QNO_AR_046", 2, "NPC_AR_SPECIAL_SOLDIER_8", arabiaReward{207045, 223583508, 5653}, "ITEM_QNO_AR_046_01", 200, "MOB_AR_BLACKROBBER_THIEF"),
		arabiaCollect("QNO_AR_048", 2, "NPC_AR_SPECIAL_SOLDIER_8", arabiaReward{207045, 223583508, 5653}, "ITEM_QNO_AR_048_01", 200, "MOB_AR_BLOOD_HYENA_FASTWOLF_CLONE"),
		arabiaDeliver("QNO_AR_050", "NPC_AR_SPECIAL_INVESTIGATION_1", "NPC_AR_MAHMUD", arabiaReward{48150, 110411609, 2792}, "ITEM_QNO_AR_050_01", "QNO_AR_046"),
		arabiaDeliver("QNO_AR_051", "NPC_AR_SPECIAL_INVESTIGATION_1", "NPC_AR_SPECIAL_SOLDIER_9", arabiaReward{48150, 110411609, 2792}, "ITEM_QNO_AR_051_01", "QNO_AR_048"),
		arabiaCollect("QNO_AR_055", 1, "NPC_AR_SPECIAL_SOLDIER_10", arabiaReward{281300, 234173558, 6404}, "ITEM_QNO_AR_055_01", 290, "MOB_AR_SNAKE_CLONE1"),
		arabiaCollect("QNO_AR_056", 1, "NPC_AR_SPECIAL_SOLDIER_10", arabiaReward{281300, 234173558, 6404}, "ITEM_QNO_AR_056_01", 150, "MOB_AR_KARMOS_CACTUS"),
		arabiaDeliver("QNO_AR_057", "NPC_AR_SPECIAL_SOLDIER_10", "NPC_AR_MERCHANT", arabiaReward{48500, 113834369, 3113}, "ITEM_QNO_AR_057_01", "QNO_AR_055"),
		arabiaDeliver("QNO_AR_058", "NPC_AR_SPECIAL_SOLDIER_10", "NPC_AR_POTION", arabiaReward{48500, 113834369, 3113}, "ITEM_QNO_AR_058_01", "QNO_AR_056"),
		arabiaEssence("QNO_AR_061", arabiaReward{3395, 66246965, 1675}, "ITEM_QNO_AR_061_01", 70, "MOB_AR_BARUSWOLF_ADONIS1", "MOB_AR_BARUSWOLF_ADONIS2"),
		arabiaEssence("QNO_AR_062", arabiaReward{3395, 66246965, 1675}, "ITEM_QNO_AR_062_01", 70, "MOB_AR_MANGYANG_PLUNDER1", "MOB_AR_MANGYANG_PLUNDER2"),
		arabiaEssence("QNO_AR_063", arabiaReward{18915, 171846862, 4006}, "ITEM_QNO_AR_063_01", 390, "MOB_AR_BLACKROBBERARCHER_ARMOR"),
		arabiaEssence("QNO_AR_064", arabiaReward{3395, 33123483, 872}, "ITEM_QNO_AR_064_01", 70, "MOB_AR_BAST_2_ARMOR"),
		arabiaEssence("QNO_AR_065", arabiaReward{3395, 33123483, 872}, "ITEM_QNO_AR_065_01", 70, "MOB_AR_SNAKEDEMON_WARAN"),
		arabiaEssence("QNO_AR_067", arabiaReward{4850, 45533747, 1245}, "ITEM_QNO_AR_067_01", 100, "MOB_AR_BLACKROBBER_ARMOR"),
		arabiaEssence("QNO_AR_069", arabiaReward{24250, 45533747, 1245}, "ITEM_QNO_AR_069_01", 1, "MOB_ARABIA_MAD_GENERAL"),
		arabiaCollect("QNO_AR_095", 5, "NPC_AR_SPECIAL_SOLDIER_13", arabiaReward{147750, 159063428, 5160}, "ITEM_QNO_AR_095", 300, "MOB_AR_KARMUS_STRANGE"),
		arabiaCollect("QNO_AR_100", 5, "NPC_AR_SPECIAL_INVESTIGATION_2", arabiaReward{150000, 184806947, 6998}, "ITEM_QNO_AR_100", 300, "MOB_AR_CRAB"),
		arabiaDeliver("QNO_AR_101", "NPC_AR_SPECIAL_INVESTIGATION_2", "NPC_AR_SPECIAL_SOLDIER_15", arabiaReward{49600, 61557546, 2158}, "ITEM_QNO_AR_101", "QNO_AR_100"),
	}
}
