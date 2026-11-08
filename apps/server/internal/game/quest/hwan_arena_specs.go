/*
===========================================================================

hwan_arena_specs.go - the four quests that run in a private arena

Port-only, not native. The trial quests (_1_1, _2_1) are "kill the commander or
survive ten minutes" and the ritual quests (_1_5, _2_5) are "collect the Soul
Fragment"; the arena programme (hwan_arena.go) opens the private instance, runs
its clock and credits the kill or the item through the ordinary objective
owners, so the specs below are plain kill and collect quests.

===========================================================================
*/
package quest

// The commanders the trials ask for (a kill of the level's commander counts).
func hwanCommander(race hwanRace, chain int) string {
	if chain == 2 {
		return "MOB_QT_" + race.tag + "_SOLDIER_02_1"
	}
	return "MOB_QT_" + race.tag + "_SOLDIER_01_1"
}

/*
================
hwanArenaSpecs
================
*/
func hwanArenaSpecs() []QuestSpec {
	var specs []QuestSpec
	for _, race := range hwanRaces {
		for _, chain := range []int{1, 2} {
			previous := ""
			if chain == 2 {
				previous = hwanQuest(race, 1, "6")
			}
			trial := hwanBase(race, chain, "1", race.general, race.general, previous)
			talk := hwanTalk(trial.Codename)
			trial.Objective = ObjectiveKill
			trial.KillMonsterCodenames, trial.KillCount = []string{hwanCommander(race, chain)}, 1
			trial.NotAchievedSymbol = talk + "04"
			trial.CompletePromptSymbol, trial.AchievedNowSymbol = talk+"07", talk+"09"
			if chain == 2 {
				// The second trial's texts run one further (the Annihilation Trap lines).
				trial.AcceptResponseSymbol, trial.DenyResponseSymbol = talk+"03", talk+"02"
				trial.CompletePromptSymbol, trial.AchievedNowSymbol = talk+"10", talk+"12"
			}
			specs = append(specs, trial)

			ritual := hwanBase(race, chain, "5", race.guide, race.guide, hwanQuest(race, chain, "4"))
			ritualTalk := hwanTalk(ritual.Codename)
			ritual.Objective = ObjectiveCollect
			fragment, orb := hwanSoulFragment, race.ritualOrb1
			if chain == 2 {
				fragment, orb = hwanUnderworldFragment, race.orb2
				ritual.RequiredQuests = []string{hwanQuest(race, 2, "4")}
			} else {
				ritual.RequiredQuests = []string{hwanQuest(race, 1, "4_1")}
			}
			ritual.CollectItemCodename, ritual.CollectCount = fragment, 1
			ritual.NotAchievedSymbol, ritual.DenyResponseSymbol = ritualTalk+"03", ritualTalk+"05"
			ritual.CompletePromptSymbol, ritual.AchievedNowSymbol, ritual.InventoryFullSymbol = ritualTalk+"06", ritualTalk+"07", ritualTalk+"08"
			if chain == 2 {
				ritual.CompletePromptSymbol, ritual.AchievedNowSymbol, ritual.InventoryFullSymbol = ritualTalk+"07", ritualTalk+"08", ritualTalk+"06"
			}
			ritual.ClearOnEnd = []string{orb}
			specs = append(specs, ritual)
		}
	}
	return specs
}
