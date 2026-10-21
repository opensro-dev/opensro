/*
===========================================================================

peer_reference_catalog_test.go - prepared peer metadata needs no live catalogue

Freeze the public rows during composition, then make the source unavailable.
Spawn references must stay byte-identical, bounded and detached from callers.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/json"
	"fmt"
	"opensro.online/server/internal/testsupport/tier"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
countedPeerItems

The counter includes both ID and codename lookups used by the wire encoder.
================
*/
type countedPeerItems struct {
	staticItemSource
	reads int
}

/*
================
ItemRefByID
================
*/
func (s *countedPeerItems) ItemRefByID(id uint32) (*enterworld.ItemRef, bool) {
	s.reads++
	return s.staticItemSource.ItemRefByID(id)
}

/*
================
ItemRefByCodename
================
*/
func (s *countedPeerItems) ItemRefByCodename(name string) (*enterworld.ItemRef, bool) {
	s.reads++
	return s.staticItemSource.ItemRefByCodename(name)
}

/*
================
TestPreparedPeerReferencesNeverReadTheSource
================
*/
func TestPreparedPeerReferencesNeverReadTheSource(t *testing.T) {
	source := &countedPeerItems{staticItemSource: testItems()}
	rt, _ := newTestRuntime(testCharacter(), source)
	var commands []enterworld.ItemCommandReference
	var ids []uint32
	for i := range maxReferencesPerFrame + 1 {
		id := uint32(100000 + i)
		name := fmt.Sprintf("REVIEW_ITEM_%d", id)
		source.staticItemSource[name] = &enterworld.ItemRef{
			RefObjID: id, Codename: name, Name: "Sword \"<rare>\"", TypeIDs: [4]int64{3, 1, 6, 2},
			NativeFields: enterworld.NewNativeFields(map[string]float64{"maxDurability": 80, "itemParam1_29c": 0}),
		}
		commands = append(commands, enterworld.ItemCommandReference{RefObjID: id})
		ids = append(ids, id)
	}
	ids = append(ids, ids[0], 0, 0xfffffff0)
	want := rt.PeerItemReferences(ids)
	catalog, err := rt.PreparePeerItemReferences(commands)
	if err != nil {
		t.Fatal(err)
	}
	source.reads = 0
	source.staticItemSource = nil
	got := catalog.Frames(ids)
	if source.reads != 0 || len(got) != len(want) || len(got) != 2 {
		t.Fatalf("reads=%d, frames=%d, want no reads and two frames", source.reads, len(got))
	}
	for i := range want {
		if got[i].Opcode != want[i].Opcode || !bytes.Equal(got[i].Payload, want[i].Payload) {
			t.Fatalf("frame %d changed the reference wire contract", i)
		}
	}
	got[0].Payload[0] = 0
	if !bytes.Equal(catalog.Frames(ids)[0].Payload, want[0].Payload) {
		t.Fatal("a caller changed the retained reference rows")
	}
}

/*
================
TestPreparedPeerReferencesRejectIncompleteCatalogues
================
*/
func TestPreparedPeerReferencesRejectIncompleteCatalogues(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	for _, ids := range [][]uint32{{0}, {0xfffffff0}, {11459, 11459}} {
		var commands []enterworld.ItemCommandReference
		for _, id := range ids {
			commands = append(commands, enterworld.ItemCommandReference{RefObjID: id})
		}
		if _, err := rt.PreparePeerItemReferences(commands); err == nil {
			t.Fatalf("accepted incomplete or ambiguous catalogue %v", ids)
		}
	}
}

/*
================
TestPreparedPeerReferencesCoverTheShippedCatalogue

Exercise the production startup projection against the bounded disk-backed
source. No authored ID may disappear, and the source can close before ticks.
================
*/
func TestPreparedPeerReferencesCoverTheShippedCatalogue(t *testing.T) {
	tier.Long(t, "peer references cover the whole shipped catalogue")
	source := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	if err := source.UseBoundedCache(512); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	commands := source.ItemCommandReferences()
	if len(commands) == 0 {
		t.Fatal("shipped item catalogue is empty")
	}
	rt, _ := newTestRuntime(testCharacter(), source)
	catalog, err := rt.PreparePeerItemReferences(commands)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.rows) != len(commands) {
		t.Fatalf("prepared %d rows, want %d", len(catalog.rows), len(commands))
	}
	var encodedBytes int
	for _, row := range catalog.rows {
		encodedBytes += len(row)
	}
	// Close releases the backing file and resident pages; cleanup is idempotent.
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	ids := make([]uint32, 0, len(commands))
	for _, command := range commands {
		ids = append(ids, command.RefObjID)
	}
	frames := catalog.Frames(ids)
	wantFrames := (len(ids) + maxReferencesPerFrame - 1) / maxReferencesPerFrame
	if len(frames) != wantFrames {
		t.Fatalf("closed-source frames=%d, want %d", len(frames), wantFrames)
	}
	seen := make(map[uint32]bool, len(ids))
	var delivered int
	for i, frame := range frames {
		if frame.Opcode != opCommerceItemReferences {
			t.Fatalf("frame %d opcode=%#x, want %#x", i, frame.Opcode, opCommerceItemReferences)
		}
		var delta struct {
			Version int `json:"version"`
			Items   []struct {
				ID uint32 `json:"refObjId"`
			} `json:"items"`
		}
		if err := json.Unmarshal(frame.Payload, &delta); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if delta.Version != 1 || len(delta.Items) == 0 || len(delta.Items) > maxReferencesPerFrame {
			t.Fatalf("frame %d version=%d rows=%d", i, delta.Version, len(delta.Items))
		}
		for _, row := range delta.Items {
			if delivered >= len(ids) || row.ID != ids[delivered] || seen[row.ID] {
				t.Fatalf("unexpected or duplicate ID %d at delivered row %d", row.ID, delivered)
			}
			seen[row.ID] = true
			delivered++
		}
	}
	if delivered != len(commands) {
		t.Fatalf("closed-source rows=%d, want %d", delivered, len(commands))
	}
	t.Logf("prepared %d public item rows, %d encoded bytes", len(catalog.rows), encodedBytes)
}
