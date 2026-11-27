/*
===========================================================================

staff-roster.ts - staff state and the staff roster (port-only 0x3FCC/0x3FCD)

Port-only, not native; the default by owner decision 2026-11-23. The
server's staff book (apps/server/internal/game/staff) decides who is
staff and how each role looks; the console grants and revokes live.
The client learns it from two frames, at world entry and on every
change, so nothing waits for a relog:

	0x3FCC staff state, own character only:
	       u8 gm (0 or 1), u32 capability bits
	0x3FCD staff roster, every player:
	       u16 count, then per entry: u16 len + UTF-8 name,
	       u16 len + UTF-8 tag, u32 name ARGB, u32 chat ARGB

The original draws a name gold only when the name itself starts with
"[GM]" (858810), and paints a GM's All line pink as chat type 3; the
roster generalises both: a rostered player's board shows its role tag
before the name, the name in the role's colour, and its public lines in
the role's chat colour. The native "[GM]" name rule stays the fallback.
A shard with no staff sends neither frame; a client that predates them
drops the unknown opcodes.

This file holds the decoders and the lookups both the worker and the UI
use; it imports nothing, so Node tests load it as it ships.

===========================================================================
*/

export const OP_STAFF_STATE = 0x3fcc;
export const OP_STAFF_ROSTER = 0x3fcd;
// The global chat type: public chat on this shard, staff-coloured too.
const GLOBAL_CHAT_CHANNEL = 6;
// Mirrors the server's bound on roles and grants: staff are few.
const MAX_STAFF = 1024;
const STATE_BYTES = 5;

export interface StaffState {
	readonly gm: boolean;
	readonly capabilities: number;
}

export interface StaffRosterEntry {
	readonly name: string;
	readonly tag: string;
	// ARGB, as the entity name colours are kept (name-color.ts).
	readonly nameColor: number;
	readonly chatColor: number;
}

/*
================
decodeStaffState

A malformed frame is a protocol error, never a partial state.
================
*/
export function decodeStaffState( payload: Uint8Array ): StaffState {
	if ( payload.length !== STATE_BYTES || payload[0]! > 1 ) throw Error( "Invalid staff state" );
	const view = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
	return { gm: payload[0] === 1, capabilities: payload[0] === 1 ? view.getUint32( 1, true ) : 0 };
}

/*
================
decodeStaffRoster

The whole roster, replacing the last; an empty one ends every tag.
================
*/
export function decodeStaffRoster( payload: Uint8Array ): readonly StaffRosterEntry[] {
	const view = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
	if ( payload.length < 2 ) throw Error( "Invalid staff roster" );
	const count = view.getUint16( 0, true );
	if ( count > MAX_STAFF ) throw Error( "Invalid staff roster count" );
	const utf8 = new TextDecoder( "utf-8", { fatal: true } );
	let offset = 2;
	const text = (): string => {
		if ( offset + 2 > payload.length ) throw Error( "Invalid staff roster text" );
		const length = view.getUint16( offset, true );
		offset += 2;
		if ( offset + length > payload.length ) throw Error( "Invalid staff roster text" );
		const value = utf8.decode( payload.subarray( offset, offset + length ) );
		offset += length;
		return value;
	};
	const colour = (): number => {
		if ( offset + 4 > payload.length ) throw Error( "Invalid staff roster colour" );
		const value = view.getUint32( offset, true );
		offset += 4;
		return value;
	};
	const entries: StaffRosterEntry[] = [];
	for ( let i = 0; i < count; i++ ) {
		const name = text(), tag = text(), nameColor = colour(), chatColor = colour();
		entries.push( { name, tag, nameColor, chatColor } );
	}
	if ( offset !== payload.length ) throw Error( "Invalid staff roster" );
	return entries;
}

/*
================
staffEntry

The roster row for a character name; names compare as logins do, ASCII
case ignored.
================
*/
export function staffEntry(
	roster: readonly StaffRosterEntry[] | undefined,
	name: string | undefined
): StaffRosterEntry | undefined {
	if ( !roster?.length || !name ) return undefined;
	const folded = name.toLowerCase();
	return roster.find( entry => entry.name.toLowerCase() === folded );
}

/*
================
staffBoardName

The name board's text for a player: the role tag before the name. A
board alias (a job suit's name, #3214) hides who wears it, so it is
never tagged; neither is anything but a player.
================
*/
export function staffBoardName(
	entity: { readonly kind: string; readonly name?: string; readonly boardName?: string; },
	shown: string,
	roster: readonly StaffRosterEntry[] | undefined
): string {
	if ( entity.kind !== "player" && entity.kind !== "local-player" || entity.boardName !== undefined ) return shown;
	const entry = staffEntry( roster, entity.name );
	return entry && shown && !shown.startsWith( entry.tag ) ? entry.tag + shown : shown;
}

/*
================
staffChatColor

A rostered sender's colour for a public line (All 1, GM 3, Global 6),
as RGBA; other channels keep their own colour (whisper, party, guild...).
Global is public chat on this shard, where every player talks, so a GM
there is told apart the same way (owner, 2026-11-27).
================
*/
export function staffChatColor(
	roster: readonly StaffRosterEntry[] | undefined,
	channel: number,
	name: string | undefined
): [number, number, number, number] | undefined {
	if ( channel !== 1 && channel !== 3 && channel !== GLOBAL_CHAT_CHANNEL ) return undefined;
	const entry = staffEntry( roster, name );
	if ( !entry ) return undefined;
	const argb = entry.chatColor;
	return [ ((argb >>> 16) & 255) / 255, ((argb >>> 8) & 255) / 255, (argb & 255) / 255, 1 ];
}
