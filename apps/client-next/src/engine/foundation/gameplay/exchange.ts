/*
===========================================================================

exchange.ts - the player-to-player exchange window's state and wire

CIFExchange (interface child 0x1A) opens on 0xB237 [1][u32 partner] for
the requester (75B370) and 0x3219 [u32 partner] for the accepting player
(75B420). Each side holds twelve slots (6B2DA0): 0x3569 [u32 owner][u8
count] and per item ([u8 bag slot] when the owner is the reader)[u8
slot][CSOItem] lists a side (75B690); 0x30BB [2][u32] is the partner's
gold (75B580). The single confirm button (6B2280) sends 0x7095 to lock
the own offer and, once the partner locked too (0x37CF, 75AB40), 0x734A
to approve. 0xB095 / 0xB34A / 0xB2DB answer [1] or [2][code]; 0x3272
reports the swap (765260) and 0x3457 [u8 code] a failure (75B820), both
closing the window.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { ExchangeOffer, ExchangeState } from "@/engine/contracts/item-process";
import { decodeInventoryItem } from "./inventory-item";

export const EXCHANGE_SLOTS = 12;
export const OP_EXCHANGE_REQUEST = 0x7237;
const OP_EXCHANGE_REQUEST_RESULT = 0xb237;
const OP_EXCHANGE_OPENED = 0x3219;
const OP_EXCHANGE_CONFIRM = 0x7095;
const OP_EXCHANGE_CONFIRM_RESULT = 0xb095;
const OP_EXCHANGE_PARTNER_LOCKED = 0x37cf;
const OP_EXCHANGE_APPROVE = 0x734a;
const OP_EXCHANGE_APPROVE_RESULT = 0xb34a;
const OP_EXCHANGE_CANCEL = 0x72db;
const OP_EXCHANGE_CANCEL_RESULT = 0xb2db;
const OP_EXCHANGE_OFFER = 0x3569;
const OP_EXCHANGE_PARTNER_GOLD = 0x30bb;
const OP_EXCHANGE_SUCCEEDED = 0x3272;
const OP_EXCHANGE_FAILED = 0x3457;
const OP_ITEM_MOVE = 0x706d;
const OP_ITEM_MOVE_RESULT = 0xb06d;
const MOVE_EXCHANGE_PUT = 4;
const MOVE_EXCHANGE_TAKE = 5;
const MOVE_EXCHANGE_GOLD = 0x0d;
const PARTNER_GOLD_KIND = 2;
// CGInterface_ShowSystemNotification category of the exchange refusals.
export const EXCHANGE_NOTICE_CATEGORY = 1;

/*
================
emptyExchange
================
*/
export function emptyExchange(): ExchangeState {
	return {
		open: false,
		partner: 0,
		own: [],
		theirs: [],
		ownGold: 0,
		theirGold: 0,
		ownLocked: false,
		theirLocked: false,
		approved: false,
		requesting: false
	};
}

/*
================
ExchangeOutcome

The next state, a category-1 notice code the frame raised, and the swap
a 0x3272 asks the bag to apply.
================
*/
export interface ExchangeOutcome {
	readonly state: ExchangeState;
	readonly notice?: number;
	readonly swap?: { readonly receive: readonly InventoryItem[]; readonly give: readonly number[]; };
}

/*
================
exchangeFrame

Folds one frame; null for frames the exchange does not own. refs maps a
RefObjID to its type word (the inventory's reference projection).
================
*/
export function exchangeFrame(
	state: ExchangeState,
	frame: WireFrame,
	refs: ReadonlyMap<number, number>,
	objRefs: ReadonlyMap<number, number> = new Map()
): ExchangeOutcome | null {
	const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	const result = () => {
		if ( p[0] === 1 ) return 0;
		if ( p[0] !== 2 || p.length !== 2 ) throw Error( "Invalid exchange result" );
		return p[1]!;
	};
	switch ( frame.opcode ) {
		case OP_EXCHANGE_REQUEST_RESULT: {
			const code = result();
			if ( code ) return { state: { ...state, requesting: false }, notice: code };
			if ( p.length !== 5 ) throw Error( "Invalid exchange opening" );
			return { state: { ...emptyExchange(), open: true, partner: v.getUint32( 1, true ) } };
		}
		case OP_EXCHANGE_OPENED:
			if ( p.length !== 4 ) throw Error( "Invalid exchange opening" );
			return { state: { ...emptyExchange(), open: true, partner: v.getUint32( 0, true ) } };
		case OP_EXCHANGE_OFFER: {
			const owner = v.getUint32( 0, true ), count = p[4]!, mine = owner !== state.partner;
			let o = 5;
			const rows: ExchangeOffer[] = [];
			for ( let i = 0; i < count; i++ ) {
				const bagSlot = mine ? p[o++] : undefined, slot = p[o++]!;
				if ( slot >= EXCHANGE_SLOTS ) throw Error( "Invalid exchange slot" );
				const decoded = decodeInventoryItem( p, o, refs, objRefs );
				o = decoded.next;
				if ( decoded.item ) {
					rows.push( { slot, ...(bagSlot === undefined ? {} : { bagSlot }), item: decoded.item } );
				}
			}
			if ( o !== p.length ) throw Error( "Trailing exchange bytes" );
			return { state: mine ? { ...state, own: rows } : { ...state, theirs: rows } };
		}
		case OP_EXCHANGE_PARTNER_GOLD:
			if ( p[0] !== PARTNER_GOLD_KIND || p.length !== 5 ) return null;
			return { state: { ...state, theirGold: v.getUint32( 1, true ) } };
		case OP_EXCHANGE_PARTNER_LOCKED:
			return { state: { ...state, theirLocked: true } };
		// The refusals of these three are the social lane's category-1
		// notices (social.ts); only a success changes the window.
		case OP_EXCHANGE_CONFIRM_RESULT:
			return state.open && p[0] === 1 ? { state: { ...state, ownLocked: true } } : null;
		case OP_EXCHANGE_APPROVE_RESULT:
			return state.open && p[0] === 1 ? { state: { ...state, approved: true } } : null;
		case OP_EXCHANGE_CANCEL_RESULT:
			return state.open && p[0] === 1 ? { state: emptyExchange() } : null;
		case OP_EXCHANGE_SUCCEEDED:
			return {
				state: emptyExchange(),
				swap: {
					receive: [ ...state.theirs ].sort( ( a, b ) => a.slot - b.slot ).map( row => row.item ),
					give: state.own.flatMap( row => row.bagSlot === undefined ? [] : [ row.bagSlot ] )
				}
			};
		case OP_EXCHANGE_FAILED:
			if ( p.length !== 1 ) throw Error( "Invalid exchange failure" );
			return { state: emptyExchange(), notice: p[0]! };
		case OP_ITEM_MOVE_RESULT:
			// 759A30 types 4, 5 and 0xD; the 0x3569 list that follows is the
			// authority over what sits on the table.
			if ( !state.open || p[0] !== 1 ) return null;
			if ( p[1] === MOVE_EXCHANGE_PUT || p[1] === MOVE_EXCHANGE_TAKE ) return { state };
			if ( p[1] === MOVE_EXCHANGE_GOLD ) {
				if ( p.length !== 6 ) throw Error( "Invalid exchange gold" );
				return { state: { ...state, ownGold: v.getUint32( 2, true ) } };
			}
			return null;
	}
	return null;
}

