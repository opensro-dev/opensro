/*
===========================================================================

chat.ts - character-session chat, receipts and received public history

World travel replaces scene data, not the conversation, and so does a
re-entry of the same character after its session was lost (#3435): the
lines a player was away for stay to be read. Character changes, a departure
to character select and session disposal clear it. A server-authored public
echo determines the beta channel; its later native receipt only releases the
outstanding request.

===========================================================================
*/
import { chatBlocks, chatIsBlocked } from "@/engine/foundation/gameplay/chat-blocks";
import { blockedWhisperers, whisperBlockRequest, whisperBlockResult } from "@/engine/foundation/gameplay/whisper-block";
import { chatRejectionKey, type ChatFeedback } from "@/engine/foundation/gameplay/chat-feedback";
import type { ChatLine, InventoryItem } from "@/engine/contracts/gameplay";
import type { WireFrame } from "@/engine/contracts/network";
import { STALL_CHAT_CHANNEL } from "@/engine/foundation/gameplay/stall";
import {
	BREAK_NOTICE_CHANNEL,
	DROP_PLUS_NOTICE_CHANNEL,
	isServerNoticeChannel
} from "@/engine/foundation/gameplay/system-notices";
import { decodeNoticeLinks, type NoticeLinks } from "@/engine/foundation/gameplay/notice-links";
import {
	type ChatLinkRef,
	decodeChatLinkTail,
	encodeChatLinkRefs,
	resolveChatLinks,
	stripChatMarkers
} from "@/engine/foundation/gameplay/chat-item-links";
import { CHAT_HISTORY_CONTROL } from "@/engine/foundation/gameplay/commerce-controls";
import { NO_JOB_CLASS } from "@/engine/foundation/gameplay/job-guild";
import { partyActiveJob } from "@/engine/foundation/gameplay/party-matching";
import { CHAT_ACK_TIMEOUT_MS, uiTuningFrom } from "@/engine/foundation/ui/ui-tuning";
const CHAT_LINE_LIMIT = 128;
const CHAT_FEEDBACK_LIMIT = 100;
const CHAT_TEXT_LIMIT = 100;
const CHAT_NAME_LIMIT = 128;
const CHAT_RECEIPT_KEY = 255;
const CHAT_GLOBAL_CHANNEL = 6;
// The server's public transcript frame layout version and line bound
// (history.go OpChatHistory). Version 2 carries each line's send time.
const CHAT_HISTORY_VERSION = 2;
const CHAT_HISTORY_LIMIT = 10;
// A replayed line repeats a retained one (#3435) when its channel, sender and
// text match and its server send time lies this close to the retained line's
// time. A live line carries no send time and is stamped with this client's
// clock on arrival, so the window absorbs the two clocks' skew.
const CHAT_REPLAY_MATCH_MS = 5 * 60 * 1000;
// The player channels whose lines may carry item links (port-only,
// SRO_CHAT_ITEM_LINKS): All, whisper, GM, party, guild, global and union.
// Stall lines never do in v1.
const CHAT_LINK_CHANNELS: readonly number[] = [ 1, 2, 3, 4, 5, CHAT_GLOBAL_CHANNEL, 11 ];

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
	// Port-only: the items the line links, as this client holds them.
	items: readonly InventoryItem[];
}

