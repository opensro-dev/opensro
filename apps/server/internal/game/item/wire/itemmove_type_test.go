/*
===========================================================================

itemmove_type_test.go - the move-kind peek the drop gate reads

===========================================================================
*/

package wire

import "testing"

/*
================
TestItemMoveTypePeeksTheKind

The movement type is the request's first byte; an empty request has
none. Every kind round-trips, readable or not.
================
*/
func TestItemMoveTypePeeksTheKind(t *testing.T) {
	if _, ok := ItemMoveType(nil); ok {
		t.Fatal("an empty request reported a move kind")
	}
	for _, kind := range []uint8{MoveTypeGroundDrop, MoveTypeGoldDrop, 0, 200} {
		got, ok := ItemMoveType([]byte{kind, 9, 9})
		if !ok || got != kind {
			t.Fatalf("ItemMoveType(%d) = %d/%v", kind, got, ok)
		}
	}
}
