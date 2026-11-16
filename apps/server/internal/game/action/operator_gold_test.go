/*
===========================================================================

operator_gold_test.go - the operator's gold correction moves gold through
the character door, refuses below zero and shows an online character its
new balance

===========================================================================
*/
package action

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestOperatorAdjustGoldCreditDebitAndPush
================
*/
func TestOperatorAdjustGoldCreditDebitAndPush(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	var pushed []wire.Frame
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) {
		if name == c.Name {
			pushed = append(pushed, frames...)
		}
	}

	done, err := rt.OperatorAdjustGold(testDivision, c.Name, 250)
	if err != nil || done != (GoldAdjustment{Character: c.Name, Requested: 250, Applied: 250, Before: 5000, After: 5250}) {
		t.Fatalf("credit %+v, %v", done, err)
	}
	if *c.Gold != 5250 {
		t.Fatalf("gold after credit %d", *c.Gold)
	}
	done, err = rt.OperatorAdjustGold(testDivision, c.Name, -5250)
	if err != nil || done.Applied != -5250 || done.Before != 5250 || done.After != 0 || *c.Gold != 0 {
		t.Fatalf("debit to exactly zero %+v, %v, gold %d", done, err, *c.Gold)
	}
	// Each committed change pushed the balance it left: an online
	// character sees its new gold without relogging.
	if len(pushed) != 2 {
		t.Fatalf("pushes %+v", pushed)
	}
	for i, want := range []uint64{5250, 0} {
		if pushed[i].Opcode != wire.OpPointsUpdate {
			t.Fatalf("push %d opcode 0x%04X", i, pushed[i].Opcode)
		}
		refresh, err := wire.DecodeGoldRefresh(pushed[i].Payload)
		if err != nil || refresh.Balance != want {
			t.Fatalf("push %d refresh %+v, %v", i, refresh, err)
		}
	}
}

/*
================
TestOperatorAdjustGoldRefusesBelowZeroAndZero

A debit past zero refuses whole - never a clamped partial debit - and
reports the amount that could have applied; the record is untouched and
nothing is pushed.
================
*/
func TestOperatorAdjustGoldRefusesBelowZeroAndZero(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	pushed := 0
	rt.PushCharacterFrames = func(_, _ string, _ []wire.Frame) { pushed++ }
	before := c.Snapshot()

	if _, err := rt.OperatorAdjustGold(testDivision, c.Name, -5001); err == nil ||
		!strings.Contains(err.Error(), "below zero") || !strings.Contains(err.Error(), "could apply -5000") {
		t.Fatalf("below-zero refusal: %v", err)
	}
	if _, err := rt.OperatorAdjustGold(testDivision, c.Name, 0); err == nil {
		t.Fatal("a zero adjustment was applied")
	}
	if _, err := rt.OperatorAdjustGold(testDivision, c.Name, math.MaxInt64); err == nil ||
		!strings.Contains(err.Error(), "overflow") {
		t.Fatalf("overflow refusal: %v", err)
	}
	if _, err := rt.OperatorAdjustGold(testDivision, "Nobody", 1); err == nil {
		t.Fatal("a missing character was adjusted")
	}
	c.DeletePending = true
	if _, err := rt.OperatorAdjustGold(testDivision, c.Name, 1); err == nil {
		t.Fatal("a deletion-pending character was adjusted")
	}
	c.DeletePending = false
	if !reflect.DeepEqual(before, c.Snapshot()) || pushed != 0 {
		t.Fatal("a refused adjustment changed the character or pushed a balance")
	}
}

/*
================
TestOperatorAdjustGoldReachesAnOfflineCharacter

No session and no push hook: the stored record is the balance.
================
*/
func TestOperatorAdjustGoldReachesAnOfflineCharacter(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	rt.PushCharacterFrames = nil
	done, err := rt.OperatorAdjustGold(testDivision, c.Name, -5000)
	if err != nil || done.After != 0 || *c.Gold != 0 {
		t.Fatalf("offline debit %+v, %v", done, err)
	}
}

/*
================
TestOperatorAdjustGoldPersistsThroughAuthorityRestart

The correction commits through the real authority door, so the economy
ledger counts it under operator_adjust and the balance survives a reboot.
================
*/
func TestOperatorAdjustGoldPersistsThroughAuthorityRestart(t *testing.T) {
	seed := testCharacter()
	d := openDoorRuntime(t, t.TempDir(), seed)
	deps := d.rt.deps.(*enterworld.Deps)
	deps.UpdateCharacter = d.authority.UpdateCharacter
	deps.ReadCharacter = func(_ string, read func()) { d.authority.ReadState(read) }
	if _, err := d.rt.OperatorAdjustGold(testDivision, d.character.Name, 250); err != nil {
		t.Fatal(err)
	}
	if _, err := d.rt.OperatorAdjustGold(testDivision, d.character.Name, -100); err != nil {
		t.Fatal(err)
	}
	if health := d.authority.Health(); health.LastError != "" {
		t.Fatalf("storage: %+v", health)
	}
	counters := d.authority.EconomyCounters()
	if counters["gold.operator_adjust.gain"] != 250 || counters["gold.operator_adjust.loss"] != 100 {
		t.Fatalf("ledger counters %v", counters)
	}
	got := d.reboot(t).character
	if got.Gold == nil || *got.Gold != 5150 {
		t.Fatalf("adjustment did not survive restart: %v", got.Gold)
	}
}
