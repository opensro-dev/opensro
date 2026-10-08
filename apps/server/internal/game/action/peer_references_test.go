/*
===========================================================================

peer_references_test.go - tests for peer_references.go

===========================================================================
*/
package action

import (
	"encoding/json"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestPeerItemReferencesResolveWornIDs

The spawn-row hook turns bare ids into one reference row each: empty ids,
repeats and ids the item table does not know are left out.
================
*/
func TestPeerItemReferencesResolveWornIDs(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	frames := rt.PeerItemReferences([]uint32{11459, 0, 11459, 0xfffffff0})
	if len(frames) != 1 || frames[0].Opcode != opCommerceItemReferences {
		t.Fatalf("frames = %+v, want one item reference delta", frames)
	}
	var delta struct {
		Items []struct {
			ID   uint32 `json:"refObjId"`
			Type uint16 `json:"typeFlags"`
		} `json:"items"`
	}
	if err := json.Unmarshal(frames[0].Payload, &delta); err != nil {
		t.Fatal(err)
	}
	if len(delta.Items) != 1 || delta.Items[0].ID != 11459 || delta.Items[0].Type != wire.PackTypeFlags(3, 1, 6, 2) {
		t.Fatalf("rows = %+v, want the sword's row alone", delta.Items)
	}
	if frames := rt.PeerItemReferences(nil); len(frames) != 0 {
		t.Fatalf("no ids sent %d frames", len(frames))
	}
}
