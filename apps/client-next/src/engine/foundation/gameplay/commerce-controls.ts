/*
===========================================================================

commerce-controls.ts - the replacement transport's world control frames

Browser-only frames in the transport control range (below 256) that carry
state native packets cannot: commerce, and the beta public chat
transcript. The server defines the same numbers (internal/game/action:
commerce.go, buyback.go, commerce_references.go, itemmall.go;
internal/game/social/chat: wire.go). The world session admits exactly
these after EnterWorld; a frame the gate does not know ends the session,
which is how the Item Mall catalogue disconnected every client (F10).

===========================================================================
*/

export const SHOP_CATALOG_CONTROL = 11;
export const SHOP_INVENTORY_CONTROL = 12;
export const SHOP_BUYBACK_CONTROL = 13;
export const COMMERCE_REFERENCES_CONTROL = 14;
export const MALL_CATALOG_CONTROL = 15;
// The server's replayed public transcript (chat/history.go OpChatHistory).
export const CHAT_HISTORY_CONTROL = 16;

/*
================
isWorldControl
================
*/
export function isWorldControl( opcode: number ): boolean {
	return opcode === SHOP_CATALOG_CONTROL || opcode === SHOP_INVENTORY_CONTROL ||
		opcode === SHOP_BUYBACK_CONTROL || opcode === COMMERCE_REFERENCES_CONTROL || opcode === MALL_CATALOG_CONTROL ||
		opcode === CHAT_HISTORY_CONTROL;
}
