/*
===========================================================================

specialty-deal-hud.ts - the trade goods window's state and its request loop

CIFSpecialtyDeal opens on a trade goods move between a specialty shop and
the summoned transport: a purchase (CIFSpecialtyDeal_OpenForPackageItem
64A0E0, +0x7C8 = 1) or a sale (CIFSpecialtyDeal_OpenForSaleItem 6496E0,
+0x7C8 = 0). It holds the typed quantity and, once confirmed, the dealing
loop of CIFSpecialtyDeal_RequestTradeGoodsMove (64A660): one request of at
most a stack at a time until the moved total reaches the target.

The window learns what each request moved from the transport's own stock
of the item, read after the worker settles the request; native counts the
moved quantity CPSMission_ApplyInventoryOperation (756CF0) reports, which
is the same number. A request goes sent -> pending (the worker took it) ->
settled (pending clears). One that moves nothing ends the deal, and one the
worker never takes within 5000 ms ends it too, as native's state timer
(64A660, 0x1388) closes a stalled deal.

===========================================================================
*/

import { dealChunk } from "@/engine/foundation/gameplay/specialty-deal";
import type { MerchantSelection } from "@/engine/foundation/ui/merchant";

// 64A660: CIObject_ScheduleStateTimer( 1, 0x1388 ) after each request.
export const DEAL_REQUEST_TIMEOUT_MS = 5000;

/*
================
SpecialtyDealOpen
================
*/
export interface SpecialtyDealOpen {
	readonly mode: "buy" | "sell";
	readonly selection: MerchantSelection;
	readonly name: string;
	readonly refObjId: number;
	readonly cosGid: number;
	// +0x7D0: the item's unit buy price.
	readonly unitBuy: number;
	// The most one request moves: a shop stack, or the sold slot's count.
	readonly stack: number;
	readonly count: number;
}

/*
================
SpecialtyDealState
================
*/
export interface SpecialtyDealState extends SpecialtyDealOpen {
	readonly text: string;
	// Set once confirmed: the target, what has moved, and the stock the
	// request in flight started from.
	readonly dealing: {
		readonly target: number;
		readonly moved: number;
		readonly baseline: number;
		readonly phase: "sent" | "pending";
		readonly sentAt: number;
	} | null;
}

/*
================
createSpecialtyDealHud
================
*/
export function createSpecialtyDealHud() {
	let deal: SpecialtyDealState | null = null;
	return {
		/*
		================
		open
		================
		*/
		open( request: SpecialtyDealOpen ) {
			deal = { ...request, text: String( request.count ), dealing: null };
		},
		/*
		================
		state
		================
		*/
		state(): SpecialtyDealState | null {
			return deal;
		},
		/*
		================
		close
		================
		*/
		close() {
			deal = null;
		},
		/*
		================
		type

		CIFEdit numeric entry (649D20 re-parses on every change); the edit's
		limit (gold / unit buy, or the sold count) caps it.
		================
		*/
		type( text: string, limit: number ) {
			if ( !deal || deal.dealing ) return;
			const digits = text.replace( /\D/g, "" ).slice( 0, 12 );
			const value = digits === "" ? 0 : Math.min( Number( digits ), Math.max( 0, limit ) );
			deal = { ...deal, text: digits === "" ? "" : String( value ) };
		},
		/*
		================
		count
		================
		*/
		count(): number {
			return deal && /^\d+$/.test( deal.text ) ? Number( deal.text ) : 0;
		},
		/*
		================
		confirm

		CIFSpecialtyDeal_OnConfirm (64A8F0): nothing to move closes the window;
		otherwise the loop starts from the transport's current stock and the
		first request's quantity is returned. More than one stack raises the
		"currently purchasing" notice (CIFSpecialtyDeal_ShowAllBuyProgressBox).
		================
		*/
		confirm( stock: number, now: number ): { readonly chunk: number; readonly progress: boolean; } | null {
			if ( !deal || deal.dealing ) return null;
			const target = /^\d+$/.test( deal.text ) ? Number( deal.text ) : 0;
			if ( target <= 0 ) {
				deal = null;
				return null;
			}
			const chunk = dealChunk( target, 0, deal.stack );
			deal = { ...deal, dealing: { target, moved: 0, baseline: stock, phase: "sent", sentAt: now } };
			return { chunk, progress: target > deal.stack };
		},
		/*
		================
		observe

		One frame of the loop: the worker's pending flag and the transport's
		stock. Returns the next request's quantity once the one in flight has
		settled (the caller sends it), 0 when the deal ended (the window
		closed), or null while it waits.
		================
		*/
		observe( pending: boolean, stock: number, now: number ): number | null {
			const dealing = deal?.dealing;
			if ( !deal || !dealing ) return null;
			if ( dealing.phase === "sent" ) {
				if ( pending ) {
					deal = { ...deal, dealing: { ...dealing, phase: "pending" } };
					return null;
				}
				if ( now - dealing.sentAt < DEAL_REQUEST_TIMEOUT_MS ) return null;
				deal = null;
				return 0;
			}
			if ( pending ) return null;
			const delta = Math.abs( stock - dealing.baseline );
			const moved = Math.min( dealing.target, dealing.moved + delta );
			const next = delta === 0 ? 0 : dealChunk( dealing.target, moved, deal.stack );
			if ( next === 0 ) {
				deal = null;
				return 0;
			}
			deal = {
				...deal,
				dealing: { target: dealing.target, moved, baseline: stock, phase: "sent", sentAt: now }
			};
			return next;
		}
	};
}

export type SpecialtyDealHud = ReturnType<typeof createSpecialtyDealHud>;
