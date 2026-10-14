/*
===========================================================================

letters.ts - the letter lane's wire: mailbox, arrivals, send, read, delete

Pure decoders and encoders for the native v1.150 letter packets, read the
way the original client reads them:

	S->C 0xB3CD mailbox          {u8 1, u8 n, n x (sized sender, u32 model,
	                              u32 packed time, u8 read)} | {u8 2, u8 code}
	                                                              (75CEF0)
	S->C 0x3F9A letter event     u8 8 arrived {sized sender, u32 model,
	                              u32 packed time}; u8 9 {u8} unknown letter
	                                                              (760FB0)
	S->C 0xB261 send result      {u8 1} | {u8 2, u8 code}         (75CF60)
	S->C 0xB3F2 read result      {u8 1, u8 index, sized body}
	                              | {u8 2, u8 code}               (760E00)
	S->C 0xB0CC delete result    {u8 1, u8 index}                 (75CFD0)
	C->S 0x7261 send             {sized receiver, sized body}     (704730)
	C->S 0x73F2 read             {u8 index}                       (701BE0)
	C->S 0x70CC delete           {u8 index}                       (701CA0)

Sized strings are u16 length + bytes. A letter is named by its index in
the mailbox, which the server and the client keep aligned move for move.
Every decoder refuses a malformed frame rather than guessing; only the
player-written text inside is decoded leniently.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

export const LETTER_LIST_OPCODE = 0xb3cd;
export const LETTER_EVENT_OPCODE = 0x3f9a;
export const LETTER_SEND_OPCODE = 0x7261;
export const LETTER_SEND_RESULT_OPCODE = 0xb261;
export const LETTER_READ_OPCODE = 0x73f2;
export const LETTER_READ_RESULT_OPCODE = 0xb3f2;
export const LETTER_DELETE_OPCODE = 0x70cc;
export const LETTER_DELETE_RESULT_OPCODE = 0xb0cc;

// The mailbox cap: the 0x14 of the panel's ": %d / %d" count (605C00).
export const LETTER_MAILBOX_LIMIT = 20;

// The write window's edit caps (CIFEdit max lengths 0xC and 0x100, 60D17B
// and 60D18B).
export const LETTER_RECEIVER_LIMIT = 12;
export const LETTER_BODY_LIMIT = 256;

// The notice category the client shows letter error codes in (689420
// 0x14/code, from 75CEF0, 75CF60 and 760E00).
export const LETTER_NOTICE_CATEGORY = 0x14;

// 0x3F9A event bytes this lane owns.
const LETTER_EVENT_ARRIVED = 8;
const LETTER_EVENT_UNKNOWN = 9;

/*
================
LetterRow

One mailbox row. received is the packed receive time (yy, month, day,
hour, minute bit fields); body is the text once a read has fetched it.
================
*/
export interface LetterRow {
	readonly sender: string;
	readonly modelRefId: number;
	readonly received: number;
	readonly read: boolean;
	readonly body: string | null;
}

/*
================
LettersState

The lane as the UI reads it: the mailbox and the letter the read window
shows (-1 when closed).
================
*/
export interface LettersState {
	readonly rows: readonly LetterRow[];
	readonly reading: number;
}

/*
================
LetterResult

An answer: its index (read, delete) or nothing (send), or a notice code.
================
*/
export type LetterResult =
	| { readonly kind: "ok"; readonly index: number; readonly body?: string; }
	| { readonly kind: "code"; readonly code: number; };

/*
================
LetterEvent
================
*/
export type LetterEvent =
	| { readonly kind: "arrived"; readonly row: LetterRow; }
	| { readonly kind: "unknown"; };

/*
================
LetterTime

A packed receive time as 60C9B0 prints it: two-digit year, month and
day, the forenoon or afternoon key, the 12-hour hour and the minute.
================
*/
export interface LetterTime {
	readonly year: number;
	readonly month: number;
	readonly day: number;
	readonly meridiem: "UIIT_STT_LETTER_FORENOON" | "UIIT_STT_LETTER_AFTEMOON";
	readonly hour: number;
	readonly minute: number;
}

/*
================
LetterReader

A bounds-checked little-endian cursor over one payload, made per decode.
================
*/
interface LetterReader {
	readonly u8: () => number;
	readonly u32: () => number;
	readonly text: () => string;
	readonly done: () => void;
}

