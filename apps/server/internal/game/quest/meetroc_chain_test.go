/*
===========================================================================

meetroc_chain_test.go - the Roc chain's loop, kill exchange and tokens

The compiled MEETROC specs load through a copy of the shipped catalog that
carries the six questdata rows the Roc import stages (scripts/roc_import.py,
source ids 91, 92, 96 and 374-376), over the shipped itemdata. Every rule
runs through its production owner: StartQuest, the kill updater, the NPC
turn-in and the client's give-up request.

===========================================================================
*/

package quest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

const (
	meetroc1        = "QNO_RM_MEETROC_1"
	meetroc2        = "QNO_RM_MEETROC_2"
	meetroc6        = "QNO_RM_MEETROC_6"
	meetrocChief    = "NPC_RM_VILLAGECHIEF"
	meetrocFragment = "ITEM_QNO_RM_MEETROC_6_02"
	meetrocCrystal  = "ITEM_QNO_RM_MEETROC_6_03"
	meetrocToken1   = "ITEM_QNO_RM_MEETROC_ECT_01"
)

// meetrocQuestRows are the staged questdata rows, in the shipped layout.
var meetrocQuestRows = []string{
	"1\t91\tQNO_RM_MEETROC_1\t80\tname\tSN_QNO_RM_MEETROC_1\txxx\txxx\txxx\txxx\txxx",
	"1\t92\tQNO_RM_MEETROC_2\t80\tname\tSN_QNO_RM_MEETROC_2\txxx\txxx\txxx\txxx\txxx",
	"1\t96\tQNO_RM_MEETROC_6\t80\tname\tSN_QNO_RM_MEETROC_6\txxx\txxx\txxx\txxx\txxx",
	"1\t374\tQNO_RM_MEETROC_ECT_1\t80\tname\tSN_QNO_RM_MEETROC_ECT_1\txxx\txxx\txxx\txxx\txxx",
	"1\t375\tQNO_RM_MEETROC_ECT_2\t80\tname\tSN_QNO_RM_MEETROC_ECT_2\txxx\txxx\txxx\txxx\txxx",
	"1\t376\tQNO_RM_MEETROC_ECT_3\t80\tname\tSN_QNO_RM_MEETROC_ECT_3\txxx\txxx\txxx\txxx\txxx",
}

// meetrocContentsRows are the client's ECT rows the import stages, cut to
// v1.150's thirteen columns; v1.150 ships the other three quests' rows.
var meetrocContentsRows = []string{
	"QNO_RM_MEETROC_ECT_1\tname\t3\txxx\t1\tSN_CON_QNO_RM_MEETROC_ECT_1\txxx\txxx\txxx\txxx\txxx\txxx\txxx",
	"QNO_RM_MEETROC_ECT_2\tname\t3\txxx\t1\tSN_CON_QNO_RM_MEETROC_ECT_2\txxx\txxx\txxx\txxx\txxx\txxx\txxx",
	"QNO_RM_MEETROC_ECT_3\tname\t3\txxx\t1\tSN_CON_QNO_RM_MEETROC_ECT_3\txxx\txxx\txxx\txxx\txxx\txxx\txxx",
}

// meetrocGuardians are QNO_RM_MEETROC_6's seven kill targets.
var meetrocGuardians = []string{
	"MOB_QT_01_ROCKY_CLON2", "MOB_QT_02_ROCKY_CLON2", "MOB_QT_03_ROCKY_CLON2", "MOB_QT_04_ROCKY_CLON2",
	"MOB_QT_05_ROCKY_CLON2", "MOB_QT_06_ROCKY_CLON2", "MOB_QT_07_ROCKY_CLON2",
}

