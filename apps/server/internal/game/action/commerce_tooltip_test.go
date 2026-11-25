package action

import (
	"encoding/json"
	"opensro.online/server/internal/game/item/wire"
	"testing"
)

// A purchase must publish its tooltip reference before publishing the item
// instance, including items absent from the login equipment snapshot.
func TestCommercePublishesTooltipReferenceBeforeInstance(t *testing.T) {
	rt, c := merchantFixture(t)
	result := trade(t, rt, c, wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 1})
	if len(result.Frames) < 2 || result.Frames[0].Opcode != opCommerceItemReferences {
		t.Fatal("reference must precede purchased instance")
	}
	var decoded struct {
		Items []struct {
			ID     uint32             `json:"refObjId"`
			Fields map[string]float64 `json:"nativeFields"`
		} `json:"items"`
	}
	if err := json.Unmarshal(result.Frames[0].Payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Items) != 1 || decoded.Items[0].ID != 3630 || decoded.Items[0].Fields["maxStack"] != 50 {
		t.Fatalf("missing native tooltip fields: %+v", decoded)
	}
}
