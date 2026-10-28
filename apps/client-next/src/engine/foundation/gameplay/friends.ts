/*
===========================================================================

friends.ts - the friend lane's wire: roster, events, request and consent

Pure decoders and encoders for the native v1.150 friend packets, read the
way the original client reads them:

	S->C 0x3769 roster seed      {u8 n, n x (u32 jid, sized name, u32 model,
	                              u8 state)}           (CharacterDependentData_ReadFriendRoster)
	S->C 0x3F9A friend event     u8 2 add / 3 delete / 4 state (760FB0);
	                              8 and 9 are the letter lane's
	S->C 0x7164 request prompt   {u32 jid, u32 serial, sized name} (760C90)
	S->C 0xB164 request result   {u8 1} | {u8 2, u8 code[, ...]}  (760AF0)
	S->C 0xB5DB delete result    {u8 1, u32 jid} | {u8 2, u8 code} (75CE90)
	C->S 0x7164 request          {sized name}                  (704590)
	C->S 0x3012 consent          {u32 jid, u32 serial, u8 accept} (701A50)
	C->S 0x75DB delete           {u32 jid}                     (701990)

Sized names are u16 length + bytes. Every decoder refuses a malformed
frame rather than guessing.

===========================================================================
*/
import { type TargetEntity, targetPlayer } from "./target-player";
import type { WireFrame } from "@/engine/contracts/network";

export const FRIEND_ROSTER_OPCODE = 0x3769;
export const FRIEND_EVENT_OPCODE = 0x3f9a;
export const FRIEND_REQUEST_OPCODE = 0x7164;
export const FRIEND_REQUEST_RESULT_OPCODE = 0xb164;
export const FRIEND_DELETE_OPCODE = 0x75db;
export const FRIEND_DELETE_RESULT_OPCODE = 0xb5db;
export const FRIEND_CONSENT_OPCODE = 0x3012;

// The friend list's row cap: the 0x14 of the panel's ": %d / %d" count
// (CIFFriendList_WriteCountLabel) and v1.188's CMessengerMgr add check.
export const FRIEND_LIST_LIMIT = 20;

// CMessengerMgr add's name bound (v1.188 5D7620: 1..0x20 bytes).
export const FRIEND_NAME_LIMIT = 32;

// The notice category the client shows friend codes in (689420 0x13/code).
export const FRIEND_NOTICE_CATEGORY = 0x13;

// 0xB164 code 0x0B carries a consent sub-reason (760AF0).
const FRIEND_CONSENT_CODE = 0x0b;
const FRIEND_CONSENT_REFUSED = 0;
const FRIEND_CONSENT_CANCELLED = 2;

// 0x3F9A event bytes this lane owns.
const FRIEND_EVENT_ADDED = 2;
const FRIEND_EVENT_DELETED = 3;
const FRIEND_EVENT_STATE = 4;

/*
================
FriendRow

One roster row. online is the wire state byte 0 (gil_contact_on); 1 is
offline.
================
*/
export interface FriendRow {
	readonly jid: number;
	readonly name: string;
	readonly modelRefId: number;
	readonly online: boolean;
}

/*
================
FriendEvent
================
*/
export type FriendEvent =
	| { readonly kind: "added"; readonly row: FriendRow; }
	| { readonly kind: "deleted"; readonly jid: number; }
	| { readonly kind: "state"; readonly jid: number; readonly online: boolean; };

/*
================
FriendPrompt

A request waiting for this player's answer.
================
*/
export interface FriendPrompt {
	readonly jid: number;
	readonly serial: number;
	readonly name: string;
}

/*
================
FriendsState

The lane as the UI reads it: the roster, the prompt waiting for this
player's answer, and whether this player's own request waits.
================
*/
export interface FriendsState {
	readonly rows: readonly FriendRow[];
	readonly prompt: FriendPrompt | null;
	readonly pending: boolean;
}

/*
================
FriendRequestResult

The requester's answer: accepted, a notice code (shown as 0x13/code), or
the consent form (refused by name, or cancelled).
================
*/
export type FriendRequestResult =
	| { readonly kind: "ok"; }
	| { readonly kind: "code"; readonly code: number; }
	| { readonly kind: "refused"; readonly name: string; }
	| { readonly kind: "cancelled"; readonly name: string; };

