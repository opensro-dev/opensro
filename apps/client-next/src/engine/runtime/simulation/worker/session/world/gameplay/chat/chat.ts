/*
===========================================================================

chat.ts - character-session chat, receipts and received public history

World travel replaces scene data, not the conversation. Character changes and
session disposal clear it. A server-authored public echo determines the beta
channel; its later native receipt only releases the outstanding request.

===========================================================================
*/
import { chatBlocks, chatIsBlocked } from "@/engine/foundation/gameplay/chat-blocks";
import { blockedWhisperers, whisperBlockRequest, whisperBlockResult } from "@/engine/foundation/gameplay/whisper-block";
import { chatRejectionKey, type ChatFeedback } from "@/engine/foundation/gameplay/chat-feedback";
import type { ChatLine } from "@/engine/contracts/gameplay";
import type { WireFrame } from "@/engine/contracts/network";
import { STALL_CHAT_CHANNEL } from "@/engine/foundation/gameplay/stall";
import { CHAT_HISTORY_CONTROL } from "@/engine/foundation/gameplay/commerce-controls";
const CHAT_LINE_LIMIT = 128;
const CHAT_FEEDBACK_LIMIT = 100;
const CHAT_TEXT_LIMIT = 100;
const CHAT_NAME_LIMIT = 128;
const CHAT_ACK_TIMEOUT_MS = 10000;
const CHAT_RECEIPT_KEY = 255;
const CHAT_GLOBAL_CHANNEL = 6;
// The server's public transcript frame layout version and line bound
// (history.go OpChatHistory). Version 2 carries each line's send time.
const CHAT_HISTORY_VERSION = 2;
const CHAT_HISTORY_LIMIT = 10;

/*
================
PendingChat

Only one native keyed receipt may be outstanding. A public echo is delivered
before that receipt, allowing the server's channel to own presentation.
================
*/
interface PendingChat {
	channel: number;
	text: string;
	target: string;
	deadline: number;
	echoed: boolean;
}

// v1.150 social/chat/wire.go: 7367 requests, B367 keyed receipt, 3667 broadcast.
/*
================
decodeChatBroadcast

One 0x3667 line (sub_753760): channels 1 and 3 carry the speaker's gid,
2, 4, 5, 6, 11 and the stall's a sized sender name, 7 neither. Null for a channel this
owner does not present.
================
*/
function decodeChatBroadcast(
	p: Uint8Array
): { channel: number; sender: string; gid: number | undefined; text: string; } | null {
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let o = 0;
	/*
	================
	take
	================
	*/
	function take( n: number ) {
		if ( o + n > p.length ) throw new Error( "Truncated chat message" );
		const at = o;
		o += n;
		return at;
	}
	const channel = v.getUint8( take( 1 ) );
	let sender = "", gid: number | undefined;
	if ( channel === 1 || channel === 3 ) gid = v.getUint32( take( 4 ), true );
	else if ( [ 2, 4, 5, 6, 11, STALL_CHAT_CHANNEL ].includes( channel ) ) {
		const n = v.getUint16( take( 2 ), true );
		if ( n >= CHAT_NAME_LIMIT ) throw new Error( "Chat name budget" );
		const at = take( n );
		sender = new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( at, at + n ) );
	} else if ( channel !== 7 ) return null;
	const n = v.getUint16( take( 2 ), true );
	if ( n > CHAT_TEXT_LIMIT ) throw new Error( "Chat text budget" );
	const at = take( n * 2 ),
		text = new TextDecoder( "utf-16le", { fatal: true } ).decode( p.subarray( at, at + n * 2 ) );
	if ( o !== p.length ) throw new Error( "Chat trailing bytes" );
	return { channel, sender, gid, text };
}