/*
================
meetrocRuntime

The shipped quest tables plus the staged rows, over the shipped itemdata.
================
*/
func meetrocRuntime(t *testing.T) *Runtime {
	t.Helper()
	clientdata.RequireGameData(t)
	textdata := gamedatatest.TextdataDir(t)
	dir := t.TempDir()
	staged := map[string][]string{"questdata.txt": meetrocQuestRows, "questcontentsdata.txt": meetrocContentsRows, "textquest.txt": nil}
	for name, rows := range staged {
		raw, err := os.ReadFile(filepath.Join(textdata, name))
		if err != nil {
			t.Fatal(err)
		}
		// The shipped tables are UTF-16LE with CRLF; the staged rows
		// follow them in the same encoding, as the Roc import writes them.
		if len(rows) != 0 {
			for _, r := range "\r\n" + strings.Join(rows, "\r\n") + "\r\n" {
				raw = append(raw, byte(r), byte(r>>8))
			}
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	items := enterworld.NewTextdataItems(textdata)
	defs, err := LoadDefinitions(NewCatalog(dir), items)
	if err != nil {
		t.Fatal(err)
	}
	deps := &enterworld.Deps{Items: items, Levels: enterworld.NewTextdataLevels(textdata)}
	rt, err := NewRuntime(deps, defs, func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) { return nil, true })
	if err != nil {
		t.Fatal(err)
	}
	rt.PlanInventory = action.NewRuntime(deps, nil).PlanQuestInventory
	return rt
}

/*
================
meetrocCharacter

A level-90 character whose history holds the named quests.
================
*/
func meetrocCharacter(t *testing.T, rt *Runtime, done ...string) *enterworld.Character {
	t.Helper()
	c := questCharacter()
	*c.Level = 90
	for _, code := range done {
		c.CompletedQuestIds = append(c.CompletedQuestIds, meetrocDef(t, rt, code).RefID)
	}
	return c
}

func meetrocDef(t *testing.T, rt *Runtime, code string) *Definition {
	t.Helper()
	def, ok := rt.Defs.ByCodename(code)
	if !ok {
		t.Fatalf("%s is not loaded", code)
	}
	return def
}

/*
================
TestMeetroc2GiveUpReopensMeetroc1

Giving MEETROC_2 up (90C1C0) zeroes MEETROC_1, so the chief offers it again.
================
*/
func TestMeetroc2GiveUpReopensMeetroc1(t *testing.T) {
	rt := meetrocRuntime(t)
	c := meetrocCharacter(t, rt, meetroc1)
	if _, err := rt.StartQuest(c, meetroc2); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.HandleGiveUp(c, u32le(meetrocDef(t, rt, meetroc2).RefID)); err != nil {
		t.Fatal(err)
	}
	if questCompleted(c, meetrocDef(t, rt, meetroc1).RefID) {
		t.Fatal("MEETROC_1 still completed after MEETROC_2 was given up")
	}
	if _, err := rt.StartQuest(c, meetroc1); err != nil {
		t.Fatalf("MEETROC_1 not offered again: %v", err)
	}
}

/*
================
TestMeetroc6NeedsTheCrystal

Seven kills without a Soul Crystal never complete the quest: the achieved
key is the exchange's (90C9D0), so the chief refuses the turn-in.
================
*/
func TestMeetroc6NeedsTheCrystal(t *testing.T) {
	rt := meetrocRuntime(t)
	c := meetrocCharacter(t, rt, meetroc1, meetroc2)
	if _, err := rt.StartQuest(c, meetroc6); err != nil {
		t.Fatal(err)
	}
	for _, mob := range meetrocGuardians {
		rt.KillUpdater()(c, mob, 0)
	}
	if captureItemCount(c, meetrocFragment) != 0 {
		t.Fatal("a fragment was granted without a crystal")
	}
	if _, err := rt.AdvanceNpcQuest(c, meetroc6, meetrocChief); err == nil {
		t.Fatal("seven kills without the exchange completed MEETROC_6")
	}
}

/*
================
TestMeetroc6ExchangeTurnInAndLoop

The crystal is exchanged only on the seventh kill. Paying MEETROC_6 takes
the fragment, pays one of each token and zeroes MEETROC_2 (90D4C0), so
MEETROC_6 waits for MEETROC_2 again.
================
*/
func TestMeetroc6ExchangeTurnInAndLoop(t *testing.T) {
	rt := meetrocRuntime(t)
	c := meetrocCharacter(t, rt, meetroc1, meetroc2)
	if _, err := rt.StartQuest(c, meetroc6); err != nil {
		t.Fatal(err)
	}
	holdItems(t, rt, c, meetrocCrystal, 2)
	for n, mob := range meetrocGuardians {
		if captureItemCount(c, meetrocFragment) != 0 {
			t.Fatalf("exchanged after %d of 7 kills", n)
		}
		rt.KillUpdater()(c, mob, 0)
	}
	if captureItemCount(c, meetrocCrystal) != 0 || captureItemCount(c, meetrocFragment) != 1 {
		t.Fatalf("exchange left %d crystals and %d fragments, want 0 and 1",
			captureItemCount(c, meetrocCrystal), captureItemCount(c, meetrocFragment))
	}
	holdItems(t, rt, c, meetrocCrystal, 1)
	rt.KillUpdater()(c, meetrocGuardians[0], 0)
	if captureItemCount(c, meetrocFragment) != 1 || captureItemCount(c, meetrocCrystal) != 1 {
		t.Fatal("a held fragment did not stop a second exchange")
	}
	if _, err := rt.AdvanceNpcQuest(c, meetroc6, meetrocChief); err != nil {
		t.Fatal(err)
	}
	if captureItemCount(c, meetrocFragment) != 0 || captureItemCount(c, meetrocCrystal) != 0 {
		t.Fatal("the turn-in left a Roc soul item behind")
	}
	for _, token := range []string{meetrocToken1, "ITEM_QNO_RM_MEETROC_ECT_02", "ITEM_QNO_RM_MEETROC_ECT_03", "ITEM_QNO_RM_MEETROC_6_01"} {
		if captureItemCount(c, token) != 1 {
			t.Fatalf("reward %s = %d, want 1", token, captureItemCount(c, token))
		}
	}
	if questCompleted(c, meetrocDef(t, rt, meetroc2).RefID) {
		t.Fatal("MEETROC_2 still completed after MEETROC_6 paid")
	}
	if _, err := rt.StartQuest(c, meetroc6); err == nil {
		t.Fatal("MEETROC_6 offered again before MEETROC_2")
	}
	if _, err := rt.StartQuest(c, meetroc2); err != nil {
		t.Fatalf("MEETROC_2 not offered again: %v", err)
	}
}

/*
================
TestMeetroc6GiveUpCleansAndReopens

Giving MEETROC_6 up (90CE00) takes every fragment and crystal and zeroes
MEETROC_2.
================
*/
func TestMeetroc6GiveUpCleansAndReopens(t *testing.T) {
	rt := meetrocRuntime(t)
	c := meetrocCharacter(t, rt, meetroc1, meetroc2)
	if _, err := rt.StartQuest(c, meetroc6); err != nil {
		t.Fatal(err)
	}
	holdItems(t, rt, c, meetrocCrystal, 1)
	holdItems(t, rt, c, meetrocFragment, 1)
	if _, err := rt.HandleGiveUp(c, u32le(meetrocDef(t, rt, meetroc6).RefID)); err != nil {
		t.Fatal(err)
	}
	if captureItemCount(c, meetrocFragment) != 0 || captureItemCount(c, meetrocCrystal) != 0 {
		t.Fatal("give-up left a Roc soul item behind")
	}
	if questCompleted(c, meetrocDef(t, rt, meetroc2).RefID) {
		t.Fatal("MEETROC_2 still completed after MEETROC_6 was given up")
	}
}

/*
================
TestMeetrocTokenQuestRepeatsAndKeepsTokensOnGiveUp

QNO_RM_MEETROC_ECT_1 takes seven tokens per run and pays five rewards;
it repeats while tokens are held, and giving it up keeps them (its 0xfc
slot skips the base cleanup).
================
*/
func TestMeetrocTokenQuestRepeatsAndKeepsTokensOnGiveUp(t *testing.T) {
	rt := meetrocRuntime(t)
	def := meetrocDef(t, rt, "QNO_RM_MEETROC_ECT_1")
	c := meetrocCharacter(t, rt)
	if _, err := rt.StartQuest(c, def.Codename); err == nil {
		t.Fatal("token quest offered with no token held")
	}
	holdItems(t, rt, c, meetrocToken1, 16)
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.HandleGiveUp(c, u32le(def.RefID)); err != nil {
		t.Fatal(err)
	}
	if captureItemCount(c, meetrocToken1) != 16 {
		t.Fatalf("give-up left %d tokens, want 16", captureItemCount(c, meetrocToken1))
	}
	for run := 1; run <= 2; run++ {
		if _, err := rt.StartQuest(c, def.Codename); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if got := captureItemCount(c, meetrocToken1); got != uint32(16-7*run) {
			t.Fatalf("run %d left %d tokens, want %d", run, got, 16-7*run)
		}
		if got := captureItemCount(c, "ITEM_QNO_RM_MEETROC_ECT_1_01"); got != uint32(5*run) {
			t.Fatalf("run %d paid %d rewards, want %d", run, got, 5*run)
		}
	}
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatalf("two tokens held, offer refused: %v", err)
	}
	if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err == nil {
		t.Fatal("two tokens completed a seven-token run")
	}
}
