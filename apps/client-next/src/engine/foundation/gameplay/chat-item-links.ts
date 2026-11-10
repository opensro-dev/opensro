/*
===========================================================================

chat-item-links.ts - item links in player chat: wire tails and labels

Port-only, not native (SRO_CHAT_ITEM_LINKS, owner decision 2026-11-07).
A chat line's text holds CHAT_LINK_MARKER where a link sits. The sender's
0x7367 (and Global Chatting 0x75BD) adds where each item is; the server
reads the item itself and appends its body after the 0x3667 text (server
chat/itemlinks.go):

	request:   [u8 count] count x {[u8 source][u16 slot][u32 pet gid, pet source only]}
	broadcast: [u8 count] count x {[u16 bodyLen][item body]}

Link i belongs to the i-th marker. The broadcast tail is player-adjacent
data, so it decodes leniently, as notice links do: anything malformed is
no links and a plain line; it never fails the session.

===========================================================================
*/

// U+FFFC, the object replacement character: one UTF-16 unit in the text.
export const CHAT_LINK_MARKER = "\uFFFC";
// Where the sender holds the item (server chat.LinkSource*).
export const CHAT_LINK_SOURCE = { inventory: 0, storage: 1, pet: 2, avatar: 3 } as const;
// The server's wire bound (chat.MaxLinksOnWire, the limit tunable's maximum).
export const CHAT_LINKS_ON_WIRE_MAX = 8;
// An item body is a few dozen bytes (notice-links.ts NOTICE_BODY_MAX).
export const CHAT_LINK_BODY_MAX = 512;
// What a link shows when its item is missing or undecodable.
const UNKNOWN_LINK_LABEL = "<?>";

/*
================
ChatLinkRef

One link as the sender names it: where the item is. A pet-source link
also names WHICH pet (its gid): a grab pet lives on its summon item's
row beside other bagged companions, and two bags can hold an item at
the same slot, so a bare {pet, slot} would name the wrong bag
(protocol 16 carries the gid; the server reads that pet's bag).
================
*/
export interface ChatLinkRef {
	readonly source: number;
	readonly slot: number;
	readonly pet?: number;
}

/*
================
encodeChatLinkRefs

The request link block; no refs is no block at all, so an unlinked line
stays the native frame. A pet-source ref is 7 bytes ([u32 gid] after
the slot), the others 3.
================
*/
export function encodeChatLinkRefs( refs: readonly ChatLinkRef[] ): Uint8Array {
	if ( !refs.length ) return new Uint8Array( 0 );
	let size = 1;
	for ( const ref of refs ) size += ref.source === CHAT_LINK_SOURCE.pet ? 7 : 3;
	const p = new Uint8Array( size );
	const v = new DataView( p.buffer );
	p[0] = refs.length;
	let o = 1;
	for ( const ref of refs ) {
		p[o] = ref.source;
		v.setUint16( o + 1, ref.slot, true );
		o += 3;
		if ( ref.source === CHAT_LINK_SOURCE.pet ) {
			v.setUint32( o, ref.pet ?? 0, true );
			o += 4;
		}
	}
	return p;
}

/*
================
decodeChatLinkTail

The item bodies of a broadcast tail starting at offset: [] when nothing
follows the text, null when the tail is malformed. Never throws.
================
*/
export function decodeChatLinkTail( p: Uint8Array, offset: number ): Uint8Array[] | null {
	if ( offset >= p.length ) return [];
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	const count = p[offset]!;
	if ( count === 0 || count > CHAT_LINKS_ON_WIRE_MAX ) return null;
	const bodies: Uint8Array[] = [];
	let o = offset + 1;
	for ( let i = 0; i < count; i++ ) {
		if ( o + 2 > p.length ) return null;
		const length = v.getUint16( o, true );
		o += 2;
		if ( length === 0 || length > CHAT_LINK_BODY_MAX || o + length > p.length ) return null;
		bodies.push( p.slice( o, o + length ) );
		o += length;
	}
	return o === p.length ? bodies : null;
}

