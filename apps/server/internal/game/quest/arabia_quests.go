/*
===========================================================================

arabia_quests.go - the Baghdad quests with complete later-client evidence

Port-only, not native. The mechanics of the Arabia quests (QNO_AR_*) were
never in the v1.150 client or its quest scripts; they are authored here
from the later client's own tables, the same evidence the quest-table
import (scripts/arabia_quest_tables.py, #4134) brings into the build:

  - questdata.txt: id and level (the row also gates admission:
    generated.go admits a spec only while its row is in the build,
    so a build without the Arabia layer loads none of these),
  - questcontentsdata.txt: the SN_CON_* lines (cols 5..12), the next
    quest (col 3, inverted below into RequiredQuests), the start and
    end NPCs (cols 17/18, which the v1.150 13-column layout drops),
  - quest.txt: the hunt targets (col 12), paired with the SN_CON
    lines in order (the line names the monster family the codename
    carries: Biyatuns are the DARKKHEPRI kinds, Qarum Batuns the
    cactus kinds),
  - refqusetreward.txt: gold / EXP / SP (cols 10/11/13; the SPExp
    column is 0 for every Arabia row, and no row carries items or
    a selection, so the rewards are the three numbers only),
  - the SN_PAYCON Korean format line: N회 수행 가능 is a completion
    limit of N (무한, unlimited, marks the repeatable essence hunts,
    none of which are in this table - they wait for the items slice).

The talk lines follow the later client's fixed roles (read off the
texts): _01 offer, _02 accept, _03 deny, _04 not-achieved, _05 the end
NPC's completion line, _06 the achieved-now banner (hunts only; a talk
quest completes at the same conversation). A quest whose _01 or _05
line the later client shipped without English is authored only once
that line is recovered or translated under the quest-table import's
QUEST_ENGLISH contract (scripts/arabia_quest_tables.py): the
QNO_AR_001 to 004 chain, QNO_AR_019, QNO_AR_026 and the hunts
QNO_AR_010 and QNO_AR_027 waited for that call (owner, 2026-11-26,
#4134) and are in. The collects and deliveries (ITEM_QNO_AR_* quest items)
live in arabia_quests_collect.go. QNO_AR_059 (its hunt names a kind
no source carries) and QNO_AR_066/068 (target kinds no source places)
stay out, recorded on #4134.

The later questcontentsdata misspells four NPCs its own character table
names: NPC_AR_FARMER_1..3 for NPC_AR_FARMER1..3 and NPC_AR_YAMAI for
NPC_AR_YAMAIN (v1.696 characterdata ids 40508/40509/40517 and 40498,
each placed once by its npcpos). The specs use the character table's
names: under the misspelling no NPC carries the marker and the GameWorld
refuses the roster at boot (r204k). A defect in the source is fixed, not
copied (owner, 2026-11-24).

KindByte is 1, the authored policy of definitions.go (the tiger chain
and the Blue Berserk quests): these quests complete at their NPC, not
through the reward window.

===========================================================================
*/
package quest

// arabiaReward is one quest's pay from refqusetreward.txt (gold, EXP, SP).
type arabiaReward struct{ gold, exp, skill int64 }

// arabiaTalk authors one talk quest (the SN_CON line is a "Speak with").
func arabiaTalk(code string, completions uint32, start, end string, reward arabiaReward) QuestSpec {
	talk := "SN_TALK_" + code + "_"
	return QuestSpec{
		Codename: code, KindByte: 1, MaxCompletions: completions, Objective: ObjectiveTalk,
		StartNpcCodename: start, EndNpcCodename: end,
		OfferPromptSymbol: talk + "01", AcceptResponseSymbol: talk + "02", DenyResponseSymbol: talk + "03",
		NotAchievedSymbol: talk + "04", CompletePromptSymbol: talk + "05",
		RewardExp: reward.exp, RewardGold: reward.gold, RewardSkillExp: reward.skill,
	}
}

// arabiaHunt is one hunt line: its SN_CON symbol, the kinds it counts and
// how many (quest.txt's targets paired with the SN_CON lines in order).
type arabiaHunt struct {
	symbol   string
	monsters []string
	count    uint32
}