/*
================
decodeChatHistory

The server's public transcript (history.go OpChatHistory, a browser
extension): {u8 version=2, u8 count, count x (u64 sentAt, u16 length, 0x3667
payload)}. sentAt is the server's Unix milliseconds when the line was said,
so a replayed line shows its real time, not this client's login (BUG-067).
Only named public lines are admitted; anything else is a malformed frame.
================
*/
export function decodeChatHistory(
	p: Uint8Array
): { channel: number; sender: string; text: string; sentAt: number; }[] {
	if ( p.length < 2 || p[0] !== CHAT_HISTORY_VERSION || p[1]! > CHAT_HISTORY_LIMIT ) {
		throw new Error( "Invalid chat history" );
	}
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength ), lines = [];
	let o = 2;
	for ( let i = 0; i < p[1]!; i++ ) {
		if ( o + 10 > p.length ) throw new Error( "Truncated chat history" );
		const sentAt = Number( v.getBigUint64( o, true ) ), n = v.getUint16( o + 8, true );
		o += 10;
		if ( o + n > p.length ) throw new Error( "Truncated chat history" );
		const line = decodeChatBroadcast( p.subarray( o, o + n ) );
		o += n;
		if ( !line || !line.sender ) throw new Error( "Chat history line has no sender" );
		lines.push( { channel: line.channel, sender: line.sender, text: line.text, sentAt } );
	}
	if ( o !== p.length ) throw new Error( "Chat history trailing bytes" );
	return lines;
}

