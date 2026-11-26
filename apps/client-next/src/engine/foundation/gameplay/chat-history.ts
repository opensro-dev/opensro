/*
===========================================================================

chat-history.ts - the chat log the browser keeps across a reload

Port-only, not native (#4513): while the Experimental row "Keep chat
history" is on, the chat window remembers the last CHAT_HISTORY_KEPT
lines of each character, so a page reload no longer wipes the
conversation. The row is off by default and off is the original: a
fresh page starts with an empty log and nothing is stored. The page
stores one book (scope to lines) in its local storage; the worker
never touches storage.
A scope names one character on one shard (the world's division), because
names are unique within a shard, not across shards.

Only the plain facts of a line are stored: channel, sender, text, whether
it was the player's own, its gid and when it was said. Item links and
notice decorations are live-session presentations of bag state, so they
are not written down and a restored line never shows a stale item.

===========================================================================
*/
import type { ChatLine } from "@/engine/contracts/gameplay";

// The lines of one character the browser remembers: the 101st line drops
// the oldest, in the window and in the record alike.
export const CHAT_HISTORY_KEPT = 100;

/*
================
StoredChatLine

One remembered line as it is written down.
================
*/
export interface StoredChatLine {
	readonly channel: number;
	readonly name: string;
	readonly gid?: number;
	readonly text: string;
	readonly outgoing: boolean;
	readonly sentAt: number;
}

/*
================
chatHistoryScope

The book's key for one character on one shard: the division (the shard's
name for itself in the world's own re-entry rule) and the character name.
================
*/
export function chatHistoryScope( division: string, name: string ): string {
	return division + "\n" + name;
}

/*
================
storedChatLines

The lines worth writing down, oldest first, capped at CHAT_HISTORY_KEPT.
================
*/
export function storedChatLines( lines: readonly ChatLine[] ): readonly StoredChatLine[] {
	const kept = lines.slice( -CHAT_HISTORY_KEPT );
	return kept.map( line => ({
		channel: line.channel,
		name: line.name,
		...(line.gid !== undefined ? { gid: line.gid } : {}),
		text: line.text,
		outgoing: line.outgoing,
		sentAt: line.sentAt ?? 0
	}) );
}

/*
================
storedChatLine

One stored row read back, or null where it is not one: a hand-edited or
older record is dropped row by row, never trusted whole.
================
*/
function storedChatLine( value: unknown ): StoredChatLine | null {
	if ( !value || typeof value !== "object" ) return null;
	const row = value as Record<string, unknown>;
	if (
		typeof row.channel !== "number" || !Number.isInteger( row.channel ) || row.channel < 0 ||
		typeof row.name !== "string" || typeof row.text !== "string" ||
		typeof row.outgoing !== "boolean" ||
		typeof row.sentAt !== "number" || !Number.isFinite( row.sentAt )
	) return null;
	return {
		channel: row.channel,
		name: row.name,
		...(typeof row.gid === "number" && Number.isInteger( row.gid ) ? { gid: row.gid } : {}),
		text: row.text,
		outgoing: row.outgoing,
		sentAt: row.sentAt
	};
}

/*
================
chatHistoryLines

The lines one scope's record holds, read leniently: unparseable rows are
dropped and the record is capped at CHAT_HISTORY_KEPT.
================
*/
export function chatHistoryLines( value: unknown ): readonly StoredChatLine[] {
	if ( !Array.isArray( value ) ) return [];
	const out: StoredChatLine[] = [];
	for ( const row of value ) {
		const line = storedChatLine( row );
		if ( line ) out.push( line );
	}
	return out.slice( -CHAT_HISTORY_KEPT );
}

/*
================
chatHistoryBook

The stored book: scope to its lines.
================
*/
export function chatHistoryBook( value: unknown ): Readonly<Record<string, readonly StoredChatLine[]>> {
	if ( !value || typeof value !== "object" || Array.isArray( value ) ) return {};
	const out: Record<string, readonly StoredChatLine[]> = {};
	for ( const [scope, entry] of Object.entries( value as Record<string, unknown> ) ) {
		if ( scope ) out[scope] = chatHistoryLines( entry );
	}
	return out;
}

/*
================
chatHistorySavedBook

The book after one scope's save joins it: the book the store holds now,
with this scope's lines replaced by the saved ones, so a record another
tab wrote since this tab loaded survives (the unique-notify book's rule,
#4433). An empty scope saves nothing.
================
*/
export function chatHistorySavedBook(
	stored: unknown,
	scope: string,
	lines: readonly StoredChatLine[]
): Readonly<Record<string, readonly StoredChatLine[]>> {
	const book: Record<string, readonly StoredChatLine[]> = { ...chatHistoryBook( stored ) };
	if ( scope ) book[scope] = chatHistoryLines( lines );
	return book;
}