// arabiaKill authors one hunt quest at one NPC: one hunt line stays the
// scalar kill objective, several become parallel missions.
func arabiaKill(code string, completions uint32, npc string, reward arabiaReward, hunts ...arabiaHunt) QuestSpec {
	talk := "SN_TALK_" + code + "_"
	spec := QuestSpec{
		Codename: code, KindByte: 1, MaxCompletions: completions,
		StartNpcCodename: npc, EndNpcCodename: npc,
		OfferPromptSymbol: talk + "01", AcceptResponseSymbol: talk + "02", DenyResponseSymbol: talk + "03",
		NotAchievedSymbol: talk + "04", CompletePromptSymbol: talk + "05", AchievedNowSymbol: talk + "06",
		RewardExp: reward.exp, RewardGold: reward.gold, RewardSkillExp: reward.skill,
	}
	if len(hunts) == 1 {
		spec.Objective = ObjectiveKill
		spec.KillMonsterCodenames = hunts[0].monsters
		spec.KillCount = hunts[0].count
		return spec
	}
	spec.Objective = ObjectiveParallel
	for _, hunt := range hunts {
		spec.Objectives = append(spec.Objectives, MissionSpec{
			ContentsSymbol: hunt.symbol, Objective: ObjectiveKill,
			KillMonsterCodenames: hunt.monsters, KillCount: hunt.count,
		})
	}
	return spec
}