/*
================
createChat
================
*/
export function createChat( send: ( frame: WireFrame ) => void ) {
	let lines: readonly ChatLine[] = [],
		pending: PendingChat | null = null,
		error: string | null = null,
		name = "";
	let localBlocks: readonly string[] = [];
	let blocked: readonly string[] = [];
	let blockPending = false;
	let blockDeadline = 0, blockError: string | null = null;
	let whispers = true;
	let sequence = 0;
	let feedback: readonly ChatFeedback[] = [];
	/*
================
append

Live lines are stamped on arrival; replayed history keeps the server's time.
================
	*/
	function append( line: ChatLine ) {
		lines = [ ...lines.slice( 1 - CHAT_LINE_LIMIT ), {
			...line,
			sequence: ++sequence,
			sentAt: line.sentAt ?? Date.now()
		} ];
	}
	return {
		/*
================
chatBlocks
================
		*/
		chatBlocks( value: readonly string[] ) {
			localBlocks = chatBlocks( value );
		},
		/*
================
options
================
		*/
		options( enabled: boolean ) {
			whispers = enabled;
		},
		/*
================
bootstrap
================
		*/
		bootstrap( value: unknown ) {
			blocked = blockedWhisperers( value );
			blockPending = false;
			blockError = null;
			const b = value as { character?: { name?: string; }; };
			const nextName = typeof b.character?.name === "string" ? b.character.name : "";
			if ( !nextName || nextName !== name ) {
				lines = [];
				feedback = [];
				pending = null;
				error = null;
			}
			name = nextName;
		},
		/*
================
block
================
		*/
		block( name: string, enabled: boolean, now = 0 ) {
			if ( blockPending ) return;
			const frame = whisperBlockRequest( name, enabled );
			send( frame );
			blockPending = true;
			blockDeadline = now + CHAT_ACK_TIMEOUT_MS;
			blockError = null;
		},
		/*
================
request
================
		*/
		request( channel: number, text: string, target: string, now: number ) {
			if ( pending ) throw new Error( "Chat acknowledgement pending" );
			if (
				![ 1, 2, 3, 4, 5, 11, STALL_CHAT_CHANNEL ].includes( channel ) || !text.trim() ||
				text.length > CHAT_TEXT_LIMIT ||
				text.includes( "\0" )
			) throw new Error( "Invalid chat message" );
			const targetBytes = new TextEncoder().encode( target );
			if (
				channel === 2 &&
				(!targetBytes.length || targetBytes.length >= CHAT_NAME_LIMIT || target.includes( "\0" ))
			) {
				throw new Error( "Invalid whisper target" );
			}
			const p = new Uint8Array( 4 + text.length * 2 + (channel === 2 ? 2 + targetBytes.length : 0) ),
				v = new DataView( p.buffer );
			p[0] = channel;
			p[1] = CHAT_RECEIPT_KEY;
			let o = 2;
			if ( channel === 2 ) {
				v.setUint16( o, targetBytes.length, true );
				o += 2;
				p.set( targetBytes, o );
				o += targetBytes.length;
			}
			v.setUint16( o, text.length, true );
			o += 2;
			for ( let i = 0; i < text.length; i++ ) v.setUint16( o + i * 2, text.charCodeAt( i ), true );
			send( { opcode: 0x7367, payload: p } );
			pending = { channel, text, target, deadline: now + CHAT_ACK_TIMEOUT_MS, echoed: false };
			error = null;
		},
		/*
================
receive
================
		*/
		receive( frame: WireFrame, localGid: number, senderName = "" ) {
			const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			if ( frame.opcode === 0xb367 ) {
				const ok = p[0] === 1;
				if ( (ok ? p.length !== 3 : p[0] !== 2 || p.length !== 4) ) {
					throw new Error( "Invalid chat acknowledgement" );
				}
				const at = ok ? 1 : 2;
				if ( !pending || p[at] !== pending.channel || p[at + 1] !== CHAT_RECEIPT_KEY ) return true;
				if ( ok ) {
					if ( !pending.echoed && (pending.channel !== 2 || whispers) ) {
						append( {
							channel: pending.channel,
							name: pending.channel === 2 ? pending.target : name,
							gid: pending.channel === 2 ? undefined : localGid,
							text: pending.text,
							outgoing: true
						} );
					}
					error = null;
				} else {
					const key = chatRejectionKey( p[1]! );
					if ( key ) {
						feedback = [ ...feedback.slice( 1 - CHAT_FEEDBACK_LIMIT ), {
							sequence: ++sequence,
							key,
							argument: pending.target
						} ];
					}
					error = null;
				}
				pending = null;
				return true;
			}
			if ( frame.opcode === 0xb66f ) {
				const r = whisperBlockResult( p );
				if ( r.name !== undefined ) {
					blocked = r.mode === 1 ?
						(blocked.includes( r.name ) ? blocked : [ ...blocked, r.name ]) :
						blocked.filter( name => name !== r.name );
				}
				if ( r.key ) {
					feedback = [ ...feedback.slice( 1 - CHAT_FEEDBACK_LIMIT ), {
						sequence: ++sequence,
						key: r.key,
						argument: ""
					} ];
				}
				blockPending = false;
				blockError = null;
				return true;
			}
			if ( frame.opcode === CHAT_HISTORY_CONTROL ) {
				// The beta public transcript the server replays at admission: lines
				// already said, filed as history so nothing presents them as speech.
				for ( const line of decodeChatHistory( p ) ) {
					if ( !chatIsBlocked( localBlocks, line.channel, line.sender ) ) {
						append( {
							channel: line.channel,
							name: line.sender,
							text: line.text,
							outgoing: false,
							history: true,
							sentAt: line.sentAt
						} );
					}
				}
				return true;
			}
			if ( frame.opcode !== 0x3667 ) return false;
			const line = decodeChatBroadcast( p );
			if ( !line ) return false;
			const { channel, sender, gid, text } = line;
			if ( channel === CHAT_GLOBAL_CHANNEL && sender === name ) {
				// Native receipts retain the request's channel. The beta server
				// sends this authoritative echo first, so the receipt must not
				// invent a second, differently colored line.
				const outgoing = pending !== null && [ 1, 3 ].includes( pending.channel ) && pending.text === text;
				if ( outgoing ) {
					if ( pending!.echoed ) return true;
					pending!.echoed = true;
				}
				append( { channel, name: sender, text, outgoing, gid: outgoing ? localGid : undefined } );
				return true;
			}
			// 752800 filters named incoming channels 1..5 and 11 before any presentation.
			if (
				!chatIsBlocked( localBlocks, channel, sender || senderName ) && (channel !== 2 || whispers) &&
				gid !== localGid && (!sender || sender !== name)
			) append( { channel, name: sender || senderName, gid, text, outgoing: false } );
			return true;
		},
		/*
================
step
================
		*/
		step( now: number ) {
			let changed = false;
			if ( blockPending && now >= blockDeadline && blockError === null ) {
				blockError = "Block acknowledgement timed out. Reconnect before sending again.";
				changed = true;
			}
			// Port-only, not native: the original client has no chat deadline.
			// An unanswered line releases the slot instead of locking chat for
			// the session. Acknowledgements carry no line identity, so a late one
			// can at worst echo the next line early; a player who could never
			// chat again until a reload was the worse failure.
			if ( pending && now >= pending.deadline ) {
				pending = null;
				error = "Your last chat message was not confirmed and may not have been sent.";
				changed = true;
			}
			return changed;
		},
		/*
================
state
================
		*/
		state() {
			return { lines, feedback, blocked, blockPending, blockError, pending: pending !== null, error };
		},
		/*
================
clear
================
		*/
		clear() {
			blocked = [];
			blockPending = false;
			blockError = null;
			lines = [];
			feedback = [];
			pending = null;
			error = null;
			name = "";
		}
	};
}
