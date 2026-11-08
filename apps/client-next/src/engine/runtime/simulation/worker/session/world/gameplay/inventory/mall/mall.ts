/*
===========================================================================

mall.ts - one outstanding mall request and its authoritative completion

A purchase stages its currency and inventory projection until the native
receipt matches. A timed-out purchase requires resynchronization, never retry.

===========================================================================
*/
import type { MallSilkHistoryState, MallState } from "@/engine/contracts/item-mall";
import { INVENTORY_TRANSACTION_TIMEOUT } from "@/engine/contracts/network";
import { MALL_CATALOG_CONTROL, MALL_SILK_HISTORY_CONTROL } from "@/engine/foundation/gameplay/commerce-controls";
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

// The Buying list's periods, the native combo's order: 1 day, 7 days,
// 1 month, 3 months (CIFSilkBuyList OnCreate 0x6CED10). The server maps
// the index to days; the wire carries the index only.
const SILK_HISTORY_PERIODS = 4;
// The server's answer cap (action mallSilkHistoryLimit).
const SILK_HISTORY_MAX_ROWS = 100;

/*
================
decodeSilkHistory

Strictly decode a history answer: exactly version, period, total and rows,
each row exactly at and charged. The caller drops answers that do not
match the outstanding inquiry.
================
*/
function decodeSilkHistory( payload: Uint8Array ): MallSilkHistoryState {
	const decoded: unknown = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( payload ) );
	if ( !decoded || typeof decoded !== "object" || Array.isArray( decoded ) ) throw Error( "Invalid silk history" );
	const row = decoded as Record<string, unknown>;
	const keys = Object.keys( row ).sort().join( "," );
	const uint = ( value: unknown ) =>
		typeof value === "number" && Number.isInteger( value ) && value >= 0 && value <= MAX_BALANCE;
	if (
		keys !== "period,rows,total,version" || row.version !== 1 || !uint( row.period ) ||
		(row.period as number) >= SILK_HISTORY_PERIODS || !uint( row.total ) || !Array.isArray( row.rows ) ||
		row.rows.length > SILK_HISTORY_MAX_ROWS
	) throw Error( "Invalid silk history" );
	const rows = row.rows.map( entry => {
		if ( !entry || typeof entry !== "object" || Array.isArray( entry ) ) throw Error( "Invalid silk history" );
		const use = entry as Record<string, unknown>;
		if ( Object.keys( use ).sort().join( "," ) !== "at,charged" || !uint( use.at ) || !uint( use.charged ) ) {
			throw Error( "Invalid silk history" );
		}
		return { at: use.at as number, charged: use.charged as number };
	} );
	return { period: row.period as number, total: row.total as number, rows, pending: false };
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
	// The latest deadline a hold may reach, fixed by the request's first hold.
	let holdLimit = 0;
	// When the request's first reference hold arrived (#2148): the timeout
	// report stamps how long before the deadline it landed.
	let firstHeldAt: number | undefined;
	let revision = 0;
	let expired = false;
	// The Buying list's last inquiry. Its lane is deliberately outside the
	// purchase request above: a lost history answer must never expire the
	// session, so step() only unpends it.
	let silk: MallSilkHistoryState | undefined;
	let silkDeadline = 0;

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
			holdLimit = 0;
			firstHeldAt = undefined;
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
			holdLimit = 0;
			firstHeldAt = undefined;
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
			// A projection answers the one outstanding request. One that
			// arrives when no request is outstanding - a scene reset at
			// the next bootstrap retired it, or a resume re-delivered an
			// answer already applied - belongs to a scene the client has
			// left: set it aside, never end the session over it (#2864).
			// The same holds behind a staged purchase delivery, whose
			// receipt is still owed.
			if ( !request || staged ) return undefined;
			if ( expired ) throw Error( "Unexpected mall projection" );
			const decoded = mallProjection( payload );
			if ( decoded.error !== undefined ) {
				// The entering refusal retires a catalogue request with
				// its reason shown. A purchase waits on its own receipt
				// lane and is never retired by it.
				if ( request === "catalog" ) {
					state = { ...decoded.state, error: decoded.error, revision: ++revision };
					request = null;
				}
				return undefined;
			}
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
			// Revision acknowledges a request to the HUD's purchase queue. A wallet
			// publication changes the displayed balance without completing that request.
			state = { ...state, ...next };
		},
		/*
  ================
  inquireSilkHistory

  Ask for one period's recorded uses. Re-asking supersedes the outstanding
  inquiry; the answer's period echo is what accepts it. Asking again for
  the same period keeps its rows on screen while it pends.
  ================
  */
		inquireSilkHistory( period: number, now: number ) {
			if ( !Number.isInteger( period ) || period < 0 || period >= SILK_HISTORY_PERIODS ) {
				throw Error( "Invalid silk history period" );
			}
			const keep = silk?.period === period ? silk : undefined;
			silk = { period, total: keep?.total ?? 0, rows: keep?.rows ?? [], pending: true };
			silkDeadline = now + REQUEST_TIMEOUT_MS;
			const payload = new TextEncoder().encode( JSON.stringify( { period } ) );
			return { opcode: MALL_SILK_HISTORY_CONTROL, payload };
		},
		/*
  ================
  silkHistoryAnswer

  A history answer (MALL_SILK_HISTORY_CONTROL). One arriving with no
  inquiry outstanding, or echoing a superseded period, is dropped: the
  newest inquiry owns the window. A malformed answer still throws, as
  every wire violation in this session does.
  ================
  */
		silkHistoryAnswer( payload: Uint8Array ) {
			const answer = decodeSilkHistory( payload );
			if ( !silk?.pending || answer.period !== silk.period ) return;
			silk = answer;
			silkDeadline = 0;
		},
		/*
  ================
  silkHistory
  ================
  */
		silkHistory() {
			return silk;
		},
		/*
  ================
  holdUntil
  ================
  */
		holdUntil( now: number, until: number, limit: number ) {
			if ( !request ) return;
			if ( !holdLimit ) firstHeldAt = now;
			holdLimit ||= limit;
			deadline = Math.max( deadline, Math.min( until, holdLimit ) );
		},
		/*
  ================
  step
  ================
  */
		step( now: number ) {
			if ( silk?.pending && now >= silkDeadline ) {
				silk = { ...silk, pending: false };
				silkDeadline = 0;
			}
			if ( request && now >= deadline ) expired = true;
			// The purchase lane's answer is an item result: a lost one resumes the
			// session as an inventory transaction does (world.ts lostAnswer).
			if ( expired ) {
				throw Error(
					`Mall request timed out; ${
						firstHeldAt === undefined ?
							"held=0" :
							`held=1 firstHoldAgeMs=${Math.round( now - firstHeldAt )}`
					}; reconnect to resynchronize`,
					{
						cause: INVENTORY_TRANSACTION_TIMEOUT
					}
				);
			}
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
			silk = undefined;
			silkDeadline = 0;
		}
	};
}