/*
================
ExchangeCommand
================
*/
export type ExchangeCommand =
	| { readonly kind: "exchange-request"; readonly gid: number; }
	| { readonly kind: "exchange-put"; readonly slot: number; }
	| { readonly kind: "exchange-take"; readonly slot: number; }
	| { readonly kind: "exchange-gold"; readonly amount: number; }
	| { readonly kind: "exchange-confirm"; }
	| { readonly kind: "exchange-cancel"; };

/*
================
exchangeRequest

The frame a command sends. The confirm button locks first and approves
once both sides locked (6B2280).
================
*/
export function exchangeRequest( state: ExchangeState, command: ExchangeCommand ): WireFrame {
	const frame = ( opcode: number, ...bytes: number[] ) => ({ opcode, payload: Uint8Array.from( bytes ) });
	switch ( command.kind ) {
		case "exchange-request": {
			if ( state.open || !Number.isInteger( command.gid ) || command.gid <= 0 ) {
				throw Error( "Exchange unavailable" );
			}
			const payload = new Uint8Array( 4 );
			new DataView( payload.buffer ).setUint32( 0, command.gid, true );
			return { opcode: OP_EXCHANGE_REQUEST, payload };
		}
		case "exchange-put":
			if ( !state.open || state.ownLocked ) throw Error( "Offer is locked" );
			return frame( OP_ITEM_MOVE, MOVE_EXCHANGE_PUT, command.slot );
		case "exchange-take":
			if ( !state.open || state.ownLocked || !state.own.some( row => row.slot === command.slot ) ) {
				throw Error( "Offer is locked" );
			}
			return frame( OP_ITEM_MOVE, MOVE_EXCHANGE_TAKE, command.slot );
		case "exchange-gold": {
			if ( !state.open || state.ownLocked || !Number.isInteger( command.amount ) || command.amount < 0 ) {
				throw Error( "Offer is locked" );
			}
			const payload = new Uint8Array( 5 );
			payload[0] = MOVE_EXCHANGE_GOLD;
			new DataView( payload.buffer ).setUint32( 1, Math.min( command.amount, 0xffffffff ), true );
			return { opcode: OP_ITEM_MOVE, payload };
		}
		case "exchange-confirm":
			if ( !state.open || state.approved ) throw Error( "Exchange unavailable" );
			if ( !state.ownLocked ) return frame( OP_EXCHANGE_CONFIRM );
			if ( !state.theirLocked ) throw Error( "Wait for the partner" );
			return frame( OP_EXCHANGE_APPROVE );
		case "exchange-cancel":
			if ( !state.open ) throw Error( "Exchange unavailable" );
			return frame( OP_EXCHANGE_CANCEL );
	}
}

/*
================
applyExchangeSwap

765260: the partner's items fill the first empty bag slots in exchange
slot order before the own offered slots empty.
================
*/
export function applyExchangeSwap(
	bag: ReadonlyMap<number, InventoryItem>,
	swap: NonNullable<ExchangeOutcome["swap"]>,
	firstBagSlot: number,
	endBagSlot: number
): Map<number, InventoryItem> {
	const next = new Map( bag );
	for ( const item of swap.receive ) {
		let slot = firstBagSlot;
		while ( slot < endBagSlot && next.has( slot ) ) slot++;
		if ( slot >= endBagSlot ) throw Error( "Exchange swap exceeds the bag" );
		next.set( slot, { ...item, slot } );
	}
	for ( const slot of swap.give ) next.delete( slot );
	return next;
}
