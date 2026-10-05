/*
===========================================================================

pk_test.go - the PK record arithmetic, clocks and the death drop picker

===========================================================================
*/

package pk

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

var testNow = time.Date(2026, 10, 5, 12, 0, 0, 0, time.Local)

/*
================
TestRecordClamps

4EB140 caps the daily delta at 15 before the sum; all three fields clamp.
================
*/
func TestRecordClamps(t *testing.T) {
	c := &domain.Character{}
	if AddDaily(c, 40, testNow) != ChangedDaily || c.PK.DailyCount != MaxDaily {
		t.Fatalf("daily = %d", c.PK.DailyCount)
	}
	if AddDaily(c, 1, testNow) != 0 {
		t.Fatal("a clamped daily reported a change")
	}
	AddTotal(c, 99)
	AddPenalty(c, 1<<30, testNow)
	if c.PK.TotalCount != MaxTotal || c.PK.Penalty != MaxPenalty {
		t.Fatalf("record = %+v", c.PK)
	}
	AddPenalty(c, -MaxPenalty-5, testNow)
	if c.PK.Penalty != 0 {
		t.Fatalf("penalty = %d, want 0", c.PK.Penalty)
	}
}

/*
================
TestMurderBookkeeping

4E2013..4E2081: total 1 -> penalty (2/2)*1200; a victim 12 levels below
weighs 12/5*2+1 = 5 on the daily count.
================
*/
func TestMurderBookkeeping(t *testing.T) {
	c := &domain.Character{}
	changed := RecordMurder(c, 40, 28, testNow)
	if changed != ChangedDaily|ChangedTotal|ChangedPenalty {
		t.Fatalf("changes = %b", changed)
	}
	if c.PK.TotalCount != 1 || c.PK.Penalty != 1200 || c.PK.DailyCount != 5 {
		t.Fatalf("record = %+v", c.PK)
	}
	RecordMurder(c, 40, 45, testNow)
	if c.PK.TotalCount != 2 || c.PK.Penalty != 2400 || c.PK.DailyCount != 6 {
		t.Fatalf("second murder record = %+v", c.PK)
	}
	RecordMurder(c, 40, 45, testNow)
	if c.PK.Penalty != 2400+2400 {
		t.Fatalf("third murder penalty = %d, want +2400", c.PK.Penalty)
	}
}

/*
================
TestMonsterKillRelief
================
*/
func TestMonsterKillRelief(t *testing.T) {
	for _, tc := range []struct {
		monster, player uint8
		want            int32
	}{{54, 50, -10}, {51, 50, -7}, {47, 50, -5}, {50, 50, -5}, {44, 50, -3}, {43, 50, -1}} {
		if got := MonsterKillRelief(tc.monster, tc.player); got != tc.want {
			t.Errorf("monster %d player %d relief %d, want %d", tc.monster, tc.player, got, tc.want)
		}
	}
}

/*
================
TestPenaltyKeeperDecaysTheTotal

The keeper starts when the penalty reaches zero and drops the total once
per 48 h, catching up offline periods.
================
*/
func TestPenaltyKeeperDecaysTheTotal(t *testing.T) {
	c := &domain.Character{PK: &domain.PKRecord{TotalCount: 3, Penalty: 5}}
	AddPenalty(c, -5, testNow)
	if c.PK.TotalDecayAt != testNow.Unix()+totalDecaySeconds {
		t.Fatalf("keeper deadline = %d", c.PK.TotalDecayAt)
	}
	if DecayTotal(c, testNow.Add(47*time.Hour)) != 0 {
		t.Fatal("decayed early")
	}
	DecayTotal(c, testNow.Add(97*time.Hour))
	if c.PK.TotalCount != 1 {
		t.Fatalf("total after two periods = %d", c.PK.TotalCount)
	}
	DecayTotal(c, testNow.Add(200*time.Hour))
	if c.PK.TotalCount != 0 || c.PK.TotalDecayAt != 0 {
		t.Fatalf("finished keeper = %+v", c.PK)
	}
}

/*
================
TestKeeperRepairAndDailyReset
================
*/
func TestKeeperRepairAndDailyReset(t *testing.T) {
	c := &domain.Character{PK: &domain.PKRecord{TotalCount: 2}}
	if RepairKeeper(c, testNow) != ChangedPenalty || c.PK.Penalty != 1 || c.PVPState() != 2 {
		t.Fatalf("repaired record = %+v", c.PK)
	}
	AddDaily(c, 3, testNow)
	if ResetDailyIfStale(c, testNow) != 0 {
		t.Fatal("same-day reset")
	}
	if ResetDailyIfStale(c, testNow.Add(24*time.Hour)) != ChangedDaily || c.PK.DailyCount != 0 {
		t.Fatalf("next-day daily = %d", c.PK.DailyCount)
	}
}

