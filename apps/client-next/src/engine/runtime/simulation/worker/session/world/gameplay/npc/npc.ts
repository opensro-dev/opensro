/*
===========================================================================

npc.ts - ordered NPC conversations and service-window receipts

Only one untagged dialog request may be outstanding. A timeout preserves the
uncertain request until a reply or explicit close; it never permits a retry.
The resuscitation receipt has its own revision so presentation opens once.

===========================================================================
*/
import {
	decodeNpcDialogue,
	npcGid,
	npcConversationTransition,
	npcInteractionMask,
	type NpcConversation
} from "@/engine/foundation/gameplay/npc-dialogue";
import type { WireFrame } from "@/engine/contracts/network";

const RESUSCITATION_OPEN = 0x3230;
const NPC_DIALOG = 0x3773;
const NPC_ACTION = 0x7338;
const TALK_MASK = 2;

/*
================
createNpcConversation
================
*/
export function createNpcConversation( send: ( frame: WireFrame ) => void ) {
	let state: NpcConversation = { phase: "closed" };
	let interactionMask = 0, restorationRevision = 0;
	return {
		/*
		================
		select
		================
		*/
		select( gid: number ) {
			state = npcConversationTransition( state, { type: "select", gid: npcGid( gid ) } );
		},
		/*
		================
		talk
		================
		*/
		talk( now: number ) {
			const next = npcConversationTransition( state, { type: "request", now } );
			if ( next.phase !== "waiting" ) throw Error( "NPC transition" );
			const payload = new Uint8Array( 8 ), view = new DataView( payload.buffer );
			view.setUint32( 0, next.gid, true );
			view.setUint32( 4, TALK_MASK, true );
			send( { opcode: NPC_ACTION, payload } );
			state = next;
		},
		/*
		================
		choose
		================
		*/
		choose( choice: number, now: number ) {
			if ( state.phase !== "ready" || !state.dialogue.options.some( row => row.choice === choice ) ) {
				throw Error( "NPC choice unavailable" );
			}
			const next = npcConversationTransition( state, { type: "request", now } );
			send( { opcode: NPC_DIALOG, payload: Uint8Array.of( choice ) } );
			state = next;
		},
		/*
		================
		receive

		75AC60 closes the conversation and opens the native mode-2 skill pane.
		An unsolicited or duplicate receipt cannot reopen a dismissed window.
		================
		*/
		receive( frame: WireFrame ) {
			if ( frame.opcode === RESUSCITATION_OPEN ) {
				if ( frame.payload.length !== 0 ) throw Error( "Invalid resuscitation window receipt" );
				if ( state.phase === "waiting" || state.phase === "uncertain" ) {
					state = npcConversationTransition( state, { type: "close" } );
					restorationRevision++;
				}
				return true;
			}
			if ( frame.opcode !== NPC_DIALOG ) return false;
			const dialogue = decodeNpcDialogue( frame.payload );
			state = npcConversationTransition( state, { type: "reply", dialogue } );
			return true;
		},
		/*
		================
		step
		================
		*/
		step( now: number ) {
			const next = npcConversationTransition( state, { type: "tick", now } );
			if ( next === state ) return false;
			state = next;
			return true;
		},
		/*
		================
		state
		================
		*/
		state() {
			return state;
		},
		/*
		================
		restorationRevision
		================
		*/
		restorationRevision() {
			return restorationRevision;
		},
		/*
		================
		interaction
		================
		*/
		interaction( payload: Uint8Array, capabilities: number ) {
			const next = npcInteractionMask( payload, capabilities );
			if ( next !== null ) interactionMask = next;
		},
		/*
		================
		interactionLocked
		================
		*/
		interactionLocked() {
			return interactionMask !== 0;
		},
		/*
		================
		clear
		================
		*/
		clear() {
			state = npcConversationTransition( state, { type: "close" } );
			interactionMask = 0;
			restorationRevision = 0;
		}
	};
}
