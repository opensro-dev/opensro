/*
===========================================================================

gacha.ts - the Magic Pang window session and roll lifecycle

Owns opening, ticket selection, roll timing and result sounds. Shared NPC
acknowledgements are validated before the Magic Pang service is selected.

===========================================================================
*/
import { gachaPrizes, isGachaTicket } from "@/engine/foundation/gameplay/gacha-catalog";
import { GACHA_FUNCTION, GACHA_ROLL_MS } from "@/engine/foundation/ui/gacha-window";
import { npcInteractionMask } from "@/engine/foundation/gameplay/npc-dialogue";
import type { GachaState } from "@/engine/contracts/item-process";
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { UiSoundHandle } from "@/engine/foundation/ui/sound-catalog";
const OP_NPC_ACTION = 0x7338;
const OP_GACHA_ROLL = 0x7053;
const OP_GACHA_EXCHANGE = 0x71be;
const FIRST_BAG_SLOT = 13;
const MAX_SLOT = 255;
const MAX_GID = 0xffffffff;
const LOW_WORD_MASK = 0xffffffffn;
const NPC_REQUEST_BYTES = 8;
const EXCHANGE_REQUEST_BYTES = 4;
const ROLL_REQUEST_BYTES = 9;

/*
================
createGacha
================
*/
export function createGacha() {
	let state: GachaState = {
		visible: false,
		phase: "closed",
		npc: 0,
		slot: null,
		entry: 0,
		started: 0,
		result: null,
		error: null
	};
	return {
		/*
		================
		open
		================
		*/
		open( gid: number ) {
			if (
				!Number.isInteger( gid ) || gid <= 0 || gid > MAX_GID || state.phase === "rolling" ||
				state.phase === "waiting"
			) throw Error( "Magic Pang unavailable" );
			const payload = new Uint8Array( NPC_REQUEST_BYTES ), v = new DataView( payload.buffer );
			v.setUint32( 0, gid, true );
			v.setUint32( 4, GACHA_FUNCTION, true );
			state = { ...state, phase: "opening", npc: gid, error: null };
			return { opcode: OP_NPC_ACTION, payload };
		},
		/*
		================
		exchange

		5DB67B: the operator row sends its NPC gid once 642270 found a voucher.
		================
		*/
		exchange( gid: number ) {
			if ( !Number.isInteger( gid ) || gid <= 0 || gid > MAX_GID ) {
				throw Error( "Magic Pang exchange unavailable" );
			}
			const payload = new Uint8Array( EXCHANGE_REQUEST_BYTES );
			new DataView( payload.buffer ).setUint32( 0, gid, true );
			return { opcode: OP_GACHA_EXCHANGE, payload };
		},
		/*
		================
		opened

		B338 is shared with other services, including special trade's six-byte
		acknowledgement. Validate its service grammar before routing by mask.
		================
		*/
		opened( p: Uint8Array ) {
			const mask = npcInteractionMask( p, 0 );
			if ( mask === null ) {
				if ( state.phase !== "opening" ) return false;
				state = { ...state, phase: "closed", error: p[1]! };
				return true;
			}
			if ( !(mask & GACHA_FUNCTION) ) return false;
			if ( state.phase !== "opening" ) return true;
			state = { ...state, visible: true, phase: "idle" };
			return true;
		},
		/*
		================
		close
		================
		*/
		close() {
			if ( state.phase === "rolling" || state.phase === "waiting" ) return false;
			state = { ...state, visible: false, phase: "closed" };
			return true;
		},
		/*
		================
		start
		================
		*/
		start( entry: number, slot: number, item: InventoryItem | undefined, now: number ): readonly UiSoundHandle[] {
			const flags = item?.typeFlags ?? 0;
			if (
				!state.visible || ![ "idle", "result" ].includes( state.phase ) ||
				!gachaPrizes().some( prize => prize.entry === entry ) || !item || slot < FIRST_BAG_SLOT ||
				slot > MAX_SLOT || item.slot !== slot || item.quantity <= 0 ||
				!isGachaTicket( flags )
			) throw Error( "Magic Pang roll unavailable" );
			state = {
				...state,
				phase: "rolling",
				entry,
				slot,
				started: now,
				result: null,
				reward: undefined,
				error: null
			};
			return [ "SND_GACHA_MOVE", "SND_GACHA_TURN" ];
		},
		/*
		================
		step
		================
		*/
		step( now: number, item: InventoryItem | undefined ) {
			if ( state.phase !== "rolling" || now - state.started < GACHA_ROLL_MS ) return null;
			if ( !item || item.slot !== state.slot || item.quantity <= 0 || !isGachaTicket( item.typeFlags ) ) {
				state = { ...state, phase: "idle", slot: null };
				return null;
			}
			const payload = new Uint8Array( ROLL_REQUEST_BYTES ), v = new DataView( payload.buffer );
			v.setUint32( 0, state.npc, true );
			v.setUint32( 4, state.entry, true );
			payload[8] = state.slot!;
			state = { ...state, phase: "waiting" };
			return { opcode: OP_GACHA_ROLL, payload };
		},
		/*
		================
		result
		================
		*/
		result( p: Uint8Array, item: InventoryItem | undefined ): readonly UiSoundHandle[] {
			// A refusal is [2][u16 code]: the server writes three bytes and
			// 766A20 reads the flag and the code's low byte for its notice.
			// 766A4E jumps directly to the system-notice dispatcher on failure;
			// it never calls CIFGhaCha_ApplyResult, so no result presentation.
			// Port-only, not native: native strands the busy byte 0xCEA7C4 that
			// 642170 set at the roll (766A4E -> 766A79 never clears it, and
			// CIFGacha_OnClose 641EC0 refuses while it is set), leaving the
			// window unclosable until relog. A refused roll returns the window
			// to idle instead, so it closes and rolls again.
			if ( p[0] !== 1 ) {
				if ( p.length !== 3 ) throw Error( "Invalid Magic Pang result" );
				if ( state.phase === "waiting" ) state = { ...state, phase: "idle" };
				return [];
			}
			if ( p.length !== 2 ) throw Error( "Invalid Magic Pang result" );
			if ( !state.visible || !item ) return [];
			if ( p[1] !== 1 ) {
				state = { ...state, phase: "result", result: "lose", error: null };
				return [ "SND_GACHA_END" ];
			}
			if ( item.magic.length !== 2 ) return [];
			// CSOItem's indexed magic map contains the two raw values for result cards.
			const refObjId = Number( BigInt( item.magic[0]! ) & LOW_WORD_MASK ),
				quantity = Number( BigInt( item.magic[1]! ) & LOW_WORD_MASK );
			state = { ...state, phase: "result", result: "win", reward: { refObjId, quantity }, error: null };
			return [ "SND_GACHA_WIN" ];
		},
		/*
		================
		state
		================
		*/
		state: () => state,
		/*
		================
		reset
		================
		*/
		reset() {
			state = {
				visible: false,
				phase: "closed",
				npc: 0,
				slot: null,
				entry: 0,
				started: 0,
				result: null,
				error: null
			};
		}
	};
}