/*
================
FriendDeleteResult
================
*/
export type FriendDeleteResult =
	| { readonly kind: "ok"; readonly jid: number; }
	| { readonly kind: "code"; readonly code: number; };

/*
================
FriendReader

A bounds-checked little-endian cursor over one payload, made per decode.
================
*/
interface FriendReader {
	readonly u8: () => number;
	readonly u32: () => number;
	readonly name: () => string;
	readonly done: () => void;
}

/*
================
friendReader
================
*/
function friendReader( bytes: Uint8Array, what: string ): FriendReader {
	const view = new DataView( bytes.buffer, bytes.byteOffset, bytes.byteLength );
	let at = 0;
	const need = ( n: number ) => {
		if ( at + n > bytes.length ) throw Error( `Truncated ${what}` );
	};
	return {
		u8() {
			need( 1 );
			return view.getUint8( at++ );
		},
		u32() {
			need( 4 );
			const value = view.getUint32( at, true );
			at += 4;
			return value;
		},
		name() {
			need( 2 );
			const length = view.getUint16( at, true );
			at += 2;
			need( length );
			const value = new TextDecoder( "utf-8", { fatal: true } ).decode( bytes.subarray( at, at + length ) );
			at += length;
			return value;
		},
		done() {
			if ( at !== bytes.length ) throw Error( `Trailing bytes in ${what}` );
		}
	};
}

/*
================
decodeFriendRoster
================
*/
export function decodeFriendRoster( payload: Uint8Array ): FriendRow[] {
	const reader = friendReader( payload, "friend roster" );
	const count = reader.u8();
	if ( count > FRIEND_LIST_LIMIT ) throw Error( "Friend roster exceeds the list limit" );
	const rows: FriendRow[] = [];
	for ( let i = 0; i < count; i++ ) {
		const jid = reader.u32(), name = reader.name(), modelRefId = reader.u32(), state = reader.u8();
		if ( state > 1 ) throw Error( "Invalid friend state" );
		rows.push( { jid, name, modelRefId, online: state === 0 } );
	}
	reader.done();
	return rows;
}

/*
================
decodeFriendEvent

A 0x3F9A friend event, or null for the letter lane's cases.
================
*/
export function decodeFriendEvent( payload: Uint8Array ): FriendEvent | null {
	const reader = friendReader( payload, "friend event" );
	const kind = reader.u8();
	if ( kind === FRIEND_EVENT_ADDED ) {
		const jid = reader.u32(), name = reader.name(), modelRefId = reader.u32();
		reader.done();
		// 0x3F9A case 2 inserts the row online (760FB0): an add needs the
		// other player's live consent.
		return { kind: "added", row: { jid, name, modelRefId, online: true } };
	}
	if ( kind === FRIEND_EVENT_DELETED ) {
		const jid = reader.u32();
		reader.done();
		return { kind: "deleted", jid };
	}
	if ( kind === FRIEND_EVENT_STATE ) {
		const jid = reader.u32(), state = reader.u8();
		reader.done();
		if ( state > 1 ) throw Error( "Invalid friend state" );
		return { kind: "state", jid, online: state === 0 };
	}
	return null;
}

/*
================
decodeFriendPrompt
================
*/
export function decodeFriendPrompt( payload: Uint8Array ): FriendPrompt {
	const reader = friendReader( payload, "friend prompt" );
	const prompt = { jid: reader.u32(), serial: reader.u32(), name: reader.name() };
	reader.done();
	return prompt;
}

/*
================
decodeFriendRequestResult
================
*/
export function decodeFriendRequestResult( payload: Uint8Array ): FriendRequestResult {
	const reader = friendReader( payload, "friend request result" );
	const result = reader.u8();
	if ( result === 1 ) {
		reader.done();
		return { kind: "ok" };
	}
	if ( result !== 2 ) throw Error( "Invalid friend request result" );
	const code = reader.u8();
	if ( code !== FRIEND_CONSENT_CODE ) {
		reader.done();
		return { kind: "code", code };
	}
	const sub = reader.u8(), name = reader.name();
	reader.done();
	if ( sub === FRIEND_CONSENT_REFUSED ) return { kind: "refused", name };
	if ( sub === FRIEND_CONSENT_CANCELLED ) return { kind: "cancelled", name };
	throw Error( "Invalid friend consent result" );
}

