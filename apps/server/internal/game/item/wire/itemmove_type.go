/*
===========================================================================

itemmove_type.go - the item move's kind byte, readable on its own

A door that must know the kind of move before the whole request is
read (the trade restriction's drop gate) peeks the movement type
here; the full decode stays DecodeItemMoveRequest's in itemmove.go.

===========================================================================
*/

package wire

/*
================
ItemMoveType

The request's movement type alone: the first byte, when there is one.
================
*/
func ItemMoveType(payload []byte) (uint8, bool) {
	if len(payload) == 0 {
		return 0, false
	}
	return payload[0], true
}