// v1.150 social/chat/wire.go: 7367 requests, B367 keyed receipt, 3667 broadcast.
/*
================
decodeChatBroadcast

One 0x3667 line (CPSMission_HandleChatBroadcast0x3667): channels 1 and 3 carry the speaker's gid,
2, 4, 5, 6, 11 and the stall's a sized sender name, 7 neither. The
port-only job-kill, drop/plus and item-break channels (isServerNoticeChannel) are laid
out and shown as channel 7; a drop/plus line may carry its links after the
text (notice-links.ts), a player line its item links (chat-item-links.ts).
Null for a channel this owner does not present.
================
*/
function decodeChatBroadcast(
	p: Uint8Array
): {
	channel: number;
	sender: string;
	gid: number | undefined;
	text: string;
	event?: boolean;
	links?: NoticeLinks;
	bodies?: Uint8Array[];
} | null {
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
	const wireChannel = v.getUint8( take( 1 ) ),
		channel = isServerNoticeChannel( wireChannel ) ? 7 : wireChannel,
		// A port-only notice channel shown as type 7 (system-notices.ts).
		event = channel === 7 && wireChannel !== 7 ? { event: true } : {};
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
	if ( (wireChannel === DROP_PLUS_NOTICE_CHANNEL || wireChannel === BREAK_NOTICE_CHANNEL) && o < p.length ) {
		// Port-only links, decoded leniently: a malformed section is dropped.
		// The break channel's section is a failed reinforce's item (#3260).
		const links = decodeNoticeLinks( p, o );
		return { channel, sender, gid, text, ...event, ...(links ? { links } : {}) };
	}
	if ( CHAT_LINK_CHANNELS.includes( wireChannel ) && o < p.length ) {
		// Port-only item links (chat-item-links.ts), decoded leniently: a
		// malformed tail is no links and a plain line.
		const bodies = decodeChatLinkTail( p, o );
		return { channel, sender, gid, text, ...event, ...(bodies?.length ? { bodies } : {}) };
	}
	if ( o !== p.length ) throw new Error( "Chat trailing bytes" );
	return { channel, sender, gid, text, ...event };
}

/*
================
decodeChatHistory

The server's public transcript (history.go OpChatHistory, a browser
extension): {u8 version=2, u8 count, count x (u64 sentAt, u16 length, 0x3667
payload)}. sentAt is the server's Unix milliseconds when the line was said,
so a replayed line shows its real time, not this client's login (BUG-067).
Only named public lines are admitted; anything else is a malformed frame.

A frame of another layout version is release skew, not a malformed frame:
a tab still on the previous release reconnects to an updated server and
is replayed the new layout before the release watch offers its refresh
(#2896: the version 1 -> 2 bump of 2026-10-08 ended 35 sessions on the old
build). The replay is cosmetic, so that frame is set aside as no lines.
================
*/
export function decodeChatHistory(
	p: Uint8Array
): { channel: number; sender: string; text: string; sentAt: number; bodies?: Uint8Array[]; }[] {
	if ( p.length >= 1 && p[0] !== CHAT_HISTORY_VERSION ) return [];
	if ( p.length < 2 || p[1]! > CHAT_HISTORY_LIMIT ) throw new Error( "Invalid chat history" );
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
		lines.push( {
			channel: line.channel,
			sender: line.sender,
			text: line.text,
			sentAt,
			...(line.bodies ? { bodies: line.bodies } : {})
		} );
	}
	if ( o !== p.length ) throw new Error( "Chat history trailing bytes" );
	return lines;
}

/*
================
ownDisplayName

The name this client's own lines arrive under: the job alias while the
entry's suit is a job suit, the character name otherwise - the same rule
the entry alias board applies (8675F0: entry suit classify + jobAlias).
The native broadcast handler (753760) compares a frame's sender name
with the local player's live display name (the wstring at +0x108, the
alias while dressed; compared at 753BF4 and 753D7B, the global case
through its accessor at 753CA4) to recognise its own line; the server
stamps that same alias into the broadcast, so a suited player's copy
only compares equal against it.
================
*/
function ownDisplayName( value: unknown, name: string ): string {
	const b = value as {
		equipItems?: readonly { slot: number; refObjId: number; }[];
		refItemSnapshot?: readonly { refObjId: number; typeFlags: number; }[];
	};
	const flags = new Map<number, number>();
	for ( const row of b.refItemSnapshot ?? [] ) {
		if ( Number.isInteger( row?.refObjId ) && Number.isInteger( row?.typeFlags ) ) {
			flags.set( row.refObjId, row.typeFlags );
		}
	}
	return suitedName(
		entryJobAlias( value ),
		name,
		(b.equipItems ?? []).map( row => ({
			slot: row.slot,
			typeFlags: flags.get( row.refObjId ) ?? 0
		}) )
	);
}

