/*
===========================================================================

chat-history.ts - the chat log the browser keeps across a reload

Port-only, not native (default by owner decision 2026-11-26): the chat
window remembers each character's log, every tab's last lines as the
window keeps them (chat-tabs.ts chatLogKept), so a page reload no longer wipes the conversation. The page stores one book
(scope to lines) in its local storage; the worker never touches storage.
A scope names one character on one shard (the world's division), because
names are unique within a shard, not across shards.

Only the plain facts of a line are stored: channel, sender, text, whether
it was the player's own, its gid, when it was said and which tabs file
it (the event and everyTab marks). Item links and
notice decorations are live-session presentations of bag state, so they
are not written down and a restored line never shows a stale item.

===========================================================================
*/
import type { ChatLine } from "@/engine/contracts/gameplay";
import { chatLogKept } from "./chat-tabs";

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
	readonly event?: boolean;
	readonly everyTab?: boolean;
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

The lines worth writing down, oldest first: the ones some tab still keeps.
================
*/
export function storedChatLines( lines: readonly ChatLine[] ): readonly StoredChatLine[] {
	return chatLogKept( lines ).map( line => ({
		channel: line.channel,
		name: line.name,
		...(line.gid !== undefined ? { gid: line.gid } : {}),
		text: line.text,
		outgoing: line.outgoing,
		sentAt: line.sentAt ?? 0,
		...(line.event ? { event: true } : {}),
		...(line.everyTab ? { everyTab: true } : {})
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
		sentAt: row.sentAt,
		...(row.event === true ? { event: true } : {}),
		...(row.everyTab === true ? { everyTab: true } : {})
	};
}

/*
================
chatHistoryLines

The lines one scope's record holds, read leniently: unparseable rows are
dropped and the record keeps what some tab still keeps.
================
*/
export function chatHistoryLines( value: unknown ): readonly StoredChatLine[] {
	if ( !Array.isArray( value ) ) return [];
	const out: StoredChatLine[] = [];
	for ( const row of value ) {
		const line = storedChatLine( row );
		if ( line ) out.push( line );
	}
	return chatLogKept( out );
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
