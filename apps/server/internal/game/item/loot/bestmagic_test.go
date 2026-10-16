package loot

import "testing"

/*
================
TestBestEquipmentMagicIsCatalogValid

Every option BestEquipmentMagic returns must come from the item's own pool
(the same pool EquipmentMagic rolls from) and must not exceed the item's
MaxMagic. A caller can therefore trust the param ids resolve in the client.
================
*/
func TestBestEquipmentMagicIsCatalogValid(t *testing.T) {
	code := ""
	for name, pool := range properties.pools {
		if len(pool) > 0 && properties.Items[name].MaxMagic > 0 {
			code = name
			break
		}
	}
	if code == "" {
		t.Skip("no catalog pool")
	}
	item := properties.Items[code]
	options := BestEquipmentMagic(code)
	if len(options) == 0 {
		t.Fatalf("BestEquipmentMagic(%s) returned nothing", code)
	}
	if len(options) > item.MaxMagic {
		t.Fatalf("BestEquipmentMagic(%s) returned %d options, cap %d", code, len(options), item.MaxMagic)
	}
	inPool := func(id uint16) bool {
		for _, option := range properties.pools[code] {
			if option.ID == id {
				return true
			}
		}
		return false
	}
	seen := make(map[uint16]bool, len(options))
	for _, encoded := range options {
		id := uint16(encoded)
		if seen[id] {
			t.Fatalf("option id %d appears twice for %s", id, code)
		}
		seen[id] = true
		if !inPool(id) {
			t.Fatalf("option id %d is not in the pool for %s", id, code)
		}
	}
}
