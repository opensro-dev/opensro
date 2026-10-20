/*
===========================================================================

sunrate_test.go - the drop tables publish the Sun rate's widened odds

Port-only, not native (progression/sunrate.go): while SRO_SUN_DROP_RATE
is on, the beta object carries the rate and each in-scope monster
table's rare rows are the composed (Sun-widened) ones the roll uses;
off, the rate publishes as 0 and no table carries a widened row.

===========================================================================
*/
package catalogue

import (
	"testing"

	"opensro.online/server/internal/game/progression"
)

/*
================
TestDropTablesPublishTheSunRate
================
*/
func TestDropTablesPublishTheSunRate(t *testing.T) {
	monsters := []Monster{{Level: 80, Country: 0, Table: equipmentTableKey(80, 0)}}
	ids := map[string]uint32{}
	itemID := func(codename string) uint32 {
		if id, ok := ids[codename]; ok {
			return id
		}
		id := uint32(len(ids) + 1)
		ids[codename] = id
		return id
	}
	sunID := itemID("ITEM_CH_SWORD_09_C_RARE")
	chanceOf := func(rows []DropChance) float64 {
		for _, row := range rows {
			if row.ItemID == sunID {
				return row.Chance
			}
		}
		return 0
	}

	t.Setenv(progression.EnvSunDropRate, "2")
	file := BuildDropTables(monsters, itemID, Header{})
	if file.Beta == nil || file.Beta.SunDropRate != 2 {
		t.Fatalf("rate 2: beta %+v, want sunDropRate 2", file.Beta)
	}
	native := chanceOf(file.Equipment["80/0"].Rare)
	widened := chanceOf(file.Beta.Rare["80/0"])
	if native <= 0 || widened <= native {
		t.Fatalf("rate 2: Sun chance native %v widened %v, want widened > native > 0", native, widened)
	}

	t.Setenv(progression.EnvSunDropRate, "")
	off := BuildDropTables(monsters, itemID, Header{})
	if off.Beta != nil && off.Beta.SunDropRate != 0 {
		t.Fatalf("rate off: sunDropRate %d, want 0", off.Beta.SunDropRate)
	}
	if off.Beta != nil {
		if got := chanceOf(off.Beta.Rare["80/0"]); got != 0 && got != native {
			t.Fatalf("rate off: widened row published with Sun chance %v", got)
		}
	}
}
