/*
===========================================================================

registry_generation_test.go - the registry write count behind snapshot reuse

A caller holding a snapshot skips the next copy while the generation is
unchanged, so every write path must advance it and no read path may.

===========================================================================
*/
package skillobject

import (
	"testing"
)

/*
================
TestRegistryGenerationCountsWrites

================
*/
func TestRegistryGenerationCountsWrites(t *testing.T) {
	var registry Registry
	object := trapFixture(t, &registry)
	gid := object.Spawn.GID
	if got := registry.Generation(); got != 1 {
		t.Fatalf("Create left generation %d, want 1", got)
	}
	_ = registry.Snapshot()
	if got := registry.Generation(); got != 1 {
		t.Fatalf("Snapshot advanced generation to %d", got)
	}
	if _, _, retired := registry.Scan(gid, 1299, true, nil); retired || registry.Generation() != 1 {
		t.Fatalf("quiet scan wrote: retired=%v generation=%d", retired, registry.Generation())
	}
	if _, _, retired := registry.Scan(gid, 1300, true, nil); retired || registry.Generation() != 2 {
		t.Fatalf("due scan restamp did not count: retired=%v generation=%d", retired, registry.Generation())
	}
	if _, due, retired := registry.Advance(gid, 1599, true); due || retired || registry.Generation() != 2 {
		t.Fatalf("quiet advance wrote: due=%v retired=%v generation=%d", due, retired, registry.Generation())
	}
	if !registry.Track(gid, []FieldRecipient{{Name: "Trapper", GID: 100001}}) || registry.Generation() != 3 {
		t.Fatalf("Track did not count: generation=%d", registry.Generation())
	}
	if !registry.Pulsed(gid, 2000) || registry.Generation() != 4 {
		t.Fatalf("Pulsed did not count: generation=%d", registry.Generation())
	}
	if registry.Track(999999, nil) || registry.Pulsed(999999, 2000) || registry.Generation() != 4 {
		t.Fatalf("absent-object writes counted: generation=%d", registry.Generation())
	}
	if registry.Remove(999999) || registry.Generation() != 4 {
		t.Fatalf("absent remove counted: generation=%d", registry.Generation())
	}
	if !registry.Remove(gid) || registry.Generation() != 5 {
		t.Fatalf("Remove did not count: generation=%d", registry.Generation())
	}
	second := trapFixture(t, &registry)
	if _, _, retired := registry.Scan(second.Spawn.GID, 1601, true, nil); !retired || registry.Generation() != 7 {
		t.Fatalf("expiry retire did not count: retired=%v generation=%d", retired, registry.Generation())
	}
}
