package grounditem

import (
	"reflect"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

// rebootedRegistry simulates the watchdog reboot at the DOMAIN level: a
// FRESH registry hydrated from the snapshot alone. Nothing in-process
// survives into it, so a stale snapshot fails the caller's assertions
// (XDROP-5 1445 anti-vacuous bar). The FILE half of the reboot - reading
// the authority state - is the store package's territory and is
// witnessed there (TestGroundTTLContinuityThroughStore, torn-write and
// corruption ladders).
func rebootedRegistry(snapshot domain.GroundSnapshot) *Registry {
	registry := NewRegistry()
	registry.Restore(snapshot)
	return registry
}

// (a) Snapshot -> Restore roundtrip preserves items, positions,
// timestamps and the gid counter across a simulated process death.
func TestGroundStateRoundTripSurvivesReboot(t *testing.T) {
	registry := NewRegistry()

	droppedAt := time.UnixMilli(1_785_000_000_000)
	sword := registry.Add("global-official", Item{
		RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE", TypeFlags: 0x08AC,
		Plus: 5, VarianceBits: 0x8000000000000000, Durability: 96, StackCount: 1,
		Position: Point{RegionID: 0x62A8, X: 1010, Z: 458}, Y: 20, Heading: 300,
		OwnerJID: 100003, DroppedBy: "asd2", DroppedAt: droppedAt,
	})
	potions := registry.Add("global-official", Item{
		RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: 0x08EC,
		StackCount: 42, Position: Point{RegionID: 0x62A8, X: 1000, Z: 450}, Y: 20,
		DroppedBy: "asd2", DroppedAt: droppedAt.Add(30 * time.Second),
	})
	gold := registry.Add("elsewhere", Item{
		RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", TypeFlags: 0x2EC, GoldAmount: 1500,
		Position: Point{RegionID: 0x62A9, X: 200, Z: 300}, Y: 10,
		DroppedBy: "asd2", DroppedAt: droppedAt,
	})
	// A pre-reboot pickup: the snapshot must track the removal too, or the
	// reboot resurrects already-picked items.
	picked := registry.Add("global-official", Item{RefObjID: 999, TypeFlags: 0x08AC})
	if _, ok := registry.Remove("global-official", picked.Gid); !ok {
		t.Fatal("seed removal failed")
	}

	restored := rebootedRegistry(registry.Snapshot())

	if !reflect.DeepEqual(registry.Snapshot(), restored.Snapshot()) {
		t.Fatalf("post-reboot snapshot diverges:\nlive:     %+v\nrestored: %+v",
			registry.Snapshot(), restored.Snapshot())
	}
	got, ok := restored.Get("global-official", sword.Gid)
	if !ok {
		t.Fatalf("sword gid %d missing after reboot", sword.Gid)
	}
	if !got.DroppedAt.Equal(droppedAt) {
		t.Fatalf("sword DroppedAt = %v, want the ORIGINAL %v", got.DroppedAt, droppedAt)
	}
	if got.VarianceBits != 0x8000000000000000 || got.Plus != 5 || got.Durability != 96 {
		t.Fatalf("sword identity fields diverged: %+v", got)
	}
	if got.OwnerJID != 100003 {
		t.Fatalf("sword owner JID = %d, want 100003 after reboot", got.OwnerJID)
	}
	if got.Position != sword.Position || got.Y != sword.Y || got.Heading != sword.Heading {
		t.Fatalf("sword placement diverged: %+v vs %+v", got.Position, sword.Position)
	}
	if heap, ok := restored.Get("global-official", potions.Gid); !ok || heap.StackCount != 42 {
		t.Fatalf("potion heap after reboot = %+v (%v), want stack 42", heap, ok)
	}
	if heap, ok := restored.Get("elsewhere", gold.Gid); !ok || heap.GoldAmount != 1500 {
		t.Fatalf("gold heap after reboot = %+v (%v), want 1500 gold in its own division", heap, ok)
	}
	if restored.Count("global-official") != 2 {
		t.Fatalf("division count = %d, want 2 (the picked item must not resurrect)",
			restored.Count("global-official"))
	}

	// The gid counter survives the reboot: the next drop allocates ABOVE
	// every gid the old process ever handed out, so a client still
	// rendering an old drop can never see its gid reused.
	next := restored.Add("global-official", Item{RefObjID: 1})
	if next.Gid != picked.Gid+1 {
		t.Fatalf("post-reboot gid = %d, want %d (counter continued past the last allocation)",
			next.Gid, picked.Gid+1)
	}
}

// (b) TTL continuity: an item dropped ~170s before the reap must expire
// ~10s after the reboot - the ORIGINAL timestamp drives the fixture TTL as
// if the reap never happened, on the exact Node-parity inclusive boundary.
//
// WITNESSED RED: this test failed while itemFromPersisted dropped the
// timestamp (rehydrated items came back never-expiring); the DroppedAtMs
// rehydration is what closed it.
func TestGroundStateTTLContinuesAcrossReboot(t *testing.T) {
	registry := NewRegistry()

	droppedAt := time.UnixMilli(2_000_000_000)
	timed := registry.Add("global-official", Item{RefObjID: 11459, TypeFlags: 0x08AC, DroppedAt: droppedAt})
	untimed := registry.Add("global-official", Item{RefObjID: 3630, TypeFlags: 0x08EC}) // no timestamp: never expires

	// ...the OS reap and the watchdog reboot happen mid-lifetime...
	restored := rebootedRegistry(registry.Snapshot())

	// 1ms shy of the ORIGINAL drop's lifetime: still alive.
	if expired := restored.ExpireItems("global-official", droppedAt.Add(FixtureLifetime-time.Millisecond), FixtureLifetime); len(expired) != 0 {
		t.Fatalf("expired %+v before the lifetime elapsed - the reboot must not reset the clock", expired)
	}
	// Exactly the lifetime since the ORIGINAL drop: reaps.
	expired := restored.ExpireItems("global-official", droppedAt.Add(FixtureLifetime), FixtureLifetime)
	if len(expired) != 1 || expired[0].Gid != timed.Gid {
		t.Fatalf("expired = %+v, want exactly the timed drop %d - rehydration must keep DroppedAt", expired, timed.Gid)
	}
	if _, ok := restored.Get("global-official", untimed.Gid); !ok {
		t.Fatal("the never-expiring drop was reaped after the reboot")
	}
}

// (c) An empty snapshot is a clean first boot: empty store, gid band
// starts fresh.
func TestRestoreEmptySnapshotIsCleanFirstBoot(t *testing.T) {
	registry := rebootedRegistry(domain.GroundSnapshot{Version: domain.GroundSnapshotVersion})
	if got := registry.Count("global-official"); got != 0 {
		t.Fatalf("first-boot store holds %d entries, want 0", got)
	}
	if first := registry.Add("global-official", Item{RefObjID: 1}); first.Gid != GidBase+1 {
		t.Fatalf("first gid = %d, want %d", first.Gid, GidBase+1)
	}
}

// A stale or hand-edited counter must never resurrect gid reuse: Restore
// bumps the counter past every restored gid.
func TestRestoreGuardsCounterAgainstStaleValue(t *testing.T) {
	registry := NewRegistry()
	registry.Restore(domain.GroundSnapshot{Version: domain.GroundSnapshotVersion, GidCounter: 0, Divisions: map[string][]domain.GroundItemRecord{
		"global-official": {{Gid: GidBase + 7, RefObjID: 1, TypeFlags: 0x08AC, RegionID: 1}},
	}})

	next := registry.Add("global-official", Item{RefObjID: 2})
	if next.Gid != GidBase+8 {
		t.Fatalf("gid after a guarded restore = %d, want %d (past the restored gid)", next.Gid, GidBase+8)
	}
}

// The beta server exhausted the band in one process lifetime (gidCounter
// 99999) and every later drop was refused: the cursor wraps instead, past
// any id a division still holds.
func TestAddWrapsPastLiveIdsAtTheBandEnd(t *testing.T) {
	registry := NewRegistry()
	registry.Restore(domain.GroundSnapshot{
		Version:    domain.GroundSnapshotVersion,
		GidCounter: domain.MaxGroundItemGIDCounter,
		Divisions: map[string][]domain.GroundItemRecord{
			"global-official": {{Gid: GidBase + 1, RefObjID: 1, TypeFlags: 0x08AC, RegionID: 1}},
			"other":           {{Gid: GidBase + 2, RefObjID: 1, TypeFlags: 0x08AC, RegionID: 1}},
		},
	})
	added := registry.Add("global-official", Item{RefObjID: 3})
	if added.Gid != GidBase+3 {
		t.Fatalf("wrapped gid = %d, want %d (past the ids both divisions still hold)", added.Gid, GidBase+3)
	}
	if snapshot := registry.Snapshot(); snapshot.GidCounter != 3 {
		t.Fatalf("cursor = %d, want 3", snapshot.GidCounter)
	}
}

func TestAddRefusesABandFullOfLiveDrops(t *testing.T) {
	registry := NewRegistry()
	for range domain.MaxGroundItemGIDCounter {
		if added := registry.Add("global-official", Item{RefObjID: 1}); added.Gid == 0 {
			t.Fatal("a free id was refused")
		}
	}
	if added := registry.Add("global-official", Item{RefObjID: 1}); added.Gid != 0 {
		t.Fatalf("a full band allocated gid %d, which a live drop already holds", added.Gid)
	}
	removed := registry.All("global-official")[0]
	registry.Remove("global-official", removed.Gid)
	if added := registry.Add("global-official", Item{RefObjID: 1}); added.Gid != removed.Gid {
		t.Fatalf("gid = %d, want the freed %d", added.Gid, removed.Gid)
	}
}
