/*
===========================================================================

ivy3_missions_test.go - IVY 3: the level-20 gate and the six missions

The class's own admission condition (level 20, not the quest row's
30) and the journal the client actually shows: six mission rows
under the one contents line, five hunts gating the pay and the
Horn Trumpet row showing without ever gating it.

===========================================================================
*/

package quest

import "testing"

/*
================
TestIvy3AdmissionLevelTwenty

The acceptance test reads the class's level condition: a hunter of
20 with Ivy 1 done is admitted, one of 19 is not, and the row's 30
never enters the comparison.
================
*/
func TestIvy3AdmissionLevelTwenty(t *testing.T) {
	w := newIvy3World(t)
	if got := admissionLevel(w.def); got != 20 {
		t.Fatalf("admission level %d, want 20", got)
	}
	for _, tc := range []struct {
		level int64
		want  bool
	}{{19, false}, {20, true}, {25, true}, {29, true}, {30, true}} {
		c := ivy3Character(w, "LevelCase", true)
		*c.Level = tc.level
		_, err := w.rt.StartQuest(c, ivy3QuestCodename)
		if tc.want && err != nil {
			t.Fatalf("level %d refused: %v", tc.level, err)
		}
		if !tc.want && err == nil {
			t.Fatalf("level %d admitted", tc.level)
		}
	}
}

/*
================
TestIvy3JournalShowsSixMissions

One contents row per mission, all under the shipped line: five
hunts and the Horn row, the Horn row reading its five at the
acceptance grant.
================
*/
func TestIvy3JournalShowsSixMissions(t *testing.T) {
	w := newIvy3World(t)
	c := ivy3Accept(t, w, "JournalReader")
	at := activeQuestIndex(c, w.def.RefID)
	if at < 0 {
		t.Fatal("the quest is not active")
	}
	contents := c.ActiveQuests[at].Contents
	if len(contents) != 6 {
		t.Fatalf("journal rows %d, want 6", len(contents))
	}
	for i, node := range contents {
		if node.Description != "SN_CON_QNO_EU_IVY_3" {
			t.Fatalf("row %d reads %q", i, node.Description)
		}
		if node.Tag != uint8(i+1) {
			t.Fatalf("row %d tag %d", i, node.Tag)
		}
	}
	if got := contents[5].ObjectiveValues; len(got) != 1 || got[0] != 5 {
		t.Fatalf("Horn row values %v, want [5]", got)
	}
}

/*
================
TestIvy3HuntsGateAndHornRowDoesNot

The pay stands on the five hunts: each uncounted hunt leaves the
quest unachieved, the last one completes it, and the Horn row -
spent down to nothing by then - never takes the achievement back.
================
*/
func TestIvy3HuntsGateAndHornRowDoesNot(t *testing.T) {
	w := newIvy3World(t)
	c := ivy3Accept(t, w, "HuntWalker")
	holdItems(t, w.rt, c, "ITEM_QNO_EU_IVY_3_04", 1)
	holdItems(t, w.rt, c, "ITEM_QNO_EU_IVY_3_03", 1)
	holdItems(t, w.rt, c, "ITEM_QNO_EU_IVY_3_02", 1)
	holdItems(t, w.rt, c, ivy3BloodCodename, 1)
	holdItems(t, w.rt, c, ivy3HairCodename, 1)
	hunts := []string{"MOB_QT_01_SOLDIER_CLON", "MOB_QT_03_PUNISHER_CLON", "MOB_QT_01_PUNISHER", ivy3BloodSource, ivy3BossCodename}
	for i, mob := range hunts {
		if at := activeQuestIndex(c, w.def.RefID); objectiveMet(c, w.def, c.ActiveQuests[at]) {
			t.Fatalf("the quest stood achieved with %d hunts counted", i)
		}
		w.rt.KillUpdater()(c, mob, 0)
	}
	at := activeQuestIndex(c, w.def.RefID)
	if !objectiveMet(c, w.def, c.ActiveQuests[at]) {
		t.Fatal("the quest did not stand achieved after the five hunts")
	}
	for captureItemCount(c, ivy3HornCodename) > 0 {
		loseItem(c, ivy3HornCodename)
	}
	w.rt.KillUpdater()(c, "MOB_AM_SOLDIER", 0)
	if at := activeQuestIndex(c, w.def.RefID); !objectiveMet(c, w.def, c.ActiveQuests[at]) {
		t.Fatal("spending the Horns took the achievement back")
	}
}
