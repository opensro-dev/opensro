/*
===========================================================================

commerce-controls.ts - the replacement transport's commerce control frames

Browser-only frames in the transport control range (below 256) that carry
structured commerce state native packets cannot. The server defines the
same numbers (internal/game/action: commerce.go, buyback.go,
commerce_references.go, itemmall.go). The world session admits exactly
these after EnterWorld; a frame the gate does not know ends the session,
which is how the Item Mall catalogue disconnected every client (F10).

===========================================================================
*/

export const SHOP_CATALOG_CONTROL = 11;
export const SHOP_INVENTORY_CONTROL = 12;
export const SHOP_BUYBACK_CONTROL = 13;
export const COMMERCE_REFERENCES_CONTROL = 14;
export const MALL_CATALOG_CONTROL = 15;

/*
================
isCommerceControl
================
*/
export function isCommerceControl( opcode: number ): boolean {
	return opcode === SHOP_CATALOG_CONTROL || opcode === SHOP_INVENTORY_CONTROL ||
		opcode === SHOP_BUYBACK_CONTROL || opcode === COMMERCE_REFERENCES_CONTROL || opcode === MALL_CATALOG_CONTROL;
}
