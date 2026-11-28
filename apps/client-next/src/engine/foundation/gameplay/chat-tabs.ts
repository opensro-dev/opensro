/*
===========================================================================

chat-tabs.ts - which chat tab files a line, and how many lines each keeps

The native chat viewer keeps one list per tab (All, Party, Guild, Ally)
and files each incoming line into the lists it belongs to
(CIFChatViewer_AppendLineByType 6ad280). Each list holds its own last
CHAT_TAB_LINES rows: CIFChatViewer's setup (6ac81e) writes 0x64 as the
maximum of all four lists, and the list's append (53a212) drops its
oldest row once it holds more. A line one tab no longer keeps can still
show on another.

The port holds one log for every tab, so this module owns both rules:
which tab shows a line (chatTabShows, read by the chat layout) and which
lines the log must keep so every tab still has its own last
CHAT_TAB_LINES (chatLogKept, applied by the worker's chat owner and the
browser's kept record). A single cap over the shared log let lines a tab
never shows push the visible ones out: an AFK player's All tab emptied
while drop and kill notices filled the port-only Event tab.

===========================================================================
*/
import { STALL_CHAT_CHANNEL } from "./stall";

// 6ac81e: every native chat list's maximum row count (0x64).
export const CHAT_TAB_LINES = 100;

/*
================
EVERY_TAB_CHAT_CHANNELS

The chat types that land on every native chat tab, never only on All:
the native viewer files each incoming line into the All block, and
these types into the Party, Guild and Ally blocks as well
(CIFChatViewer_AppendLineByType 6ad280: after the type table at 6ad5d8,
the branch on type 2, 0xff, 0xa or 3 appends to this+0x44c/0x450/0x454;
ClientChat_PresentChannelMessage 752800 presents by raw type). Type 2
is the whisper (#3356) and type 3 the GM's chat (#3370), so switching
tabs must not hide a conversation or a GM line. Types 10 and 0xff are
filed the same way; this port's chat decoder does not produce them
today, and the rule holds for them the moment it does. The channel
tabs otherwise keep filtering to their own channel alone. The
port-only Event tab stays notice-only, as it is for every other
channel.
================
*/
const EVERY_TAB_CHAT_CHANNELS: readonly number[] = [ 2, 3, 0xa, 0xff ] as const;

// The channel each native tab files beside the every-tab types: All
// (general chat), Party, Guild and Ally.
const CHAT_TAB_CHANNELS: readonly number[] = [ 1, 4, 5, 11 ] as const;

// The tabs with the port-only Event tab offered (All, Party, Guild,
// Ally, Event) and without it (the native four).
const EVENT_TAB_COUNT = 5;
const NATIVE_TAB_COUNT = 4;

/*
================
chatTabShows

Whether a line of the given chat channel shows on the given tab. Stall
lines belong to the stall window's own chat box. With the port-only
Event tab offered (tab 4), it lists every channel-7 notice, and the
port-only event notices (event: job kills, drops, plus results, breaks)
live only there: All skips them and the native tabs never carried them.
A native type-7 server notice, such as the operator's restart warning,
is meant for every player, so All keeps it too (owner 2026-11-14).
Port-only: a line marked everyTab (the consignment market's notices,
#3529) shows on every native tab, as the native every-tab types do; a
native notice stays on All.
================
*/
export function chatTabShows(
	channel: number,
	tab: number,
	eventTab: boolean,
	event = false,
	everyTab = false
): boolean {
	if ( channel === STALL_CHAT_CHANNEL ) return false;
	if ( everyTab && !(eventTab && tab === 4) ) return true;
	if ( eventTab && tab === 4 ) return channel === 7;
	if ( tab === 0 ) return !eventTab || !event;
	return channel === CHAT_TAB_CHANNELS[tab] || EVERY_TAB_CHAT_CHANNELS.includes( channel );
}

/*
================
ChatTabLine

The facts of a line the tab rule reads.
================
*/
export interface ChatTabLine {
	readonly channel: number;
	readonly event?: boolean;
	readonly everyTab?: boolean;
}

/*
================
chatLogKept

The lines of a log, oldest first, that some view still keeps: a line
stays while it is one of the last CHAT_TAB_LINES of at least one tab,
with the Event tab offered or not (the worker does not know the
player's choice, and either must find its full list), or of the stall
window's box. A view's own window then shows exactly its last
CHAT_TAB_LINES, as each native list does. Returns the log itself when
nothing is dropped.
================
*/
export function chatLogKept<T extends ChatTabLine>( lines: readonly T[] ): readonly T[] {
	const views: { tab: number; eventTab: boolean; }[] = [];
	for ( let tab = 0; tab < EVENT_TAB_COUNT; tab++ ) views.push( { tab, eventTab: true } );
	for ( let tab = 0; tab < NATIVE_TAB_COUNT; tab++ ) views.push( { tab, eventTab: false } );
	const counts = views.map( () => 0 );
	let stall = 0;
	const kept: T[] = [];
	for ( let i = lines.length - 1; i >= 0; i-- ) {
		const line = lines[i]!;
		let keep = false;
		if ( line.channel === STALL_CHAT_CHANNEL ) {
			keep = stall++ < CHAT_TAB_LINES;
		}
		views.forEach( ( view, v ) => {
			if ( !chatTabShows( line.channel, view.tab, view.eventTab, !!line.event, !!line.everyTab ) ) return;
			if ( counts[v]!++ < CHAT_TAB_LINES ) keep = true;
		} );
		if ( keep ) kept.push( line );
	}
	if ( kept.length === lines.length ) return lines;
	return kept.reverse();
}
