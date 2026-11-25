/*
===========================================================================

contracts_slice5_test.go - slice 5's abuse-proof economy rules

An AI-trader escort pays only when the Hunter stayed in range for the
whole run and the trader survived it; one Hunter holds one escort at a
time; paid escorts are capped per Hunter per day in the ledger; hiring
an AI Hunter debits the trader's purse before it binds.

===========================================================================
*/
package caravanai

import (
	"context"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

// deliverNow completes one trader's run inside the runtime, as the
// final anchor would, and fires the queued economy hooks.
func deliverNow(t *testing.T, rt *Runtime, division, agentName string, nowMs int64) {
	t.Helper()
	rt.mu.Lock()
	unit := rt.findAgentLocked(division, agentName)
	if unit == nil {
		rt.mu.Unlock()
		t.Fatalf("no agent %s in %s", agentName, division)
	}
	rt.deliverAgentLocked(unit, nowMs)
	rt.mu.Unlock()
	rt.drainEconomy(division)
}

// fakeEscortDayStore is an in-memory EscortDayStore.
type fakeEscortDayStore struct {
	rows map[string]domain.CaravanEscortDayRow
}

func newFakeEscortDayStore() *fakeEscortDayStore {
	return &fakeEscortDayStore{rows: make(map[string]domain.CaravanEscortDayRow)}
}

func (s *fakeEscortDayStore) LoadCaravanEscortDays(division string) ([]domain.CaravanEscortDayRow, error) {
	var out []domain.CaravanEscortDayRow
	for _, row := range s.rows {
		if row.Division == division {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *fakeEscortDayStore) SaveCaravanEscortDay(row domain.CaravanEscortDayRow) error {
	s.rows[row.Division+"|"+row.HunterName+"|"+row.Day] = row
	return nil
}

/*
==================
TestOneEscortPerHunter

A Hunter holding an active AI-trader escort is refused a second one
until the first completes; another Hunter is unaffected.
==================
*/
func TestOneEscortPerHunter(t *testing.T) {
	book := NewContractBook(nil)
	if _, bound := book.BindAITraderEscort("div", "[AI]Trader_Jin", "IronGuard", TownJangan, TownDonwhang, 3, 1_000); !bound {
		t.Fatal("the first escort did not bind")
	}
	if _, bound := book.BindAITraderEscort("div", "[AI]Trader_Mei", "IronGuard", TownJangan, TownHotan, 4, 2_000); bound {
		t.Fatal("a second concurrent escort bound for one Hunter")
	}
	if _, bound := book.BindAITraderEscort("div", "[AI]Trader_Mei", "OtherGuard", TownJangan, TownHotan, 4, 2_000); !bound {
		t.Fatal("another Hunter's escort did not bind")
	}
	book.CompleteAITraderRun("div", "[AI]Trader_Jin", RegionDonwhangTown, 3_000, nil)
	if _, bound := book.BindAITraderEscort("div", "[AI]Trader_Mei", "IronGuard", TownJangan, TownHotan, 4, 4_000); !bound {
		t.Fatal("the Hunter could not sign again after completing a run")
	}
}

/*
==================
TestEscortWatchAndVoidGatePayment

One step out of range, or the trader's death on the road, completes
the run unpaid; a kept, survived run pays and records trust.
==================
*/
func TestEscortWatchAndVoidGatePayment(t *testing.T) {
	memories := NewMemoryBook(nil)

	book := NewContractBook(nil)
	book.BindAITraderEscort("div", "[AI]Trader_Jin", "IronGuard", TownJangan, TownDonwhang, 3, 1_000)
	book.NoteEscortWatch("div", "[AI]Trader_Jin", false)
	if _, completed, payable := book.CompleteAITraderRun("div", "[AI]Trader_Jin", RegionDonwhangTown, 2_000, memories); !completed || payable {
		t.Fatalf("an abandoned escort completed payable (completed=%v payable=%v)", completed, payable)
	}
	if _, ok := memories.Relationship("div", "[AI]Trader_Jin", "IronGuard"); ok {
		t.Fatal("an abandoned escort recorded trust")
	}

	book.BindAITraderEscort("div", "[AI]Trader_Jin", "IronGuard", TownJangan, TownDonwhang, 3, 3_000)
	book.NoteEscortWatch("div", "[AI]Trader_Jin", true)
	book.VoidTraderEscort("div", "[AI]Trader_Jin")
	if _, completed, payable := book.CompleteAITraderRun("div", "[AI]Trader_Jin", RegionDonwhangTown, 4_000, memories); !completed || payable {
		t.Fatalf("a dead trader's escort completed payable (completed=%v payable=%v)", completed, payable)
	}

	book.BindAITraderEscort("div", "[AI]Trader_Jin", "IronGuard", TownJangan, TownDonwhang, 3, 5_000)
	book.NoteEscortWatch("div", "[AI]Trader_Jin", true)
	if _, completed, payable := book.CompleteAITraderRun("div", "[AI]Trader_Jin", RegionDonwhangTown, 6_000, memories); !completed || !payable {
		t.Fatalf("a kept, survived escort did not complete payable (completed=%v payable=%v)", completed, payable)
	}
	if row, ok := memories.Relationship("div", "[AI]Trader_Jin", "IronGuard"); !ok || row.TrustScore != 25 {
		t.Fatalf("a kept escort recorded trust %+v (ok=%v)", row, ok)
	}
}

/*
==================
TestDailyEscortLedger

Paid escorts count per Hunter per UTC day, flush to the ledger, and a
fresh book over the same store inherits the counts.
==================
*/
func TestDailyEscortLedger(t *testing.T) {
	store := newFakeEscortDayStore()
	book := NewContractBook(store)
	const now = int64(1_700_000_000_000)
	book.RecordPaidEscort("div", "IronGuard", now)
	book.RecordPaidEscort("div", "IronGuard", now)
	if got := book.PaidEscortsToday("div", "IronGuard", now); got != 2 {
		t.Fatalf("paid today = %d, want 2", got)
	}
	if got := book.PaidEscortsToday("div", "IronGuard", now+86_400_000); got != 0 {
		t.Fatalf("paid tomorrow = %d, want 0", got)
	}
	if err := book.Flush(); err != nil {
		t.Fatal(err)
	}
	fresh := NewContractBook(store)
	if got := fresh.PaidEscortsToday("div", "IronGuard", now); got != 2 {
		t.Fatalf("paid today after reload = %d, want 2", got)
	}
}

/*
==================
TestDeliveryPaysOncePerKeptRunUnderTheDailyCap

End to end through the runtime: a kept run pays the Hunter once
through OnContractReward; after MaxPaidEscortsPerDay paid runs the
next delivery completes silently unpaid.
==================
*/
func TestDeliveryPaysOncePerKeptRunUnderTheDailyCap(t *testing.T) {
	payments := 0
	var lastGold int64
	rt := NewRuntime(
		Config{Enabled: true, BrainMode: BrainHardcoded},
		nil, nil,
		WorldHooks{OnContractReward: func(_ string, hunterPlayerName string, rewardGold int64, _ string) {
			if hunterPlayerName != "IronGuard" {
				t.Errorf("paid %q, want IronGuard", hunterPlayerName)
			}
			payments++
			lastGold = rewardGold
		}},
	)
	const div = "Shard_Silkroad"
	rt.EnsureDivisionRoster(div)

	runs := MaxPaidEscortsPerDay + 1
	for run := range runs {
		now := int64(run+1) * 100_000
		if _, handled := rt.HandlePlayerCommand(div, "IronGuard", domain.JobHunter, "!accept [AI]Trader_Jin", now); !handled {
			t.Fatalf("run %d: the accept was not handled", run)
		}
		if _, _, ok := rt.StepAgent(context.Background(), div, "[AI]Trader_Jin", []ObservedPlayer{
			{Name: "IronGuard", JobType: domain.JobHunter, HPPercent: 100, Distance: 25},
		}, now+1_000); !ok {
			t.Fatalf("run %d: the trader did not step", run)
		}
		deliverNow(t, rt, div, "[AI]Trader_Jin", now+2_000)
		want := min(run+1, MaxPaidEscortsPerDay)
		if payments != want {
			t.Fatalf("after run %d: %d payments, want %d", run, payments, want)
		}
	}
	if lastGold <= 0 {
		t.Fatalf("the last payment was %d gold", lastGold)
	}
	if got := rt.Contracts().PaidEscortsToday(div, "IronGuard", 100_000); got != MaxPaidEscortsPerDay {
		t.Fatalf("paid today = %d, want the cap %d", got, MaxPaidEscortsPerDay)
	}
}

/*
==================
TestHireFeeRefusalBindsNobody

The ChargeHireFee hook is asked before a hire binds: false refuses the
hire in the agent's words and no contract exists; true binds.
==================
*/
func TestHireFeeRefusalBindsNobody(t *testing.T) {
	affordable := false
	charged := 0
	rt := NewRuntime(
		Config{Enabled: true, BrainMode: BrainHardcoded},
		nil, nil,
		WorldHooks{ChargeHireFee: func(_, _ string, feeGold int64, _ string) bool {
			charged++
			if feeGold != BaseHunterHireFeeGold {
				t.Errorf("the hook was asked %d gold, want the hire fee %d", feeGold, BaseHunterHireFeeGold)
			}
			return affordable
		}},
	)
	const div = "Shard_Silkroad"
	rt.EnsureDivisionRoster(div)

	reply, handled := rt.HandlePlayerCommand(div, "MerchantLin", domain.JobTrader, "!hire [AI]Hunter_Kwan", 1_000)
	if !handled || !strings.Contains(reply, "cannot pay") {
		t.Fatalf("expected a hire-fee refusal, got handled=%v reply=%q", handled, reply)
	}
	if _, ok := rt.Contracts().ActiveContractFor(div, "[AI]Hunter_Kwan"); ok {
		t.Fatal("a refused hire bound a contract")
	}

	affordable = true
	reply, handled = rt.HandlePlayerCommand(div, "MerchantLin", domain.JobTrader, "!hire [AI]Hunter_Kwan", 10_000)
	if !handled || !strings.Contains(reply, "MerchantLin") {
		t.Fatalf("expected an affordable hire to bind, got handled=%v reply=%q", handled, reply)
	}
	if _, ok := rt.Contracts().ActiveContractFor(div, "[AI]Hunter_Kwan"); !ok {
		t.Fatal("an affordable hire did not bind")
	}
	if charged != 2 {
		t.Fatalf("the fee hook was asked %d times, want 2", charged)
	}
}
