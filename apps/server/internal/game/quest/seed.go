package quest

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
)

// The character-creation quest seed: the active quest(s) retail grants a
// fresh character.
//
// MECHANISM (v1.188 Eternity shard, _RefCharDefault_Quest - recovered
// cluster fixedEnd=11/ncols=5: [u8
// service][u32 shard quest id][u8 race][u8 level] + quest codename):
// retail seeds fresh characters' quest state at creation from a
// race-keyed default table, exactly parallel to _RefCharDefault_Skill
// (the defaultskills.go seed). THE LIST IS CODENAMES (the load-bearing
// cross-version rule); exactly two shard rows survive the codename join
// into the shipped v1.150 questdata:
//
//	QTUTORIAL_CH  race 0 (China), level 1  -> SEEDED for Chinese
//	              characters (grade B mechanism, grade A id resolution).
//	QEVENT_GUIDE  race 3 (both), level 1   -> DELIBERATELY NOT SEEDED
//	              (declared choice): its shipped questdata row is the
//	              "EventGuide(서버전용)" server-only marker (title symbol
//	              is the literal "xxx" miss), and its client-visible
//	              machinery is the EVENT-GUIDE plane this server already
//	              persists (Character.Mission.EventGuideStateMask, the
//	              0x707B ack lane) - seeding it as a pane quest would
//	              put a titleless row in the quest window that retail
//	              players never saw.
//
// The shard's other 12 rows are its custom QEV_ALL_BASIC_* event chain
// (level-keyed grants) whose codenames do not exist in v1.150 - the
// codename join self-filters them, the extract_shard_bak.py precedent.
//
// Europe seeds NOTHING (grade B negative: no race-1 row survives the
// join). v1.150 does ship QTUTORIAL_EU (id 210), but as an offer: Guide
// Lipria asks "Would you like my help?" (SN_TALK_QTUTORIAL_EU_01) with
// accept/deny branches, so it is an NPC-offered quest
// (european_tutorial.go), not a creation seed.
//
// NO MIGRATION BACKFILL (declared choice, unlike the skills seed's
// v5->v6): ActiveQuests already persists with omitempty and absent =
// none active is a fully valid state the enter-world emission handles.
// Pre-release, pre-existing dev characters simply keep their empty
// lists; retro-granting a tutorial onto a played character has no
// retail analog (retail characters were BORN with it).

// chDefaultQuestCodenames is the Chinese creation seed.
var chDefaultQuestCodenames = []string{
	"QTUTORIAL_CH",
}

// euDefaultQuestCodenames is the European creation seed (empty - see
// the grade-B negative above).
var euDefaultQuestCodenames = []string{}

// DefaultQuestCodenames answers the racial creation-seed codenames
// (defensive copy; race resolution mirrors DefaultSkillCodenames).
func DefaultQuestCodenames(raceKey string) []string {
	codenames := euDefaultQuestCodenames
	if raceKey == enterworld.RaceKeyChina {
		codenames = chDefaultQuestCodenames
	}
	out := make([]string, len(codenames))
	copy(out, codenames)
	return out
}

// DefaultQuestSeeder adapts the loaded definitions into the store's
// creation hook (store.Options.DefaultQuests): given a race, it answers
// the fresh-character active-quest records in seed order. FAIL LOUD
// contract: a seed codename that does not resolve in the loaded
// definitions errors with the codename named - the caller must refuse
// the creation rather than persist a short seed (the DefaultSkillSeeder
// posture). A definition-less set (textdata absent) with a NON-EMPTY
// racial seed list refuses the same way; an empty racial list (Europe)
// seeds nothing everywhere.
func DefaultQuestSeeder(defs *Definitions) func(raceKey string) ([]enterworld.ActiveQuestRecord, error) {
	return func(raceKey string) ([]enterworld.ActiveQuestRecord, error) {
		codenames := DefaultQuestCodenames(raceKey)
		records := make([]enterworld.ActiveQuestRecord, 0, len(codenames))
		for _, codename := range codenames {
			def, ok := defs.ByCodename(codename)
			if !ok {
				return nil, fmt.Errorf("default quest seed: codename %s does not resolve in the loaded quest definitions - refusing to seed a short list", codename)
			}
			records = append(records, BuildActiveQuestRecord(def, 0))
		}
		return records, nil
	}
}