/*
================
entryJobAlias

The alias the world entry recorded for this character, or "" when it
carries none.
================
*/
function entryJobAlias( value: unknown ): string {
	const alias = (value as { localPlayerEntry?: { jobAlias?: unknown; }; }).localPlayerEntry?.jobAlias;
	return typeof alias === "string" ? alias : "";
}

/*
================
suitedName

The name own lines arrive under for one suit state: the job alias while
a row set wears a job suit in the suit slot, the character name
otherwise. The entry snapshot classifies the suit worn at entry; live
equipment rows (equipment()) classify the suit worn now.
================
*/
function suitedName(
	alias: string,
	name: string,
	rows: readonly { slot: number; typeFlags: number; }[]
): string {
	if ( !name || !alias ) return name;
	return partyActiveJob( rows ) !== NO_JOB_CLASS ? alias : name;
}

/*
================
createChat
================
*/
export function createChat(
	send: ( frame: WireFrame ) => void,
	noticeItem: ( body: Uint8Array ) => InventoryItem | undefined = () => undefined,
	linkedItem: ( ref: ChatLinkRef ) => InventoryItem | undefined = () => undefined
) {
	// The port's own deadline for a chat or block acknowledgement (ui-tuning.ts).
	let ackTimeoutMs = CHAT_ACK_TIMEOUT_MS;
	let lines: readonly ChatLine[] = [],
		pending: PendingChat | null = null,
		error: string | null = null,
		name = "",
		alias = "",
		displayName = "";
	let localBlocks: readonly string[] = [];
	let blocked: readonly string[] = [];
	let blockPending = false;
	let blockDeadline = 0, blockError: string | null = null;
	let whispers = true;
	let sequence = 0;
	let feedback: readonly ChatFeedback[] = [];
	// Port-only (SRO_CHAT_ITEM_LINKS): the server's per-line link limit; 0 is off.
	let linkLimit = 0;
	/*
================
linkedLine

A received line's item links, decoded through the bag's own presentation.
================
	*/
	function linkedLine( bodies: readonly Uint8Array[] | undefined ) {
		return bodies?.length ? { links: bodies.map( body => noticeItem( body ) ) } : {};
	}
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
	/*
================
retained

Whether the log as it stood before the replay already holds a replayed line
(CHAT_REPLAY_MATCH_MS). A replay that repeats its own text keeps both.
================
	*/
	function retained( log: readonly ChatLine[], channel: number, sender: string, text: string, sentAt: number ) {
		return log.some( line =>
			line.channel === channel && line.name === sender && line.text === text &&
			Math.abs( (line.sentAt ?? 0) - sentAt ) <= CHAT_REPLAY_MATCH_MS
		);
	}
	/*
================
suspend

The transport the character spoke on is gone: a send waiting for its
receipt and a block waiting for its answer will hear nothing.
================
	*/
	function suspend() {
		blockPending = false;
		blockError = null;
		pending = null;
		error = null;
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
			// Port-only: the shard may tune the acknowledgement deadline (ui-tuning.ts).
			ackTimeoutMs = uiTuningFrom( value ).chatAckTimeoutMs;
			const limit = (value as { chatItemLinks?: unknown; } | null)?.chatItemLinks;
			linkLimit = typeof limit === "number" && Number.isInteger( limit ) && limit > 0 ? limit : 0;
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
			alias = entryJobAlias( value );
			displayName = ownDisplayName( value, nextName );
		},
		/*
		================
		equipment

		Live equipment rows replace the entry snapshot's suit
		classification: the server stamps a line's sender from the suit
		worn at send time, so the name own lines are recognised by
		follows the suit slot as it is equipped and removed mid-session,
		the way the native client's live display name does.
		================
		*/
		equipment( rows: readonly { slot: number; typeFlags: number; }[] ) {
			displayName = suitedName( alias, name, rows );
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
			blockDeadline = now + ackTimeoutMs;
			blockError = null;
		},
		/*
================
request
================
		*/
		request( channel: number, text: string, target: string, now: number, links: readonly ChatLinkRef[] = [] ) {
			if ( pending ) throw new Error( "Chat acknowledgement pending" );
			// Port-only (SRO_CHAT_ITEM_LINKS): only links this client can read
			// go out, as the server would keep them; the stall box links nothing.
			const resolved = resolveChatLinks(
				text,
				channel === STALL_CHAT_CHANNEL ? [] : links,
				linkLimit,
				linkedItem
			);
			// A line that was only an unreadable link has nothing left to say.
			if ( linkLimit > 0 && !resolved.text.trim() && text.trim() ) return;
			text = resolved.text;
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
			const block = encodeChatLinkRefs( resolved.refs ),
				payload = block.length ? new Uint8Array( p.length + block.length ) : p;
			if ( block.length ) {
				payload.set( p );
				payload.set( block, p.length );
			}
			send( { opcode: 0x7367, payload } );
			pending = { channel, text, target, deadline: now + ackTimeoutMs, echoed: false, items: resolved.items };
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
							outgoing: true,
							...(pending.items.length ? { links: pending.items } : {})
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
				// After a re-entry the retained log already holds the lines it
				// repeats (#3435).
				const before = lines;
				for ( const line of decodeChatHistory( p ) ) {
					if ( retained( before, line.channel, line.sender, line.text, line.sentAt ) ) continue;
					if ( !chatIsBlocked( localBlocks, line.channel, line.sender ) ) {
						append( {
							channel: line.channel,
							name: line.sender,
							text: line.text,
							outgoing: false,
							history: true,
							sentAt: line.sentAt,
							...linkedLine( line.bodies )
						} );
					}
				}
				return true;
			}
			if ( frame.opcode !== 0x3667 ) return false;
			const line = decodeChatBroadcast( p );
			if ( !line ) return false;
			const { channel, sender, gid, text, event, links, bodies } = line;
			if ( channel === CHAT_GLOBAL_CHANNEL && sender === displayName ) {
				// Native receipts retain the request's channel. The beta server
				// sends this authoritative echo first (under the display name -
				// the alias while suited, which 753760 compares at +0x108), so
				// the receipt must not invent a second, differently colored line.
				// A link the server could not read leaves the echo without its
				// marker, so the two compare without markers (chat-item-links.ts).
				const outgoing = pending !== null && [ 1, 3 ].includes( pending.channel ) &&
					stripChatMarkers( pending.text ) === stripChatMarkers( text );
				if ( outgoing ) {
					if ( pending!.echoed ) return true;
					pending!.echoed = true;
				}
				append( {
					channel,
					name: sender,
					text,
					outgoing,
					gid: outgoing ? localGid : undefined,
					...linkedLine( bodies )
				} );
				return true;
			}
			// 752800 filters named incoming channels 1..5 and 11 before any presentation.
			if (
				!chatIsBlocked( localBlocks, channel, sender || senderName ) && (channel !== 2 || whispers) &&
				gid !== localGid && (!sender || sender !== displayName)
			) {
				// Port-only: a drop/plus notice's player and item (notice-links.ts).
				const item = links?.body ? noticeItem( links.body ) : undefined,
					notice = links && (links.name || item) ?
						{ notice: { ...(links.name ? { name: links.name } : {}), ...(item ? { item } : {}) } } :
						{};
				append( {
					channel,
					name: sender || senderName,
					gid,
					text,
					outgoing: false,
					...(event ? { event } : {}),
					...notice,
					...linkedLine( bodies )
				} );
			}
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
suspend
================
		*/
		suspend,
		/*
================
clear
================
		*/
		clear() {
			suspend();
			blocked = [];
			lines = [];
			feedback = [];
			name = "";
			alias = "";
			displayName = "";
		}
	};
}