/*
================
arabiaSpecs

The 37 quests, in codename order: 15 talks and 22 hunts.
================
*/
func arabiaSpecs() []QuestSpec {
	return []QuestSpec{

		arabiaTalk("QNO_AR_001", 1, "NPC_AR_SHARIAL", "NPC_AR_SHEHERAZADE", arabiaReward{47050, 34546860, 671}),
		arabiaTalk("QNO_AR_002", 1, "NPC_AR_SHEHERAZADE", "NPC_AR_ABSHAD", arabiaReward{47050, 34546860, 671}),
		arabiaTalk("QNO_AR_003", 1, "NPC_AR_ABSHAD", "NPC_AR_MAHMUD", arabiaReward{47050, 34546860, 671}),
		arabiaTalk("QNO_AR_004", 1, "NPC_AR_MAHMUD", "NPC_AR_SOLDIER_1", arabiaReward{47050, 34546860, 671}),
		arabiaTalk("QNO_AR_005", 1, "NPC_AR_SOLDIER_1", "NPC_AR_FARMER1", arabiaReward{47050, 34546860, 671}),
		arabiaKill("QNO_AR_008", 2, "NPC_AR_FARMER1", arabiaReward{366990, 165824929, 3222}, arabiaHunt{symbol: "SN_CON_QNO_AR_008_1", monsters: []string{"MOB_AR_BARUSWOLF_ADONIS1_CLONE"}, count: 390}, arabiaHunt{symbol: "SN_CON_QNO_AR_008_2", monsters: []string{"MOB_AR_BARUSWOLF_ADONIS2_CLONE"}, count: 390}),
		arabiaKill("QNO_AR_009", 2, "NPC_AR_FARMER3", arabiaReward{185848, 82912465, 1611}, arabiaHunt{symbol: "SN_CON_QNO_AR_009_1", monsters: []string{"MOB_AR_UNEG"}, count: 395}),
		arabiaKill("QNO_AR_010", 2, "NPC_AR_FARMER2", arabiaReward{366990, 165824929, 3222}, arabiaHunt{symbol: "SN_CON_QNO_AR_010_1", monsters: []string{"MOB_AR_MANGYANG_PLUNDER1_CLONE"}, count: 390}, arabiaHunt{symbol: "SN_CON_QNO_AR_010_2", monsters: []string{"MOB_AR_MANGYANG_PLUNDER2_CLONE"}, count: 390}),
		arabiaTalk("QNO_AR_019", 1, "NPC_AR_FARMER1", "NPC_AR_SPECIAL_SOLDIER_1", arabiaReward{47050, 34546860, 671}),
		arabiaKill("QNO_AR_020", 2, "NPC_AR_SPECIAL_SOLDIER_1", arabiaReward{232260, 135250958, 2887}, arabiaHunt{symbol: "SN_CON_QNO_AR_020_1", monsters: []string{"MOB_AR_BLOOD_HYENA_FASTWOLF"}, count: 490}),
		arabiaKill("QNO_AR_021", 2, "NPC_AR_SPECIAL_SOLDIER_1", arabiaReward{232260, 135250958, 2887}, arabiaHunt{symbol: "SN_CON_QNO_AR_021_1", monsters: []string{"MOB_AR_WENEG"}, count: 490}),
		arabiaTalk("QNO_AR_026", 1, "NPC_AR_SPECIAL_SOLDIER_1", "NPC_AR_SPECIAL_SOLDIER_4", arabiaReward{47400, 105195190, 2246}),
		arabiaKill("QNO_AR_027", 2, "NPC_AR_SPECIAL_SOLDIER_4", arabiaReward{232260, 135250958, 2887}, arabiaHunt{symbol: "SN_CON_QNO_AR_027_1", monsters: []string{"MOB_AR_MUJIGI_KOOLD"}, count: 490}),
		arabiaKill("QNO_AR_028", 2, "NPC_AR_SPECIAL_SOLDIER_4", arabiaReward{232260, 135250958, 2887}, arabiaHunt{symbol: "SN_CON_QNO_AR_028_1", monsters: []string{"MOB_AR_BASTET_MAD"}, count: 490}),
		arabiaKill("QNO_AR_029", 1, "NPC_AR_SPECIAL_SOLDIER_4", arabiaReward{478000, 220945965, 5151}, arabiaHunt{symbol: "SN_CON_QNO_AR_029_1", monsters: []string{"MOB_AR_BLACKROBBERARCHER_MAD"}, count: 500}, arabiaHunt{symbol: "SN_CON_QNO_AR_029_2", monsters: []string{"MOB_AR_BLACKROBBER_MAD"}, count: 500}),
		arabiaKill("QNO_AR_030", 1, "NPC_AR_SPECIAL_SOLDIER_4", arabiaReward{239000, 110472983, 2575}, arabiaHunt{symbol: "SN_CON_QNO_AR_030_1", monsters: []string{"MOB_AR_BLOOD_HYENA_WILDWOLF"}, count: 500}),
		arabiaKill("QNO_AR_031", 1, "NPC_AR_SPECIAL_SOLDIER_4", arabiaReward{239000, 110472983, 2575}, arabiaHunt{symbol: "SN_CON_QNO_AR_031_1", monsters: []string{"MOB_AR_DARKKHEPRI_EVIL"}, count: 500}),
		arabiaTalk("QNO_AR_040", 1, "NPC_AR_SPECIAL_SOLDIER_1", "NPC_AR_SPECIAL_SOLDIER_7", arabiaReward{47800, 71602859, 1669}),
		arabiaTalk("QNO_AR_041", 1, "NPC_AR_SPECIAL_SOLDIER_4", "NPC_AR_KAREM", arabiaReward{47800, 71602859, 1669}),
		arabiaKill("QNO_AR_042", 1, "NPC_AR_SPECIAL_SOLDIER_7", arabiaReward{239000, 110472983, 2575}, arabiaHunt{symbol: "SN_CON_QNO_AR_042_1", monsters: []string{"MOB_AR_BLOOD_HYENA_WILDWOLF_CLONE"}, count: 500}),
		arabiaKill("QNO_AR_043", 1, "NPC_AR_SPECIAL_SOLDIER_7", arabiaReward{478000, 220945965, 5151}, arabiaHunt{symbol: "SN_CON_QNO_AR_043_1", monsters: []string{"MOB_AR_BASTET_MAD_THIEF"}, count: 500}, arabiaHunt{symbol: "SN_CON_QNO_AR_043_2", monsters: []string{"MOB_AR_BLACKROBBERARCHER_THIEF"}, count: 500}),
		arabiaTalk("QNO_AR_045", 1, "NPC_AR_SPECIAL_SOLDIER_7", "NPC_AR_SPECIAL_SOLDIER_8", arabiaReward{47800, 71602859, 1669}),
		arabiaKill("QNO_AR_047", 2, "NPC_AR_SPECIAL_SOLDIER_8", arabiaReward{207045, 223583508, 5653}, arabiaHunt{symbol: "SN_CON_QNO_AR_047_1", monsters: []string{"MOB_AR_DARKKHEPRI_EVIL_CLONE1"}, count: 430}),
		arabiaKill("QNO_AR_049", 2, "NPC_AR_SPECIAL_SOLDIER_8", arabiaReward{207045, 223583508, 5653}, arabiaHunt{symbol: "SN_CON_QNO_AR_049_1", monsters: []string{"MOB_AR_SABRA_CACTUS"}, count: 430}),
		arabiaKill("QNO_AR_052", 1, "NPC_AR_SPECIAL_SOLDIER_9", arabiaReward{562600, 468347116, 12807}, arabiaHunt{symbol: "SN_CON_QNO_AR_052_1", monsters: []string{"MOB_AR_BLACKROBBERARCHER_THIEF_CLONE"}, count: 580}, arabiaHunt{symbol: "SN_CON_QNO_AR_052_2", monsters: []string{"MOB_AR_BLACKROBBER_THIEF_CLONE"}, count: 580}),
		arabiaTalk("QNO_AR_053", 1, "NPC_AR_SPECIAL_SOLDIER_9", "NPC_AR_SPECIAL_SOLDIER_10", arabiaReward{48500, 113834369, 3113}),
		arabiaKill("QNO_AR_054", 1, "NPC_AR_SPECIAL_SOLDIER_10", arabiaReward{562600, 468347116, 12807}, arabiaHunt{symbol: "SN_CON_QNO_AR_054_1", monsters: []string{"MOB_AR_DARKKHEPRI_EVIL_CLONE2"}, count: 380}, arabiaHunt{symbol: "SN_CON_QNO_AR_054_2", monsters: []string{"MOB_AR_SNAKE_CLONE1"}, count: 380}, arabiaHunt{symbol: "SN_CON_QNO_AR_054_3", monsters: []string{"MOB_AR_KARMOS_CACTUS"}, count: 380}),
		arabiaTalk("QNO_AR_060", 1, "NPC_AR_SPECIAL_SOLDIER_10", "NPC_AR_MAHMUD", arabiaReward{48500, 113834369, 3113}),
		arabiaKill("QNO_AR_092", 1, "NPC_AR_SPECIAL_SOLDIER_12", arabiaReward{293400, 231196843, 6942}, arabiaHunt{symbol: "SN_CON_QNO_AR_092_1", monsters: []string{"MOB_AR_SABRA_STRANGE"}, count: 600}),
		arabiaTalk("QNO_AR_093", 1, "NPC_AR_SPECIAL_SOLDIER_12", "NPC_AR_SPECIAL_SOLDIER_13", arabiaReward{48900, 57799210, 1735}),
		arabiaKill("QNO_AR_094", 1, "NPC_AR_SPECIAL_SOLDIER_13", arabiaReward{295500, 159063428, 5160}, arabiaHunt{symbol: "SN_CON_QNO_AR_094_1", monsters: []string{"MOB_AR_WHITESPIDER_FALLINBLOOD_CLONE"}, count: 600}),
		arabiaTalk("QNO_AR_096", 1, "NPC_AR_SPECIAL_SOLDIER_13", "NPC_AR_SPECIAL_SOLDIER_14", arabiaReward{49250, 238595142, 7740}),
		arabiaKill("QNO_AR_097", 5, "NPC_AR_SPECIAL_SOLDIER_14", arabiaReward{300000, 184806947, 6998}, arabiaHunt{symbol: "SN_CON_QNO_AR_097_1", monsters: []string{"MOB_AR_FIRESPIRIT_CLONE"}, count: 300}, arabiaHunt{symbol: "SN_CON_QNO_AR_097_2", monsters: []string{"MOB_AR_FIRESPIRIT_DEMON_CLONE"}, count: 300}),
		arabiaTalk("QNO_AR_098", 1, "NPC_AR_SPECIAL_SOLDIER_14", "NPC_AR_SPECIAL_INVESTIGATION_2", arabiaReward{50000, 254109552, 9623}),
		arabiaKill("QNO_AR_099", 1, "NPC_AR_SPECIAL_INVESTIGATION_2", arabiaReward{325000, 184806947, 6998}, arabiaHunt{symbol: "SN_CON_QNO_AR_099_1", monsters: []string{"MOB_AR_SNAKE_CLONE2"}, count: 650}),
		arabiaKill("QNO_AR_102", 5, "NPC_AR_SPECIAL_SOLDIER_15", arabiaReward{352450, 99901355, 4087}, arabiaHunt{symbol: "SN_CON_QNO_AR_102_1", monsters: []string{"MOB_AR_CURSEFIRE_CLONE1"}, count: 350}, arabiaHunt{symbol: "SN_CON_QNO_AR_102_2", monsters: []string{"MOB_AR_CURSEBLOOD_CLONE1"}, count: 350}),
		arabiaKill("QNO_AR_103", 5, "NPC_AR_SPECIAL_SOLDIER_15", arabiaReward{352450, 99901355, 4087}, arabiaHunt{symbol: "SN_CON_QNO_AR_103_1", monsters: []string{"MOB_AR_FLAME_WITCH_CLONE"}, count: 350}, arabiaHunt{symbol: "SN_CON_QNO_AR_103_2", monsters: []string{"MOB_AR_FLAME_WITCH_SUMMONER_CLONE"}, count: 350}),
	}
}

// arabiaQuestSpecs is the registered table (generated.go admits a spec
// only while its questdata row is in the build): the talks and hunts
// above plus the collects and deliveries of arabia_quests_collect.go.
var arabiaQuestSpecs = append(arabiaSpecs(), arabiaCollectSpecs()...)
