package grounditem

import (
	"testing"
	"time"
)

func TestSetStackCountRewritesInPlace(t *testing.T) {
	registry := NewRegistry()
	added := registry.Add("1", Item{RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 20})

	if !registry.SetStackCount("1", added.Gid, 5) {
		t.Fatal("SetStackCount reported a missing entry")
	}
	got, ok := registry.Get("1", added.Gid)
	if !ok {
		t.Fatal("the entry vanished")
	}
	if got.StackCount != 5 {
		t.Fatalf("stack count = %d, want 5", got.StackCount)
	}
	if got.Gid != added.Gid {
		t.Fatalf("gid changed: %d -> %d", added.Gid, got.Gid)
	}

	if registry.SetStackCount("1", added.Gid+1, 5) {
		t.Fatal("SetStackCount succeeded for a gid that does not exist")
	}
	if registry.SetStackCount("2", added.Gid, 5) {
		t.Fatal("SetStackCount succeeded in the wrong division")
	}
}

func TestExpireItemsSweepsOnlyElapsedEntries(t *testing.T) {
	registry := NewRegistry()
	now := time.Unix(10_000, 0)

	stale := registry.Add("1", Item{RefObjID: 1, DroppedAt: now.Add(-FixtureLifetime)})
	staler := registry.Add("1", Item{RefObjID: 2, DroppedAt: now.Add(-FixtureLifetime - time.Minute)})
	fresh := registry.Add("1", Item{RefObjID: 3, DroppedAt: now.Add(-FixtureLifetime + time.Second)})
	// No timestamp: never expires, matching the fixture sweep's skip.
	untimed := registry.Add("1", Item{RefObjID: 4})
	otherDivision := registry.Add("2", Item{RefObjID: 5, DroppedAt: now.Add(-FixtureLifetime)})

	expired := registry.ExpireItems("1", now, FixtureLifetime)
	if len(expired) != 2 {
		t.Fatalf("expired %d entries, want 2", len(expired))
	}
	// Ordered by gid so the despawn burst is deterministic; stale was added
	// first and so carries the lower gid.
	if expired[0].Gid != stale.Gid || expired[1].Gid != staler.Gid {
		t.Fatalf("expired gids = %d, %d; want %d then %d", expired[0].Gid, expired[1].Gid, stale.Gid, staler.Gid)
	}

	if _, ok := registry.Get("1", fresh.Gid); !ok {
		t.Fatal("a fresh entry was swept")
	}
	if _, ok := registry.Get("1", untimed.Gid); !ok {
		t.Fatal("an entry without a timestamp was swept; it must never expire")
	}
	if _, ok := registry.Get("2", otherDivision.Gid); !ok {
		t.Fatal("the sweep crossed divisions")
	}

	// The boundary is inclusive: exactly-elapsed entries sweep (stale above
	// sat exactly at the lifetime).
	if len(registry.ExpireItems("1", now, FixtureLifetime)) != 0 {
		t.Fatal("a second sweep found something new")
	}
}

func TestDivisionIDs(t *testing.T) {
	registry := NewRegistry()
	if got := registry.DivisionIDs(); len(got) != 0 {
		t.Fatalf("an empty registry reported divisions %v", got)
	}

	registry.Add("beta", Item{RefObjID: 1})
	registry.Add("alpha", Item{RefObjID: 2})
	only := registry.Add("emptied", Item{RefObjID: 3})
	registry.Remove("emptied", only.Gid)

	got := registry.DivisionIDs()
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("divisions = %v, want [alpha beta] sorted with the emptied one skipped", got)
	}
}

func TestStackCountSurvivesAddAndRemove(t *testing.T) {
	registry := NewRegistry()
	added := registry.Add("1", Item{RefObjID: 3630, StackCount: 7, DroppedAt: time.Unix(5000, 0)})

	removed, ok := registry.Remove("1", added.Gid)
	if !ok {
		t.Fatal("the entry was not removable")
	}
	if removed.StackCount != 7 {
		t.Fatalf("removed stack count = %d, want 7", removed.StackCount)
	}
	if !removed.DroppedAt.Equal(time.Unix(5000, 0)) {
		t.Fatalf("removed DroppedAt = %v, want the stamp it was added with", removed.DroppedAt)
	}
}

/*
================
TestRegistryHasExpired

The read-only peek agrees with the sweep it gates: an entry at exactly
its lifetime counts, one second short does not, and a drop without a
timestamp never does. The peek must also leave every record in place.
================
*/
func TestRegistryHasExpired(t *testing.T) {
	registry := NewRegistry()
	now := time.Unix(10_000, 0)

	if registry.HasExpired("1", now, FixtureLifetime) {
		t.Fatal("an empty division reported an expired entry")
	}
	registry.Add("1", Item{RefObjID: 4})
	if registry.HasExpired("1", now, FixtureLifetime) {
		t.Fatal("an entry without a timestamp reported expired")
	}
	fresh := registry.Add("1", Item{RefObjID: 3, DroppedAt: now.Add(-FixtureLifetime + time.Second)})
	if registry.HasExpired("1", now, FixtureLifetime) {
		t.Fatal("an entry one second short of its lifetime reported expired")
	}
	registry.Add("1", Item{RefObjID: 1, DroppedAt: now.Add(-FixtureLifetime)})
	if !registry.HasExpired("1", now, FixtureLifetime) {
		t.Fatal("an entry exactly at its lifetime did not report expired")
	}
	// The peek is read-only: the fresh entry is still swept by the real pass.
	expired := registry.ExpireItems("1", now, FixtureLifetime)
	if len(expired) != 1 || expired[0].RefObjID != 1 {
		t.Fatalf("sweep after the peek expired %v, want only the elapsed entry", expired)
	}
	if _, ok := registry.Get("1", fresh.Gid); !ok {
		t.Fatal("the peek disturbed a fresh entry")
	}
	if registry.HasExpired("1", now, FixtureLifetime) {
		t.Fatal("the peek still reports expired after the sweep took the entry")
	}
}
