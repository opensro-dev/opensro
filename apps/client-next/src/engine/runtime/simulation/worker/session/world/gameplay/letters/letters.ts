/*
===========================================================================

letters.ts - the mailbox, an arriving letter and the open read window

The worker owner of the letter lane (wire: foundation/gameplay/letters.ts).
It keeps the mailbox the server seeds at world entry (0xB3CD) and patches
with 0x3F9A case 8 and the delete answer, each letter's body once a read
fetched it (8277D0 stores it and marks the letter read), and which letter
the read window shows (607830).

The original asks for the mailbox (0x73CD) only when the panel opens
before any seed arrived (605BB0); the port's server seeds it at every
world entry, so the request is never needed.

Notices are returned to the composer, which owns the notice list: the
send confirmation and an unknown letter in the notice window (67D030),
error codes as notice 0x14/code.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import { constantNativeNotice } from "@/engine/foundation/gameplay/native-notice";
import type { SystemNotice } from "@/engine/foundation/gameplay/system-notices";
import {
	decodeLetterDeleteResult,
	decodeLetterEvent,
	decodeLetterList,
	decodeLetterReadResult,
	decodeLetterSendResult,
	LETTER_DELETE_RESULT_OPCODE,
	LETTER_EVENT_OPCODE,
	LETTER_LIST_OPCODE,
	LETTER_NOTICE_CATEGORY,
	LETTER_READ_RESULT_OPCODE,
	LETTER_SEND_RESULT_OPCODE,
	letterDelete,
	letterRead,
	letterSend,
	type LetterRow,
	type LettersState
} from "@/engine/foundation/gameplay/letters";

/*
================
LettersReceipt

What one frame did: whether this owner took it, and its notices.
================
*/
export interface LettersReceipt {
	readonly handled: boolean;
	readonly notices: readonly SystemNotice[];
}

/*
================
windowNotice

A text in the notice window (67D030).
================
*/
function windowNotice( key: string ): SystemNotice {
	return { key, value: 0, notificationBanner: true, bannerOnly: true };
}

/*
================
codeNotices

Notice 0x14/code; the codes the original shows nothing for give none.
================
*/
function codeNotices( code: number ): readonly SystemNotice[] {
	const notice = constantNativeNotice( LETTER_NOTICE_CATEGORY, code );
	return notice ? [ notice ] : [];
}

/*
================
createLetters
================
*/
export function createLetters( send: ( frame: WireFrame ) => void ) {
	let rows: readonly LetterRow[] = [], reading = -1;

	return {
		/*
		================
		reset

		A character change or session end forgets the mailbox; the next
		world entry seeds it again.
		================
		*/
		reset() {
			rows = [];
			reading = -1;
		},
		/*
		================
		state
		================
		*/
		state(): LettersState {
			return { rows, reading };
		},
		/*
		================
		send

		Sends a letter, or answers the write window's own refusal (60D270):
		an empty receiver first, then an empty body.
		================
		*/
		send( receiver: string, body: string ): readonly SystemNotice[] {
			if ( !receiver.trim() ) return [ windowNotice( "UIIT_MSG_LETTERERR_IDWRITE" ) ];
			if ( !body ) return [ windowNotice( "UIIT_MSG_LETTERERR_MESSAGEWRITE" ) ];
			const frame = letterSend( receiver, body );
			if ( frame ) send( frame );
			return [];
		},
		/*
		================
		read

		609B50: a letter whose body is already here opens at once; any other
		is fetched, and the answer opens it.
		================
		*/
		read( index: number ) {
			const row = rows[index];
			if ( !row ) return;
			if ( row.body !== null ) {
				reading = index;
				return;
			}
			send( letterRead( index ) );
		},
		/*
		================
		close
		================
		*/
		close() {
			reading = -1;
		},
		/*
		================
		remove

		Sent after the confirm box (605C50); the answer erases the row.
		================
		*/
		remove( index: number ) {
			if ( !rows[index] ) return;
			send( letterDelete( index ) );
		},
		/*
		================
		receive
		================
		*/
		receive( frame: WireFrame ): LettersReceipt {
			const p = frame.payload;
			if ( frame.opcode === LETTER_LIST_OPCODE ) {
				const list = decodeLetterList( p );
				if ( "code" in list ) return { handled: true, notices: codeNotices( list.code ) };
				rows = list;
				reading = -1;
				return { handled: true, notices: [] };
			}
			if ( frame.opcode === LETTER_EVENT_OPCODE ) {
				const event = decodeLetterEvent( p );
				// Cases 2, 3 and 4 are the friend lane's.
				if ( !event ) return { handled: false, notices: [] };
				if ( event.kind === "unknown" ) {
					return { handled: true, notices: [ windowNotice( "UIIT_MSG_LETTERERR_UNKNOWNLETTER" ) ] };
				}
				rows = [ ...rows, event.row ];
				return { handled: true, notices: [] };
			}
			if ( frame.opcode === LETTER_SEND_RESULT_OPCODE ) {
				const result = decodeLetterSendResult( p );
				if ( result.kind === "code" ) return { handled: true, notices: codeNotices( result.code ) };
				return { handled: true, notices: [ windowNotice( "UIIT_MSG_LETTER_SENDOK" ) ] };
			}
			if ( frame.opcode === LETTER_READ_RESULT_OPCODE ) {
				const result = decodeLetterReadResult( p );
				if ( result.kind === "code" ) return { handled: true, notices: codeNotices( result.code ) };
				if ( !rows[result.index] ) return { handled: true, notices: [] };
				rows = rows.map( ( row, i ) =>
					i === result.index ? { ...row, read: true, body: result.body ?? "" } : row
				);
				reading = result.index;
				return { handled: true, notices: [] };
			}
			if ( frame.opcode === LETTER_DELETE_RESULT_OPCODE ) {
				const index = decodeLetterDeleteResult( p );
				if ( index === null || !rows[index] ) return { handled: true, notices: [] };
				rows = rows.filter( ( _, i ) => i !== index );
				// The read window shows a row by index: the erased one closes it,
				// a later one moves up with the list.
				if ( reading === index ) reading = -1;
				else if ( reading > index ) reading--;
				return { handled: true, notices: [] };
			}
			return { handled: false, notices: [] };
		}
	};
}
