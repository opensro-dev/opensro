/*
===========================================================================
arenachest_test.go - pending rewards must not alias transaction snapshots.
===========================================================================
*/
package domain

import "testing"

/*
================
TestArenaChestClone
================
*/
func TestArenaChestClone(t *testing.T) {
	c := &Character{SookChest: &ArenaChest{ID: "test", Items: []InventoryRow{{RefObjID: 1, MagicOptions: []uint64{2}}}}}
	clone := CloneCharacter(c)
	clone.SookChest.Items[0].MagicOptions[0] = 3
	clone.SookChest.Stage = 4
	if c.SookChest.Stage != 0 || c.SookChest.Items[0].MagicOptions[0] != 2 {
		t.Fatal("chest leaked out of its transaction")
	}
}
