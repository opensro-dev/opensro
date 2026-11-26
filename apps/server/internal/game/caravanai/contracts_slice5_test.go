/*
===========================================================================

contracts_slice5_test.go - the escort economy's abuse rules (#3704 slice 5)

An AI-trader escort pays only a run the Hunter was sampled beside for
most of its length, with no observation gap, that lasted the minimum
and whose trader lived; one Hunter holds one escort and one trader
takes one Hunter at a time; paid escorts are capped per Hunter per UTC
day in the escort-day ledger, which survives a restart; a hire whose
fee cannot be paid binds nobody.

===========================================================================
*/
package caravanai

import (
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

// stepMs is one runtime step, as the tick paces StepAgent.
const stepMs = 100

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
================
sampleRun

Samples the escort of trader every second from fromMs to toMs, near or
not, as StepAgent does once per step.
================
*/
func sampleRun(book *ContractBook, division, trader string, near bool, fromMs, toMs int64) {
	for now := fromMs; now <= toMs; now += 1_000 {
		book.SampleEscort(division, trader, near, now)
	}
}

/*
==================
TestOneEscortPerHunterAndPerTrader

A Hunter holding an active escort is refused a second one, and a trader
under contract is refused a second Hunter, until the run ends.
==================
*/
func TestOneEscortPerHunterAndPerTrader(t *testing.T) {
	book := NewContractBook(nil)
	if _, bound := book.BindAITraderEscort("div", "[AI]Trader_Jin", "IronGuard", TownJangan, TownDonwhang, 3, 1_000); !bound {
		t.Fatal("the first escort did not bind")
	}
	if _, bound := book.BindAITraderEscort("div", "[AI]Trader_Mei", "IronGuard", TownJangan, TownHotan, 4, 2_000); bound {
		t.Fatal("a second concurrent escort bound for one Hunter")
	}
	if _, bound := book.BindAITraderEscort("div", "[AI]Trader_Jin", "OtherGuard", TownJangan, TownDonwhang, 3, 2_000); bound {
		t.Fatal("a trader under contract took a second Hunter")
	}
	if _, bound := book.BindAITraderEscort("div", "[AI]Trader_Mei", "OtherGuard", TownJangan, TownHotan, 4, 2_000); !bound {
		t.Fatal("another Hunter's escort of a free trader did not bind")
	}
	book.CompleteAITraderRun("div", "[AI]Trader_Jin", RegionDonwhangTown, 3_000, nil)
	if _, bound := book.BindAITraderEscort("div", "[AI]Trader_Jin", "IronGuard", TownDonwhang, TownJangan, 4, 4_000); !bound {
		t.Fatal("the Hunter could not sign again after its run ended")
	}
}

/*
==================
TestEscortSamplingGatesPayment

A run pays only when it lasted escortMinRunMs, the Hunter was sampled
near for escortNearPercent of it and no gap passed escortSampleGapMs;
a dead trader's contract is cancelled and never completes. Only a
payable run records the +25 escort trust.
==================
*/
func TestEscortSamplingGatesPayment(t *testing.T) {
	memories := NewMemoryBook(nil)
	book := NewContractBook(nil)
	const trader = "[AI]Trader_Jin"
	run := func(start int64, sample func(start int64), end int64) (completed, payable bool) {
		t.Helper()
		if _, bound := book.BindAITraderEscort("div", trader, "IronGuard", TownJangan, TownDonwhang, 3, start); !bound {
			t.Fatalf("the escort at %d did not bind", start)
		}
		sample(start)
		_, completed, payable = book.CompleteAITraderRun("div", trader, RegionDonwhangTown, end, memories)
		return completed, payable
	}

	// Away for the whole road.
	if completed, payable := run(0, func(s int64) { sampleRun(book, "div", trader, false, s+1_000, s+40_000) }, 40_000); !completed || payable {
		t.Fatalf("an abandoned escort: completed=%v payable=%v", completed, payable)
	}
	// Near, then away for the last fifth: under the 90% share.
	if completed, payable := run(100_000, func(s int64) {
		sampleRun(book, "div", trader, true, s+1_000, s+32_000)
		sampleRun(book, "div", trader, false, s+33_000, s+40_000)
	}, 140_000); !completed || payable {
		t.Fatalf("an escort near for 80%% of the run: completed=%v payable=%v", completed, payable)
	}
	// Near, but one gap past escortSampleGapMs (the Hunter went offline).
	if completed, payable := run(200_000, func(s int64) {
		sampleRun(book, "div", trader, true, s+1_000, s+15_000)
		sampleRun(book, "div", trader, true, s+15_000+escortSampleGapMs+1_000, s+40_000)
	}, 240_000); !completed || payable {
		t.Fatalf("an escort with an observation gap: completed=%v payable=%v", completed, payable)
	}
	// Near all the way, but shorter than escortMinRunMs.
	if completed, payable := run(300_000, func(s int64) { sampleRun(book, "div", trader, true, s+1_000, s+20_000) }, 320_000); !completed || payable {
		t.Fatalf("a run shorter than the minimum: completed=%v payable=%v", completed, payable)
	}
	if _, ok := memories.Relationship("div", trader, "IronGuard"); ok {
		t.Fatal("an unpaid escort recorded trust")
	}
	// The trader died: its respawn cancels the contract, which never completes.
	if _, bound := book.BindAITraderEscort("div", trader, "IronGuard", TownJangan, TownDonwhang, 3, 400_000); !bound {
		t.Fatal("the escort after the short run did not bind")
	}
	sampleRun(book, "div", trader, true, 401_000, 420_000)
	book.Cancel("div", trader)
	if _, completed, _ := book.CompleteAITraderRun("div", trader, RegionDonwhangTown, 440_000, memories); completed {
		t.Fatal("a dead trader's cancelled escort completed")
	}
	// Kept and survived: pays, with trust.
	if completed, payable := run(500_000, func(s int64) { sampleRun(book, "div", trader, true, s+1_000, s+40_000) }, 540_000); !completed || !payable {
		t.Fatalf("a kept escort: completed=%v payable=%v", completed, payable)
	}
	if row, ok := memories.Relationship("div", trader, "IronGuard"); !ok || row.TrustScore != 25 {
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
================
deliverNow

Completes trader's run inside the runtime, as its last waypoint would:
the trader is set onto the final waypoint and advanced once. It
returns the contract the delivery pays, or nil.
================
*/
func deliverNow(t *testing.T, rt *Runtime, trader string, nowMs int64) *EscortContract {
	t.Helper()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	unit := rt.findAgentLocked("division", trader)
	if unit == nil || len(unit.Route) == 0 {
		t.Fatalf("no routed agent %s", trader)
	}
	unit.WaypointIndex = len(unit.Route) - 1
	// Placed a step earlier, so this step has time to cover the zero
	// distance left and arrives.
	unit.place(unit.Route[unit.WaypointIndex], nowMs-stepMs)
	return rt.advanceAlongRouteLocked(unit, nowMs)
}

/*
==================
TestDeliveryPaysUnderTheDailyCap

Through the runtime: each kept run's delivery pays the Hunter, until
MaxPaidEscortsPerDay paid runs; then a capped Hunter is refused at
!accept, and a run signed before the cap filled delivers unpaid.
==================
*/
func TestDeliveryPaysUnderTheDailyCap(t *testing.T) {
	players := map[string]*hirePlayer{"IronGuard": {job: domain.JobHunter, online: true}}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	const trader = "[AI]Trader_Jin"
	accept := func(now int64) string {
		t.Helper()
		rt.mu.Lock()
		unit := rt.findAgentLocked("division", trader)
		unit.State, unit.RunKind = StateRecruiting, RunNone
		players["IronGuard"].at = position(unit)
		rt.mu.Unlock()
		reply, handled := rt.HandlePlayerCommand("division", "IronGuard", domain.JobHunter, "!accept "+trader, now)
		if !handled {
			t.Fatalf("the accept at %d was not handled", now)
		}
		return reply
	}
	keptRun := func(now int64) *EscortContract {
		t.Helper()
		sampleRun(rt.Contracts(), "division", trader, true, now+1_000, now+40_000)
		return deliverNow(t, rt, trader, now+40_000)
	}
	for run := range MaxPaidEscortsPerDay {
		now := int64(run+1) * 1_000_000
		if reply := accept(now); !strings.Contains(reply, "IronGuard") {
			t.Fatalf("run %d: the accept was refused: %q", run, reply)
		}
		contract := keptRun(now)
		if contract == nil || contract.HunterName != "IronGuard" || contract.RewardGold <= 0 {
			t.Fatalf("run %d: a kept run under the cap paid %+v", run, contract)
		}
	}
	if got := rt.Contracts().PaidEscortsToday("division", "IronGuard", 1_000_000); got != MaxPaidEscortsPerDay {
		t.Fatalf("paid today = %d, want the cap %d", got, MaxPaidEscortsPerDay)
	}
	if reply := accept(10_000_000); !strings.Contains(reply, "today's") {
		t.Fatalf("a capped Hunter was not refused at !accept: %q", reply)
	}
	if _, ok := rt.Contracts().ActiveContractFor("division", trader); ok {
		t.Fatal("a capped Hunter's refused accept bound a contract")
	}

	// The next UTC day: the accept binds, then the day's ledger fills
	// from elsewhere before delivery, and the run delivers unpaid.
	nextDay := int64(86_400_000) + 1_000_000
	if reply := accept(nextDay); !strings.Contains(reply, "IronGuard") {
		t.Fatalf("the next day's accept was refused: %q", reply)
	}
	for range MaxPaidEscortsPerDay {
		rt.Contracts().RecordPaidEscort("division", "IronGuard", nextDay)
	}
	if contract := keptRun(nextDay); contract != nil {
		t.Fatalf("a run delivered past the cap paid %+v", *contract)
	}
}

/*
==================
TestHireFeeRefusalBindsNobody

OnHireFee is asked before a hire binds: false refuses it with the price
and leaves the agent free; true binds it.
==================
*/
func TestHireFeeRefusalBindsNobody(t *testing.T) {
	players := map[string]*hirePlayer{"MerchantLin": {at: TownWaypoint(TownJangan), job: domain.JobTrader, online: true}}
	paid := map[string]int64{}
	rt := hireRuntime(t, players, &paid)
	affordable, asked := false, 0
	rt.hooks.OnHireFee = func(_, _ string, fee int64) bool {
		asked++
		if fee < HireFeeGold {
			t.Errorf("the hook was asked %d gold, under the hire fee %d", fee, HireFeeGold)
		}
		return affordable
	}
	if reply := rt.HireAgent("division", "MerchantLin", domain.JobTrader, "[AI]Hunter_Kwan", 1_000); !strings.Contains(reply, "costs") {
		t.Fatalf("expected the price refusal, got %q", reply)
	}
	rt.mu.Lock()
	hiredBy := rt.findAgentLocked("division", "[AI]Hunter_Kwan").HiredBy
	rt.mu.Unlock()
	if hiredBy != "" {
		t.Fatalf("a refused hire bound the agent to %q", hiredBy)
	}
	affordable = true
	if reply := rt.HireAgent("division", "MerchantLin", domain.JobTrader, "[AI]Hunter_Kwan", 10_000); !strings.Contains(reply, "joins you") {
		t.Fatalf("expected an affordable hire to bind, got %q", reply)
	}
	if asked != 2 {
		t.Fatalf("the fee hook was asked %d times, want 2", asked)
	}
}
