/*
===========================================================================

mall.ts - one outstanding mall request and its authoritative completion

A purchase stages its currency and inventory projection until the native
receipt matches. A timed-out purchase requires resynchronization, never retry.

===========================================================================
*/
import type { MallState } from "@/engine/contracts/item-mall";
import { MALL_CATALOG_CONTROL } from "@/engine/foundation/gameplay/commerce-controls";
import { mallProjection } from "@/engine/foundation/gameplay/item-mall-catalog";
import { mallPurchasePayload, type MallPurchase } from "@/engine/foundation/gameplay/item-mall-wire";

const ITEM_MOVE_REQUEST = 0x706d;
const REQUEST_TIMEOUT_MS = 10000;

// A wallet field is the server's uint32 (domain.MallBalance).
const MAX_BALANCE = 0xffffffff;

/*
================
mallBalance

Strictly decode a balance push: exactly silk, giftSilk and points.
================
*/
function mallBalance( payload: Uint8Array ): { silk: number; giftSilk: number; points: number; } {
	const decoded: unknown = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( payload ) );
	if ( !decoded || typeof decoded !== "object" || Array.isArray( decoded ) ) throw Error( "Invalid mall balance" );
	const row = decoded as Record<string, unknown>;
	const keys = Object.keys( row ).sort().join( "," );
	const field = ( value: unknown ) =>
		typeof value === "number" && Number.isInteger( value ) && value >= 0 && value <= MAX_BALANCE;
	if ( keys !== "giftSilk,points,silk" || !field( row.silk ) || !field( row.giftSilk ) || !field( row.points ) ) {
		throw Error( "Invalid mall balance" );
	}
	return { silk: row.silk as number, giftSilk: row.giftSilk as number, points: row.points as number };
}

/*
================
createMall
================
*/
export function createMall() {
	let state: MallState | undefined;
	let request: MallPurchase | "catalog" | null = null;
	let staged: MallState | undefined;
	let deadline = 0;
	let revision = 0;
	let expired = false;

	return {
		/*
  ================
  open
  ================
  */
		open( now: number ) {
			if ( request || expired ) throw Error( "Mall request unavailable" );
			const frame = { opcode: MALL_CATALOG_CONTROL, payload: new Uint8Array() };
			request = "catalog";
			deadline = now + REQUEST_TIMEOUT_MS;
			if ( state ) state = { ...state, pending: true, error: undefined };
			return frame;
		},
		/*
  ================
  purchase
  ================
  */
		purchase( next: MallPurchase, now: number ) {
			if ( !state || request || expired ) throw Error( "Mall purchase unavailable" );
			const payload = mallPurchasePayload( next );
			const offer = state.offers.find( row =>
				row.group === next.group && row.shop === next.shop && row.tab === next.tab && row.slot === next.slot &&
				row.packageId === next.packageId
			);
			if (
				!offer || next.quantity > offer.purchaseLimit || next.points > state.points ||
				next.points > offer.silk * next.quantity || next.points !== 0 && !offer.allowsPoints ||
				offer.silk * next.quantity - next.points > state.silk || offer.giftSilk * next.quantity > state.giftSilk
			) throw Error( "Mall purchase exceeds current authority" );
			request = { ...next };
			deadline = now + REQUEST_TIMEOUT_MS;
			staged = undefined;
			state = { ...state, pending: true, error: undefined };
			const frame = { opcode: ITEM_MOVE_REQUEST, payload };
			return frame;
		},
		/*
  ================
  projection
  ================
  */
		projection( payload: Uint8Array ): unknown {
			if ( !request || staged || expired ) throw Error( "Unexpected mall projection" );
			const decoded = mallProjection( payload );
			if ( request === "catalog" ) {
				if ( decoded.items !== undefined ) throw Error( "Unsolicited mall delivery" );
				state = { ...decoded.state, revision: ++revision };
				request = null;
				return undefined;
			}
			if ( !Array.isArray( decoded.items ) || decoded.items.length === 0 ) throw Error( "Missing mall delivery" );
			staged = decoded.state;
			return decoded.items;
		},
		/*
  ================
  acknowledge
  ================
  */
		acknowledge( payload: Uint8Array, slots: readonly number[] ) {
			if (
				!request || request === "catalog" || !staged || expired || payload.length < 11 || payload[0] !== 1 ||
				payload[1] !== 0x18 || payload.length !== 10 + payload[7]!
			) throw Error( "Unmatched mall receipt" );
			const view = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
			if (
				view.getUint16( 2, true ) !== request.group || payload[4] !== request.shop ||
				payload[5] !== request.tab || payload[6] !== request.slot ||
				view.getUint16( payload.length - 2, true ) !== request.quantity
			) throw Error( "Mall receipt identity mismatch" );
			const destinations = [ ...payload.subarray( 8, payload.length - 2 ) ];
			if (
				destinations.some( slot => !slots.includes( slot ) ) ||
				slots.some( slot => !destinations.includes( slot ) )
			) throw Error( "Mall delivery differs from receipt" );
			state = { ...staged, revision: ++revision };
			request = null;
			staged = undefined;
		},
		/*
  ================
  reject
  ================
  */
		reject( payload: Uint8Array ) {
			if ( !request ) return false;
			if ( payload.length !== 2 || payload[0] !== 2 || staged ) throw Error( "Invalid mall rejection" );
			if ( state ) {
				state = {
					...state,
					pending: false,
					error: `Mall purchase rejected: ${payload[1]}`,
					revision: ++revision
				};
			}
			request = null;
			return true;
		},
		/*
  ================
  balance

  A balance push (MALL_BALANCE_CONTROL): the beta credited earned silk to
  the account. It updates an open mall's balance in place; with no mall
  state yet there is nothing to show, and the next catalog carries it.
  ================
  */
		balance( payload: Uint8Array ) {
			const next = mallBalance( payload );
			if ( !state ) return;
			state = { ...state, ...next, revision: ++revision };
		},
		/*
  ================
  step
  ================
  */
		step( now: number ) {
			if ( request && now >= deadline ) expired = true;
			if ( expired ) throw Error( "Mall request timed out; reconnect to resynchronize" );
		},
		/*
  ================
  pending
  ================
  */
		pending() {
			return request !== null;
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
  reset
  ================
  */
		reset() {
			state = undefined;
			request = null;
			staged = undefined;
			deadline = 0;
			revision = 0;
			expired = false;
		}
	};
}
