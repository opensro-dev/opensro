/*
===========================================================================

friends.ts - the friend list, an incoming request and the request in flight

The worker owner of the friend lane (wire: foundation/gameplay/friends.ts).
It keeps the roster the server seeds at world entry (0x3769) and patches
with 0x3F9A, the one request prompt that may be waiting for this player,
and whether this player's own request waits for an answer: the original's
panel states 2 and 1 (CIFPanel +0x7D4, set by 760C90 and the request
send, cleared by 760AF0 and the consent).

Notices are returned to the composer, which owns the notice list:
connect/disconnect as chat system lines (760FB0 -> ChatSystemMessage
channel 0, for any friend but oneself: 56E110), a refusal and a cancel in
the notice window (67D030), every other code as notice 0x13/code.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import { constantNativeNotice } from "@/engine/foundation/gameplay/native-notice";
import type { SystemNotice } from "@/engine/foundation/gameplay/system-notices";
import {
	applyFriendEvent,
	decodeFriendDeleteResult,
	decodeFriendEvent,
	decodeFriendPrompt,
	decodeFriendRequestResult,
	decodeFriendRoster,
	FRIEND_DELETE_RESULT_OPCODE,
	FRIEND_EVENT_OPCODE,
	FRIEND_NOTICE_CATEGORY,
	FRIEND_REQUEST_OPCODE,
	FRIEND_REQUEST_RESULT_OPCODE,
	FRIEND_ROSTER_OPCODE,
	friendConsent,
	friendDelete,
	friendRequest,
	type FriendPrompt,
	type FriendRow,
	type FriendsState
} from "@/engine/foundation/gameplay/friends";

// ChatSystemMessage channel 0, the system line of 760FB0's notices.
const SYSTEM_CHAT_CHANNEL = 0;

/*
================
FriendsReceipt

What one frame did: whether this owner took it, and its notices.
================
*/
export interface FriendsReceipt {
	readonly handled: boolean;
	readonly notices: readonly SystemNotice[];
}

/*
================
windowNotice

A text in the notice window (67D030), as 760AF0 shows a refusal and a
cancel.
================
*/
function windowNotice( key: string ): SystemNotice {
	return { key, value: 0, notificationBanner: true, bannerOnly: true };
}

/*
================
codeNotices

Notice 0x13/code; the codes the original shows nothing for give none.
================
*/
function codeNotices( code: number ): readonly SystemNotice[] {
	const notice = constantNativeNotice( FRIEND_NOTICE_CATEGORY, code );
	return notice ? [ notice ] : [];
}

/*
================
createFriends
================
*/
export function createFriends( send: ( frame: WireFrame ) => void ) {
	let rows: readonly FriendRow[] = [], prompt: FriendPrompt | null = null, pending = false;

	return {
		/*
		================
		reset

		A character change or session end forgets everything; the next world
		entry seeds the roster again.
		================
		*/
		reset() {
			rows = [];
			prompt = null;
			pending = false;
		},
		/*
		================
		state
		================
		*/
		state(): FriendsState {
			return { rows, prompt, pending };
		},
		/*
		================
		request

		Asks the server to befriend name. One request at a time, as the
		original's panel state 1 allows; a name the original would not send
		sends nothing.
		================
		*/
		request( name: string ) {
			if ( pending ) return;
			const frame = friendRequest( name );
			if ( !frame ) return;
			send( frame );
			pending = true;
		},
		/*
		================
		answer

		Answers the waiting prompt (0x3012) and closes it.
		================
		*/
		answer( accept: boolean ) {
			if ( !prompt ) return;
			send( friendConsent( prompt, accept ) );
			prompt = null;
		},
		/*
		================
		remove
		================
		*/
		remove( jid: number ) {
			if ( !rows.some( row => row.jid === jid ) ) return;
			send( friendDelete( jid ) );
		},
		/*
		================
		receive
		================
		*/
		receive( frame: WireFrame, localName: string ): FriendsReceipt {
			const p = frame.payload;
			if ( frame.opcode === FRIEND_ROSTER_OPCODE ) {
				rows = decodeFriendRoster( p );
				return { handled: true, notices: [] };
			}
			if ( frame.opcode === FRIEND_EVENT_OPCODE ) {
				const event = decodeFriendEvent( p );
				// Cases 8 and 9 are the letter lane's.
				if ( !event ) return { handled: false, notices: [] };
				const row = event.kind === "state" ? rows.find( r => r.jid === event.jid ) : undefined;
				rows = applyFriendEvent( rows, event );
				if ( event.kind !== "state" || !row || row.online === event.online || row.name === localName ) {
					return { handled: true, notices: [] };
				}
				return {
					handled: true,
					notices: [ {
						key: event.online ? "UIIT_MSG_FRIEND_CONNECT" : "UIIT_MSG_FRIEND_DISCONNECT",
						value: 0,
						arguments: [ row.name ],
						formatKinds: [ "s" ],
						nativeType: SYSTEM_CHAT_CHANNEL
					} ]
				};
			}
			if ( frame.opcode === FRIEND_REQUEST_OPCODE ) {
				prompt = decodeFriendPrompt( p );
				return { handled: true, notices: [] };
			}
			if ( frame.opcode === FRIEND_REQUEST_RESULT_OPCODE ) {
				const result = decodeFriendRequestResult( p );
				if ( result.kind === "cancelled" ) {
					// 760AF0: an open prompt is retired silently; a waiting request
					// is told it was cancelled.
					if ( prompt && prompt.name === result.name ) {
						prompt = null;
						return { handled: true, notices: [] };
					}
					const notices = pending ? [ windowNotice( "UIIT_MSG_FRIEND_SYSCANCEL" ) ] : [];
					pending = false;
					return { handled: true, notices };
				}
				pending = false;
				if ( result.kind === "refused" ) {
					return { handled: true, notices: [ windowNotice( "UIIT_MSG_FRIENDERR_REFUSAL" ) ] };
				}
				return { handled: true, notices: result.kind === "code" ? codeNotices( result.code ) : [] };
			}
			if ( frame.opcode === FRIEND_DELETE_RESULT_OPCODE ) {
				const result = decodeFriendDeleteResult( p );
				return { handled: true, notices: result.kind === "code" ? codeNotices( result.code ) : [] };
			}
			return { handled: false, notices: [] };
		}
	};
}