/*
================
letterReader
================
*/
function letterReader( bytes: Uint8Array, what: string ): LetterReader {
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
		text() {
			need( 2 );
			const length = view.getUint16( at, true );
			at += 2;
			need( length );
			// Player text decodes leniently (U+FFFD): a stored letter must never
			// fail its reader's session. Lengths and counts stay strict.
			const value = new TextDecoder( "utf-8" ).decode( bytes.subarray( at, at + length ) );
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
readCode

The {u8 2, u8 code} error arm every answer but the delete shares.
================
*/
function readCode( reader: LetterReader, result: number, what: string ): LetterResult {
	if ( result !== 2 ) throw Error( `Invalid ${what}` );
	const code = reader.u8();
	reader.done();
	return { kind: "code", code };
}

/*
================
decodeLetterList

The mailbox, or the notice code of the error arm.
================
*/
export function decodeLetterList( payload: Uint8Array ): readonly LetterRow[] | { readonly code: number; } {
	const reader = letterReader( payload, "letter list" );
	const result = reader.u8();
	if ( result !== 1 ) {
		const error = readCode( reader, result, "letter list result" );
		return { code: error.kind === "code" ? error.code : 0 };
	}
	const count = reader.u8();
	if ( count > LETTER_MAILBOX_LIMIT ) throw Error( "Letter list exceeds the mailbox limit" );
	const rows: LetterRow[] = [];
	for ( let i = 0; i < count; i++ ) {
		const sender = reader.text(), modelRefId = reader.u32(), received = reader.u32(), read = reader.u8();
		if ( read > 1 ) throw Error( "Invalid letter read flag" );
		rows.push( { sender, modelRefId, received, read: read === 1, body: null } );
	}
	reader.done();
	return rows;
}

/*
================
decodeLetterEvent

A 0x3F9A letter event, or null for the friend lane's cases.
================
*/
export function decodeLetterEvent( payload: Uint8Array ): LetterEvent | null {
	const reader = letterReader( payload, "letter event" );
	const kind = reader.u8();
	if ( kind === LETTER_EVENT_ARRIVED ) {
		const sender = reader.text(), modelRefId = reader.u32(), received = reader.u32();
		reader.done();
		// 822D10 appends the arrival unread.
		return { kind: "arrived", row: { sender, modelRefId, received, read: false, body: null } };
	}
	if ( kind === LETTER_EVENT_UNKNOWN ) {
		reader.u8();
		reader.done();
		return { kind: "unknown" };
	}
	return null;
}

/*
================
decodeLetterSendResult
================
*/
export function decodeLetterSendResult( payload: Uint8Array ): LetterResult {
	const reader = letterReader( payload, "letter send result" );
	const result = reader.u8();
	if ( result === 1 ) {
		reader.done();
		return { kind: "ok", index: -1 };
	}
	return readCode( reader, result, "letter send result" );
}

/*
================
decodeLetterReadResult
================
*/
export function decodeLetterReadResult( payload: Uint8Array ): LetterResult {
	const reader = letterReader( payload, "letter read result" );
	const result = reader.u8();
	if ( result === 1 ) {
		const index = reader.u8(), body = reader.text();
		reader.done();
		return { kind: "ok", index, body };
	}
	return readCode( reader, result, "letter read result" );
}

/*
================
decodeLetterDeleteResult

The erased index, or null: 75CFD0 reads nothing past a result other
than 1.
================
*/
export function decodeLetterDeleteResult( payload: Uint8Array ): number | null {
	const reader = letterReader( payload, "letter delete result" );
	if ( reader.u8() !== 1 ) return null;
	const index = reader.u8();
	reader.done();
	return index;
}

/*
================
sizedText
================
*/
function sizedText( value: string ): Uint8Array {
	const bytes = new TextEncoder().encode( value );
	const out = new Uint8Array( 2 + bytes.length );
	new DataView( out.buffer ).setUint16( 0, bytes.length, true );
	out.set( bytes, 2 );
	return out;
}

/*
================
letterSend

The C->S 0x7261 letter, receiver first; null for one the write window
would not send (an empty field, or one over its edit cap).
================
*/
export function letterSend( receiver: string, body: string ): WireFrame | null {
	const to = sizedText( receiver.trim() ), text = sizedText( body );
	if ( to.length === 2 || text.length === 2 ) return null;
	if ( to.length - 2 > LETTER_RECEIVER_LIMIT || text.length - 2 > LETTER_BODY_LIMIT ) return null;
	const payload = new Uint8Array( to.length + text.length );
	payload.set( to, 0 );
	payload.set( text, to.length );
	return { opcode: LETTER_SEND_OPCODE, payload };
}

/*
================
letterRead
================
*/
export function letterRead( index: number ): WireFrame {
	return { opcode: LETTER_READ_OPCODE, payload: Uint8Array.of( index ) };
}

/*
================
letterDelete
================
*/
export function letterDelete( index: number ): WireFrame {
	return { opcode: LETTER_DELETE_OPCODE, payload: Uint8Array.of( index ) };
}

/*
================
letterTime

60C9B0's fields: bits 0..5 year, 6..9 month, 10..14 day, 15..19 hour,
20..25 minute. An hour up to 12 is forenoon, as the original compares.
================
*/
export function letterTime( packed: number ): LetterTime {
	const hour = (packed >>> 15) & 0x1f, afternoon = hour > 12;
	return {
		year: packed & 0x3f,
		month: (packed >>> 6) & 0xf,
		day: (packed >>> 10) & 0x1f,
		meridiem: afternoon ? "UIIT_STT_LETTER_AFTEMOON" : "UIIT_STT_LETTER_FORENOON",
		hour: afternoon ? hour - 12 : hour,
		minute: (packed >>> 20) & 0x3f
	};
}

/*
================
letterTimeKey

A packed receive time as one ascending number (year, month, day, hour,
minute), for the time sort: the packing puts the year in the low bits.
================
*/
export function letterTimeKey( packed: number ): number {
	const time = letterTime( packed ), hour = (packed >>> 15) & 0x1f;
	return ((((time.year * 16 + time.month) * 32 + time.day) * 32 + hour) * 64) + time.minute;
}
