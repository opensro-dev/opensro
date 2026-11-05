/*
===========================================================================

registry_reads_test.go - the registry's cheap reads (#2030)

The bootstrap and the per-tick scope cache once paid a deep copy of
every drop in a division to read a fraction of what they needed:
codenames for the reference seam, one population's rows for a world
entry, a rebuild test per division. These tests pin the lock-side
reads to the deep copy's answers, byte for byte, and the benchmarks
price them against it at the live plane's size (1,600 drops).

===========================================================================
*/
package grounditem

import (
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestRegistryCodenamesNamesEachDropOnceInGidOrder

The reference seam's names: distinct codenames, first sighting in
entity-id order, empty codenames skipped, divisions apart - exactly
what the All-walking read produced.
================
*/
func TestRegistryCodenamesNamesEachDropOnceInGidOrder(t *testing.T) {
	r := NewRegistry()
	r.Add("a", Item{Codename: "ITEM_Z"})
	r.Add("a", Item{Codename: "ITEM_A"})
	r.Add("a", Item{Codename: "ITEM_Z"})
	r.Add("a", Item{})
	r.Add("b", Item{Codename: "ITEM_B"})

	if got := r.Codenames("a"); len(got) != 2 || got[0] != "ITEM_Z" || got[1] != "ITEM_A" {
		t.Fatalf("codenames = %v, want [ITEM_Z ITEM_A]", got)
	}
	if got := r.Codenames("b"); len(got) != 1 || got[0] != "ITEM_B" {
		t.Fatalf("codenames = %v, want [ITEM_B]", got)
	}
	if got := r.Codenames("never-held"); len(got) != 0 {
		t.Fatalf("codenames = %v, want none", got)
	}
}

/*
================
TestRegistryAllInPopulationCopiesOnlyItsOwn

The population read answers exactly what filtering All answered -
same rows, same order, detached the same way - across a division
whose other populations it must not copy.
================
*/
func TestRegistryAllInPopulationCopiesOnlyItsOwn(t *testing.T) {
	r := NewRegistry()
	dungeon := Population{0x1000a, 2}
	r.Add("a", Item{Codename: "ITEM_MAIN"})
	want1 := r.Add("a", Item{Codename: "ITEM_DEN", Population: dungeon,
		MagicOptions: []uint64{7, 8}, Summon: &domain.CharacterCOS{GID: 42, Level: 9}})
	r.Add("a", Item{Codename: "ITEM_OTHER", Population: Population{0x1000a, 3}})
	want2 := r.Add("a", Item{Codename: "ITEM_DEN2", Population: dungeon})

	var want []Item
	for _, item := range r.All("a") {
		if item.Population == dungeon {
			want = append(want, item)
		}
	}
	got := r.AllInPopulation("a", dungeon)
	if len(got) != len(want) || len(got) != 2 {
		t.Fatalf("population rows = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Gid != want[i].Gid || got[i].Codename != want[i].Codename {
			t.Fatalf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got[0].Gid != want1.Gid || got[1].Gid != want2.Gid {
		t.Fatalf("rows out of entity order: %d, %d", got[0].Gid, got[1].Gid)
	}
	// Detached like All: writing through a returned row reaches nothing.
	got[0].MagicOptions[0] = 99
	got[0].Summon.Level = 1
	stored, ok := r.Get("a", want1.Gid)
	if !ok || stored.MagicOptions[0] != 7 || stored.Summon.Level != 9 {
		t.Fatalf("population read escaped registry ownership: %+v", stored)
	}
	if rows := r.AllInPopulation("a", Population{World: 0x1000a}); rows != nil {
		t.Fatalf("generation-less population read %d rows, want none", len(rows))
	}
	if rows := r.AllInPopulation("a", Population{0x1000a, 99}); rows != nil {
		t.Fatalf("unheld population read %d rows, want none", len(rows))
	}
}

/*
================
TestRegistryDivisionRevisionTracksOnlyItsDivision

Every mutation site moves its own division's counter and no other:
the scope cache rebuilds a division only for that division's edits,
where the one global counter rebuilt them all.
================
*/
func TestRegistryDivisionRevisionTracksOnlyItsDivision(t *testing.T) {
	r := NewRegistry()
	if r.DivisionRevision("a") != 0 || r.DivisionRevision("b") != 0 {
		t.Fatal("a fresh registry already counts mutations")
	}
	first := r.Add("a", Item{Codename: "ITEM_A"})
	r.Add("b", Item{Codename: "ITEM_B"})
	if r.DivisionRevision("a") != 1 || r.DivisionRevision("b") != 1 {
		t.Fatalf("after adds: a=%d b=%d, want 1 and 1", r.DivisionRevision("a"), r.DivisionRevision("b"))
	}
	if _, ok := r.Remove("a", first.Gid); !ok {
		t.Fatal("seeded drop vanished")
	}
	if r.DivisionRevision("a") != 2 || r.DivisionRevision("b") != 1 {
		t.Fatalf("after remove: a=%d b=%d, want 2 and 1", r.DivisionRevision("a"), r.DivisionRevision("b"))
	}
	second := r.Add("a", Item{Codename: "ITEM_A2", StackCount: 3})
	if !r.SetStackCount("a", second.Gid, 2) {
		t.Fatal("seeded drop vanished")
	}
	if r.DivisionRevision("a") != 4 || r.DivisionRevision("b") != 1 {
		t.Fatalf("after stack rewrite: a=%d b=%d, want 4 and 1", r.DivisionRevision("a"), r.DivisionRevision("b"))
	}
	global := r.Revision()
	if global == 0 || r.DivisionRevision("a")+r.DivisionRevision("b") > global {
		t.Fatalf("global counter %d no longer covers the divisions", global)
	}
	r.Clear("a")
	if r.DivisionRevision("a") != 5 || r.DivisionRevision("b") != 1 {
		t.Fatalf("after clear: a=%d b=%d, want 5 and 1", r.DivisionRevision("a"), r.DivisionRevision("b"))
	}
}

/*
================
TestRegistryDivisionRevisionFollowsRestore

A restore replaces the whole plane: divisions the registry held and
divisions the snapshot brings alike take a fresh stamp, so no cache
keeps serving its pre-restore copy.
================
*/
func TestRegistryDivisionRevisionFollowsRestore(t *testing.T) {
	source := NewRegistry()
	source.Add("held", Item{Codename: "ITEM_A"})
	source.Add("restored", Item{Codename: "ITEM_B"})

	fresh := NewRegistry()
	fresh.Add("held", Item{Codename: "ITEM_OLD"})
	before := fresh.DivisionRevision("held")
	fresh.Restore(source.Snapshot())
	if fresh.DivisionRevision("held") == before {
		t.Fatal("restore left the surviving division's stamp unchanged")
	}
	if fresh.DivisionRevision("restored") == 0 {
		t.Fatal("restore left the brought division unstamped")
	}
	if fresh.DivisionRevision("held") != fresh.Revision() ||
		fresh.DivisionRevision("restored") != fresh.Revision() {
		t.Fatal("restore did not stamp every division with the new global counter")
	}
}

/*
================
BenchmarkRegistryReadCosts

The three reads over one live-sized division (1,600 drops, 25 of
them one dungeon's): the deep copy every reader used to pay, the
population filter under the lock, and the codename walk.
================
*/
func BenchmarkRegistryReadCosts(b *testing.B) {
	dungeon := Population{0x1000a, 2}
	seed := func() *Registry {
		r := NewRegistry()
		for i := 0; i < 1600; i++ {
			item := Item{RefObjID: 3117, StackCount: 1, Codename: "ITEM_ETC_HP_POTION_01",
				MagicOptions: []uint64{uint64(i)}, DroppedBy: "hunter"}
			if i%64 == 0 {
				item.Population = dungeon
			}
			r.Add("global-official", item)
		}
		return r
	}

	b.Run("All", func(b *testing.B) {
		r := seed()
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			_ = r.All("global-official")
		}
	})
	b.Run("AllInPopulation", func(b *testing.B) {
		r := seed()
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			_ = r.AllInPopulation("global-official", dungeon)
		}
	})
	b.Run("Codenames", func(b *testing.B) {
		r := seed()
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			_ = r.Codenames("global-official")
		}
	})
}
