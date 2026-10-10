package fortress

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/instance"
)

/*
================
TestFreshShardHoldsEveryFortressUnoccupied

New builds the state of a shard no war has run on: the siege world binds to
its fortress row, nobody owns it, and no war mode is set. Divisions do not
share their tables.
================
*/
func TestFreshShardHoldsEveryFortressUnoccupied(t *testing.T) {
	a := New([]Catalog{{ID: 1, CodeName: "FORTRESS_JANGAN", MaxEntrance: 300}})
	jangan, _ := instance.Lookup(2)
	field, _ := instance.Lookup(1)
	if id, ok := a.ForWorld(jangan); !ok || id != 1 {
		t.Fatalf("INS_FORT_JA bound to fortress %d (%v)", id, ok)
	}
	if _, ok := a.ForWorld(field); ok {
		t.Fatal("the field resolved to a fortress")
	}
	record, ok := a.Get("a", 1)
	if !ok || record.GuildID != 0 || record.MaxEntrance != 300 {
		t.Fatalf("fresh record %+v", record)
	}
	if a.GuildOwns("a", 1, 0) || a.GuildOwns("a", 1, 7) || a.WarActive("a") {
		t.Fatal("an unoccupied fortress has an owner or a war")
	}
	a.divisions["a"].records[1].GuildID = 7
	if !a.GuildOwns("a", 1, 7) || a.GuildOwns("b", 1, 7) {
		t.Fatal("occupation did not stay in its division")
	}
}

/*
================
TestWarActiveTakesNoLock

Action code asks WarActive while holding a character store door, and
CollectTax/HireStaff hold the authority's lock while they write the store;
a locked read would invert that order. WarActive must answer while the
authority's lock is held, and see the war SetPeriod set.
================
*/
func TestWarActiveTakesNoLock(t *testing.T) {
	a := New([]Catalog{{ID: 1}})
	a.SetPeriod("d", PeriodWar, true)
	a.mu.Lock()
	answered := make(chan bool, 1)
	go func() { answered <- a.WarActive("d") }()
	select {
	case on := <-answered:
		a.mu.Unlock()
		if !on {
			t.Fatal("WarActive missed the war")
		}
	case <-time.After(2 * time.Second):
		a.mu.Unlock()
		t.Fatal("WarActive waited on the authority's lock")
	}
	a.SetPeriod("d", PeriodWar, false)
	if a.WarActive("d") {
		t.Fatal("WarActive kept an ended war")
	}
}