/*
================
TestDeathLossRates

4E6980's table, the level-10 protection and the guild-war exemption.
================
*/
func TestDeathLossRates(t *testing.T) {
	if _, ok := DeathLoss(DeathMonster, 10, 0, false, false); ok {
		t.Fatal("level 10 lost EXP")
	}
	for _, tc := range []struct {
		kind    DeathKind
		penalty uint32
		player  bool
		want    LossRule
	}{
		{DeathMonster, 0, false, LossRule{Percent: 0.02, CapFactor: 100}},
		{DeathMonster, 9, false, LossRule{Percent: 0.06, CapFactor: 300, SP: 60}},
		{DeathPlayer, 0, true, LossRule{Percent: 0.004, CapFactor: 20}},
		{DeathPlayer, 9, true, LossRule{Percent: 0.02, CapFactor: 100, SP: 20}},
		{DeathGuildWar, 9, true, LossRule{Percent: 0.004, CapFactor: 20}},
	} {
		if got, ok := DeathLoss(tc.kind, 40, tc.penalty, tc.player, false); !ok || got != tc.want {
			t.Errorf("kind %d penalty %d: %+v %v, want %+v", tc.kind, tc.penalty, got, ok, tc.want)
		}
	}
	if _, ok := DeathLoss(DeathTeam, 40, 9, true, false); ok {
		t.Fatal("a team kill cost EXP")
	}
}

/*
================
sequence

A deterministic rand() stream.
================
*/
func sequence(values ...uint32) DropRoll {
	return func() (uint32, error) {
		v := values[0]
		values = values[1:]
		return v, nil
	}
}

/*
================
TestDropChance
================
*/
func TestDropChance(t *testing.T) {
	for _, tc := range []struct {
		penalty uint32
		want    int32
	}{{0, 5}, {3999, 30}, {4000, 50}, {14999, 50}, {15000, 70}, {29999, 70}, {30000, 100}} {
		if got := DropChance(tc.penalty); got != tc.want {
			t.Errorf("penalty %d chance %d, want %d", tc.penalty, got, tc.want)
		}
	}
	if drops, _ := RollsDrop(0, sequence(5)); !drops {
		t.Fatal("5 % 101 <= 5 must drop")
	}
	if drops, _ := RollsDrop(0, sequence(6)); drops {
		t.Fatal("6 % 101 > 5 must not drop")
	}
}

/*
================
TestSelectDropSlot

4BB460: a murderer's equipped roll, the weapon reroll, the ammunition
reroll, and the bag fallback by occupied position.
================
*/
func TestSelectDropSlot(t *testing.T) {
	slots := make([]DropSlot, 45)
	for _, i := range []int{0, 1, 6, 7, 13, 20, 30} {
		slots[i] = DropSlot{Occupied: true, Droppable: true}
	}
	slots[7].Ammunition = true
	slots[20].Droppable = false

	// Equipped 1 drops.
	if slot, ok, _ := SelectDropSlot(slots, 9, sequence(1)); !ok || slot != 1 {
		t.Fatalf("murderer roll 1 -> %d %v", slot, ok)
	}
	// 6 (weapon) rerolls to 0.
	if slot, ok, _ := SelectDropSlot(slots, 9, sequence(6, 6)); !ok || slot != 0 {
		t.Fatalf("weapon reroll -> %d %v", slot, ok)
	}
	// 7 holding arrows rerolls; 2 is empty, so the bag answers: 3 % 3 + 1 = the first.
	if slot, ok, _ := SelectDropSlot(slots, 9, sequence(7, 2, 3)); !ok || slot != 13 {
		t.Fatalf("ammunition reroll -> %d %v", slot, ok)
	}
	// No penalty: bag only; the second occupied bag slot cannot drop.
	if _, ok, _ := SelectDropSlot(slots, 0, sequence(1)); ok {
		t.Fatal("an undroppable bag item dropped")
	}
	if slot, ok, _ := SelectDropSlot(slots, 0, sequence(2)); !ok || slot != 30 {
		t.Fatalf("third bag item -> %d %v", slot, ok)
	}
}
