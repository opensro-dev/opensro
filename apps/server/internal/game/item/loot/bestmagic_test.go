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

/*
================
TestBestEquipmentMagicSkipsAlchemyAids

A perfect combat option set must not spend reinforcement aids. Any option
whose tag is Lucky, Immortal, Steady or Astral is skipped even when the pool
offers it.
================
*/
func TestBestEquipmentMagicSkipsAlchemyAids(t *testing.T) {
	checked := 0
	for code, pool := range properties.pools {
		if len(pool) == 0 || properties.Items[code].MaxMagic <= 0 {
			continue
		}
		aids := 0
		for _, option := range pool {
			if alchemyAidTags[option.Tag] {
				aids++
			}
		}
		if aids == 0 {
			continue
		}
		checked++
		for _, encoded := range BestEquipmentMagic(code) {
			id := uint16(encoded)
			for _, option := range pool {
				if option.ID == id && alchemyAidTags[option.Tag] {
					t.Fatalf("%s: perfect set carries alchemy aid %s", code, option.Name)
				}
			}
		}
	}
	if checked == 0 {
		t.Skip("the catalog pools carry no alchemy aid to check")
	}
}