/*
================
decodeFriendDeleteResult
================
*/
export function decodeFriendDeleteResult( payload: Uint8Array ): FriendDeleteResult {
	const reader = friendReader( payload, "friend delete result" );
	const result = reader.u8();
	if ( result === 1 ) {
		const jid = reader.u32();
		reader.done();
		return { kind: "ok", jid };
	}
	if ( result !== 2 ) throw Error( "Invalid friend delete result" );
	const code = reader.u8();
	reader.done();
	return { kind: "code", code };
}

/*
================
friendRequest

The C->S 0x7164 request; null for a name the original would not send
(empty, over FRIEND_NAME_LIMIT bytes).
================
*/
export function friendRequest( name: string ): WireFrame | null {
	const bytes = new TextEncoder().encode( name.trim() );
	if ( bytes.length < 1 || bytes.length > FRIEND_NAME_LIMIT ) return null;
	const payload = new Uint8Array( 2 + bytes.length );
	new DataView( payload.buffer ).setUint16( 0, bytes.length, true );
	payload.set( bytes, 2 );
	return { opcode: FRIEND_REQUEST_OPCODE, payload };
}

/*
================
friendConsent

The C->S 0x3012 answer to a prompt. The port's own game-ready is an empty
0x3012; this body is always 9 bytes.
================
*/
export function friendConsent( prompt: FriendPrompt, accept: boolean ): WireFrame {
	const payload = new Uint8Array( 9 ), view = new DataView( payload.buffer );
	view.setUint32( 0, prompt.jid, true );
	view.setUint32( 4, prompt.serial, true );
	payload[8] = accept ? 1 : 0;
	return { opcode: FRIEND_CONSENT_OPCODE, payload };
}

/*
================
friendDelete
================
*/
export function friendDelete( jid: number ): WireFrame {
	const payload = new Uint8Array( 4 );
	new DataView( payload.buffer ).setUint32( 0, jid, true );
	return { opcode: FRIEND_DELETE_OPCODE, payload };
}

/*
================
applyFriendEvent

The roster after one event. A delete or state change for an unlisted jid
is ignored here; the original dereferences it unguarded, the server never
sends one.
================
*/
export function applyFriendEvent( rows: readonly FriendRow[], event: FriendEvent ): readonly FriendRow[] {
	if ( event.kind === "added" ) {
		if ( rows.some( row => row.jid === event.row.jid ) ) {
			return rows.map( row => row.jid === event.row.jid ? event.row : row );
		}
		return [ ...rows, event.row ];
	}
	if ( event.kind === "deleted" ) return rows.filter( row => row.jid !== event.jid );
	return rows.map( row => row.jid === event.jid ? { ...row, online: event.online } : row );
}

/*
================
friendAddPrefill

The name CIFFriend_OnAddButton (602010) puts in the add box: the selected
player, or the rider of a selected mounted pet (target-player.ts).
================
*/
export function friendAddPrefill(
	target: number | undefined,
	entities: readonly TargetEntity[],
	localGid: number | undefined
): string {
	return targetPlayer( target, entities, localGid )?.name ?? "";
}

/*
================
friendNameOrder

The friend list's order (CIFFriend_SortAndFill 604FD0 through
WString_Less 78EC00): a-z folded to A-Z only, then UTF-16 code units, a
name before any longer name it begins. Native 78EC00 builds both keys in
one uninitialised buffer without clearing it between them (two
StringCchCatW, 78EC8F and 78ED0C), so it ends up ordering mostly by
length; the port keeps the comparison it was written to make, not the bug.
================
*/
export function friendNameOrder( a: string, b: string ): number {
	const fold = ( c: number ) => c >= 0x61 && c <= 0x7a ? c - 0x20 : c;
	const n = Math.min( a.length, b.length );
	for ( let i = 0; i < n; i++ ) {
		const d = fold( a.charCodeAt( i ) ) - fold( b.charCodeAt( i ) );
		if ( d ) return d;
	}
	return a.length - b.length;
}