/*
================
chatLinkLabel

"<Name +N>", "<Name>" at +0, "<?>" without an item: the text a link shows
in the input, the log and the server's text consumers alike.
================
*/
export function chatLinkLabel( item: { readonly name?: string; readonly plus?: number; } | undefined ): string {
	if ( !item?.name ) return UNKNOWN_LINK_LABEL;
	return item.plus ? `<${item.name} +${item.plus}>` : `<${item.name}>`;
}

/*
================
markerCount
================
*/
export function markerCount( text: string ): number {
	let count = 0;
	for ( const c of text ) if ( c === CHAT_LINK_MARKER ) count++;
	return count;
}

/*
================
stripChatMarkers
================
*/
export function stripChatMarkers( text: string ): string {
	return text.split( CHAT_LINK_MARKER ).join( "" );
}

/*
================
replaceChatMarkers

Each marker replaced, in order, by its label; a marker past the labels
shows "<?>".
================
*/
export function replaceChatMarkers( text: string, labels: readonly string[] ): string {
	let next = 0;
	return text.replace( /\uFFFC/g, () => labels[next++] ?? UNKNOWN_LINK_LABEL );
}

/*
================
resolveChatLinks

The sender's side of the server's trimming (chat ResolveLinks): link i
belongs to marker i. Refs are kept in order while fewer than limit are
kept and read answers an item; a dropped ref's own marker goes, and so
does every marker past the refs. With
limit 0 (the server switch off) the line stays native: text untouched,
no refs.
================
*/
export function resolveChatLinks<T>(
	text: string,
	refs: readonly ChatLinkRef[],
	limit: number,
	read: ( ref: ChatLinkRef ) => T | undefined
): { text: string; refs: ChatLinkRef[]; items: T[]; } {
	if ( limit <= 0 ) return { text, refs: [], items: [] };
	const readAt = new Map<number, T>();
	for ( const [index, ref] of refs.entries() ) {
		if ( readAt.size >= limit ) break;
		const item = read( ref );
		if ( item !== undefined ) readAt.set( index, item );
	}
	const kept: ChatLinkRef[] = [], items: T[] = [];
	let marker = 0;
	const out = text.replace( /\uFFFC/g, () => {
		const index = marker++, item = readAt.get( index );
		if ( item === undefined ) return "";
		kept.push( refs[index]! );
		items.push( item );
		return CHAT_LINK_MARKER;
	} );
	return { text: out, refs: kept, items };
}

/*
================
chatLinkSpans

A displayed chat line with each marker replaced by its item's label, and,
when linked, the span each label covers (kind "chat-item:<index>") for the
chat log's hover regions. A marker without an item shows "<?>" and links
nothing. With links off the text is the line as it arrived, unchanged:
a pasted marker is plain text then, and no "<?>" stands in for it
(an owner report: links off turned a pasted marker into "<?>").
================
*/
export function chatLinkSpans(
	text: string,
	items: readonly ({ readonly name?: string; readonly plus?: number; } | undefined)[] | undefined,
	linked: boolean
): { text: string; spans: { kind: `chat-item:${number}`; start: number; end: number; }[]; } {
	const spans: { kind: `chat-item:${number}`; start: number; end: number; }[] = [];
	if ( !linked || !text.includes( CHAT_LINK_MARKER ) ) return { text, spans };
	let out = "", index = 0;
	for ( const c of text ) {
		if ( c !== CHAT_LINK_MARKER ) {
			out += c;
			continue;
		}
		const item = items?.[index], label = chatLinkLabel( item );
		if ( linked && item?.name ) {
			spans.push( { kind: `chat-item:${index}`, start: out.length, end: out.length + label.length } );
		}
		out += label;
		index++;
	}
	return { text: out, spans };
}
