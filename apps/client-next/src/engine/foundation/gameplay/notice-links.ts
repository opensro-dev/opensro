/*
===========================================================================

notice-links.ts - the player and item a drop or plus notice names

Port-only, not native. The shard's seal-drop and +8-and-above notices
(0x3667 channel 0x81, server wire.DropPlusNotificationFrame) and its +7
notices (channel 0x82, the same frame) carry, after their text, the
player the line names and the item's encoded body:

	[u16 nameLen][ascii name][u16 bodyLen][item body (decodeInventoryItem)]

A zero length means no name or no item. The section is player-adjacent
data, so it decodes leniently: anything malformed yields no links and the
line stays plain text; it never fails the session.

===========================================================================
*/

// The character-name budget (domain.CharacterNameMaxBytes).
const NOTICE_NAME_MAX = 12;
// An item body is a few dozen bytes; a magic-option list stays well inside this.
const NOTICE_BODY_MAX = 512;

/*
================
NoticeLinks

What a notice line links to: a player name (a click whispers it) and an
item's encoded body (a hover shows its tooltip).
================
*/
export interface NoticeLinks {
	readonly name?: string;
	readonly body?: Uint8Array;
}

/*
================
decodeNoticeLinks

The link section starting at offset, or null when it is absent or
malformed. Never throws.
================
*/
export function decodeNoticeLinks( p: Uint8Array, offset: number ): NoticeLinks | null {
	if ( offset >= p.length ) return null;
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let o = offset;
	if ( o + 2 > p.length ) return null;
	const nameLength = v.getUint16( o, true );
	o += 2;
	if ( nameLength > NOTICE_NAME_MAX || o + nameLength > p.length ) return null;
	let name = "";
	for ( let i = 0; i < nameLength; i++ ) {
		const c = p[o + i]!;
		if ( !(c >= 0x30 && c <= 0x39 || c >= 0x41 && c <= 0x5a || c >= 0x61 && c <= 0x7a || c === 0x5f) ) return null;
		name += String.fromCharCode( c );
	}
	o += nameLength;
	if ( o + 2 > p.length ) return null;
	const bodyLength = v.getUint16( o, true );
	o += 2;
	if ( bodyLength > NOTICE_BODY_MAX || o + bodyLength !== p.length ) return null;
	const body = bodyLength ? p.slice( o, o + bodyLength ) : undefined;
	if ( !name && !body ) return null;
	return { ...(name ? { name } : {}), ...(body ? { body } : {}) };
}

/*
================
NoticeRowLink

One linked span of a displayed chat row: its kind, its offset in the row
and the text it covers.
================
*/
export interface NoticeRowLink {
	readonly kind: "name" | "item";
	readonly at: number;
	readonly value: string;
	readonly part?: number;
}

/*
================
noticeRowLinks

The player and item names one displayed row of a notice holds. The player
comes first in the line, so the item is looked for after it. An item whose
client name differs from the server's text is not linked: the line stays
plain text there.
================
*/
export function noticeRowLinks(
	row: string,
	notice: { readonly name?: string; readonly item?: { readonly name?: string; }; }
): NoticeRowLink[] {
	return noticeWrappedLinks( row, [ row ], notice )[0]!;
}

/*
================
noticeWrappedLinks

The linked player and item spans across the wrapped rows of one notice
line. Matching in the full line before projecting onto each wrapped row
keeps a multi-word item name linked across a line break and never matches
the player's name inside a later row's monster or seal text.
================
*/
export function noticeWrappedLinks(
	full: string,
	rows: readonly string[],
	notice: { readonly name?: string; readonly item?: { readonly name?: string; }; } | undefined
): NoticeRowLink[][] {
	if ( !notice ) return rows.map( () => [] );
	const spans: { readonly kind: "name" | "item"; readonly start: number; readonly end: number; }[] = [];
	let from = 0;
	if ( notice.name ) {
		const at = full.indexOf( notice.name );
		if ( at >= 0 ) {
			spans.push( { kind: "name", start: at, end: at + notice.name.length } );
			from = at + notice.name.length;
		}
	}
	const itemName = notice.item?.name;
	if ( itemName ) {
		const at = full.indexOf( itemName, from );
		if ( at >= 0 ) spans.push( { kind: "item", start: at, end: at + itemName.length } );
	}
	const parts = { name: 0, item: 0 };
	let cursor = 0;
	return rows.map( row => {
		const rowStart = row ? full.indexOf( row, cursor ) : cursor;
		if ( rowStart < 0 ) return [];
		const rowEnd = rowStart + row.length;
		cursor = rowEnd;
		const links: NoticeRowLink[] = [];
		for ( const span of spans ) {
			const start = Math.max( rowStart, span.start ), end = Math.min( rowEnd, span.end );
			if ( end <= start ) continue;
			const at = start - rowStart, value = row.slice( at, end - rowStart );
			if ( !value.trim() ) continue;
			const part = parts[span.kind]++;
			links.push( { kind: span.kind, at, value, ...(part ? { part } : {}) } );
		}
		return links;
	} );
}
