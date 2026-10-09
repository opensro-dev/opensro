/*
===========================================================================

global-chat.ts - the Global Chatting item's message on the item-use wire

CIFWholeChat (CIFGlobalChatItem_OnSend 6D1EE0) sends its line inside the
item's use: 0x75BD {u8 slot, u16 type word, u16 count + UTF-16LE text}
(CGInterface_SendGlobalChatItemUse 693C90). The server spends the item and
relays the line to every player as chat type 6.

===========================================================================
*/

// CIFWholeChat_OnCreate (6D2080): CIFEdit_SetMaximumTextLength( edit, 0x64 ).
export const GLOBAL_CHAT_MAX_LENGTH = 0x64;

/*
================
isGlobalChatItem

693C90's type test: an etc item (3/3) of type 3, type 4 = 5.
================
*/
export function isGlobalChatItem( typeFlags: number ) {
	return (typeFlags & 2) === 0 && (typeFlags & 0x1c) === 0xc && (typeFlags & 0x60) === 0x60 &&
		(typeFlags & 0x780) === 0x180 && (typeFlags & 0xf800) === 0x2800;
}

/*
================
globalChatTail

The message after the slot and type word: its UTF-16 length, then its
code units little-endian (CMsgStreamBuffer_WriteWideString).
================
*/
export function globalChatTail( message: string ): Uint8Array {
	const tail = new Uint8Array( 2 + message.length * 2 ), view = new DataView( tail.buffer );
	view.setUint16( 0, message.length, true );
	for ( let i = 0; i < message.length; i++ ) view.setUint16( 2 + i * 2, message.charCodeAt( i ), true );
	return tail;
}
