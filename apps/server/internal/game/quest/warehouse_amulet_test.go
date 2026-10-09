/*
===========================================================================

warehouse_amulet_test.go - Irina's amulet orders (QNO_WC_WAREHOUSE_W_2/_3)

Exercise the compiled definitions: a hunted amulet becomes authentic or
flawed on a right-click (895BC0 / 8970F0) until the order's count is held,
the hunt itself never gates the pay (vf17C's one required mission), the
second run is offered with _07 and one OK row (895740), and the second
order waits for the first completed twice.

===========================================================================
*/
package quest

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
amuletFixture

A character that finished Irina's first order (QNO_WC_WAREHOUSE_W_1).
================
*/
func amuletFixture(t *testing.T) (*Runtime, *enterworld.Character, *Definition) {
	t.Helper()
	rt := expansionRuntime(t)
	def := mustQuest(t, rt, amuletFirstQuest)
	first := mustQuest(t, rt, "QNO_WC_WAREHOUSE_W_1")
	level, gold := int64(30), int64(0)
	c := &enterworld.Character{ID: 9, Name: "irina", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: &level, Gold: &gold}
	c.CompletedQuestIds = []uint32{first.RefID}
	recordCompletion(c, first.RefID)
	return rt, c, def
}

/*
================
TestAmuletTurnsAuthenticOrFlawedUntilTheOrderIsHeld

An even roll makes the amulet authentic, an odd one flawed; once the
order's hundred authentic amulets are held the next one is refused and
kept. Outside an order the amulet cannot be used.
================
*/
func TestAmuletTurnsAuthenticOrFlawedUntilTheOrderIsHeld(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def := amuletFixture(t)
	if _, admitted := rt.BeginItemUse(c, amuletHunted, adventurerHorse(), 0); admitted {
		t.Fatal("amulet used without an order")
	}
	if _, err := rt.StartQuest(c, def.Codename); err != nil {
		t.Fatalf("accept: %v", err)
	}
	draw := uint32(0)
	rt.CaptureRoll = func() (uint32, error) { return draw, nil }
	if _, admitted := rt.BeginItemUse(c, amuletHunted, adventurerHorse(), 0); !admitted || captureItemCount(c, amuletAuthentic) != 1 {
		t.Fatal("an even roll did not give an authentic amulet")
	}
	draw = 1
	if _, admitted := rt.BeginItemUse(c, amuletHunted, adventurerHorse(), 0); !admitted || captureItemCount(c, amuletFlawed) != 1 {
		t.Fatal("an odd roll did not give a flawed amulet")
	}
	holdItems(t, rt, c, amuletAuthentic, def.Objectives[0].CollectCount-1)
	if def.Objectives[0].CollectCount != 100 {
		t.Fatalf("the order asks for %d, want v1.150's 100", def.Objectives[0].CollectCount)
	}
	if _, admitted := rt.BeginItemUse(c, amuletHunted, adventurerHorse(), 0); admitted {
		t.Fatal("an amulet was used with the order's count held")
	}
}

/*
================
TestAmuletOrderRepeatsThenOpensTheSecond

The authentic amulets alone pay; the second run is offered with _07 and
its OK row, answered _08; two completions open QNO_WC_WAREHOUSE_W_3.
================
*/
func TestAmuletOrderRepeatsThenOpensTheSecond(t *testing.T) {
	licensed.RequireGameData(t)
	rt, c, def := amuletFixture(t)
	second := mustQuest(t, rt, amuletSecondQuest)
	for run := 0; run < 2; run++ {
		row, ok := npcRow(rt, c, def.Codename, def.StartNpcCodename)
		wantPrompt, wantAccept, wantRow := def.OfferPromptSymbol, def.AcceptResponseSymbol, ""
		if run == 1 {
			wantPrompt, wantAccept, wantRow = "SN_TALK_QNO_WC_WAREHOUSE_W_2_07", "SN_TALK_QNO_WC_WAREHOUSE_W_2_08", "SN_TALK_COMMON_OK"
		}
		if !ok || row.PromptSymbol != wantPrompt || row.AcceptResponseSymbol != wantAccept || row.AcceptRowSymbol != wantRow {
			t.Fatalf("run %d offer %+v", run, row)
		}
		if _, offered := npcRow(rt, c, second.Codename, second.StartNpcCodename); offered {
			t.Fatalf("the second order was offered after %d runs", run)
		}
		if _, err := rt.StartQuest(c, def.Codename); err != nil {
			t.Fatalf("run %d accept: %v", run, err)
		}
		holdItems(t, rt, c, amuletAuthentic, 100)
		if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil {
			t.Fatalf("run %d pay: %v", run, err)
		}
		if captureItemCount(c, amuletAuthentic) != 0 {
			t.Fatalf("run %d kept the authentic amulets", run)
		}
	}
	if _, offered := npcRow(rt, c, def.Codename, def.StartNpcCodename); offered {
		t.Fatal("a third run was offered")
	}
	if row, offered := npcRow(rt, c, second.Codename, second.StartNpcCodename); !offered || row.PromptSymbol != second.OfferPromptSymbol {
		t.Fatalf("two runs did not open the second order: %+v", row)
	}
}
