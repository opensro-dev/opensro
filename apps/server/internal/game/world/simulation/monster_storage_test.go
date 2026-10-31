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
		if s.hot[copy.Gid].ref != s.hot[original.Gid].ref || s.hot[copy.Gid].nest != s.hot[original.Gid].nest {
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
