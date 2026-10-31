package simulation

import (
	"math/rand"
	"opensro.online/server/internal/game/world/monster"
	"reflect"
	"testing"
)

func TestResidentMonsterRoundTripAndIsolation(t *testing.T) {
	random := rand.New(rand.NewSource(11))
	s := newMonsterStorage(nil)
	for n := 0; n < 100; n++ {
		original := monster.Instance{}
		fillResidentTestValue(reflect.ValueOf(&original).Elem(), random)
		s.set(original.Gid, original)
		if got := s.get(original.Gid); got != original {
			a, b := reflect.ValueOf(original), reflect.ValueOf(got)
			for i := 0; i < a.NumField(); i++ {
				if a.Field(i).Interface() != b.Field(i).Interface() {
					t.Errorf("field %s: want %+v got %+v", a.Type().Field(i).Name, a.Field(i).Interface(), b.Field(i).Interface())
				}
			}
			t.Fatal("resident storage lost instance fields")
		}
		copy := original
		copy.Gid++
		s.set(copy.Gid, copy)
		copyRow, copyHot := s.hotRow(copy.Gid)
		originalRow, originalHot := s.hotRow(original.Gid)
		if !copyHot || !originalHot || copyRow.ref != originalRow.ref || copyRow.nest != originalRow.nest {
			t.Fatal("static definitions not shared")
		}
		copy.Ref.MaxHP++
		copy.CurrentHP++
		s.set(copy.Gid, copy)
		if s.get(original.Gid) != original || s.get(copy.Gid) != copy {
			t.Fatal("mutation leaked across actors")
		}
		s.remove(original.Gid)
		if got, ok := s.lookup(original.Gid); ok || got != (monster.Instance{}) {
			t.Fatal("deleted actor survived")
		}
	}
}

/*
================
TestLeaderFactsMatchLookup

The FOLLOW transaction's field-level leader facts must answer exactly
what the full lookup answers, in either residency tier: a divergence
would move a commit's fresh compare with no behaviour test watching.
The leader's radius is grade-scaled on purpose.
================
*/
func TestLeaderFactsMatchLookup(t *testing.T) {
	s := newMonsterStorage(nil)
	leader := monster.Instance{Gid: 7, CurrentHP: 42}
	leader.Ref.BodyRadius = 10
	leader.Ref.MonsterType = 4 // giant (4): BodyRadius scales by GradeScale
	s.set(leader.Gid, leader)
	check := func(gid uint32) {
		hp, radius, exists := s.leaderFacts(gid)
		want, wantExists := s.lookup(gid)
		if exists != wantExists || hp != want.CurrentHP || radius != want.BodyRadius() {
			t.Fatalf("gid %d: facts (%d, %v, %v), lookup (%d, %v, %v)",
				gid, hp, radius, exists, want.CurrentHP, want.BodyRadius(), wantExists)
		}
	}
	check(leader.Gid)
	check(404)
	s.archive = &monsterArchive{}
	s.freeze(leader.Gid)
	if _, cold := s.cold[leader.Gid]; !cold {
		t.Fatal("leader did not freeze")
	}
	check(leader.Gid)
}

func fillResidentTestValue(v reflect.Value, r *rand.Rand) {
	if !v.CanSet() {
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			fillResidentTestValue(v.Field(i), r)
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			fillResidentTestValue(v.Index(i), r)
		}
	case reflect.String:
		v.SetString("reference-" + string(rune('A'+r.Intn(26))))
	case reflect.Bool:
		v.SetBool(r.Intn(2) == 1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(r.Float64() * 1000)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(r.Uint64())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(r.Int63())
	}
}

/*
================
TestRemoveHotKeepsTheMovedRowReachable

Swap-removal moves the last row into the hole, so the moved row's slot
entry must follow it. Removing the last row itself moves nothing.
================
*/
func TestRemoveHotKeepsTheMovedRowReachable(t *testing.T) {
	storage := newMonsterStorage(nil)
	for gid := uint32(1); gid <= 4; gid++ {
		storage.set(gid, monster.Instance{Gid: gid, CurrentHP: gid * 10})
	}
	storage.remove(2)
	storage.remove(3)
	if _, ok := storage.hotRow(2); ok {
		t.Fatal("a removed row is still reachable")
	}
	for _, gid := range []uint32{1, 4} {
		row, ok := storage.hotRow(gid)
		if !ok || row.currentHP != gid*10 {
			t.Fatalf("gid %d after removal: found %v, hp %d", gid, ok, row.currentHP)
		}
	}
	if len(storage.hot) != 2 || len(storage.byGID) != 2 {
		t.Fatalf("hot %d rows, %d slots, want 2 of each", len(storage.hot), len(storage.byGID))
	}
}

/*
================
TestShrinkHotExcessRebuildsBothHalves

The rebuild runs only after a large contraction, and the rows and slot
map must still agree after it. Below the threshold it copies nothing.
================
*/
func TestShrinkHotExcessRebuildsBothHalves(t *testing.T) {
	storage := newMonsterStorage(nil)
	for gid := uint32(1); gid <= 8192; gid++ {
		storage.set(gid, monster.Instance{Gid: gid, CurrentHP: gid})
	}
	peak := 0
	storage.shrinkHotExcess(&peak, true)
	if peak != 8192 || cap(storage.hot) < 8192 {
		t.Fatalf("a full storage shrank: peak %d, capacity %d", peak, cap(storage.hot))
	}
	for gid := uint32(11); gid <= 8192; gid++ {
		storage.remove(gid)
	}
	storage.shrinkHotExcess(&peak, false)
	if cap(storage.hot) < 8192 {
		t.Fatal("a shrink ran without being allowed")
	}
	storage.shrinkHotExcess(&peak, true)
	if cap(storage.hot) != 10 || peak != 10 {
		t.Fatalf("no shrink: capacity %d, peak %d", cap(storage.hot), peak)
	}
	for gid := uint32(1); gid <= 10; gid++ {
		if row, ok := storage.hotRow(gid); !ok || row.currentHP != gid {
			t.Fatalf("gid %d lost in the rebuild", gid)
		}
	}
}
