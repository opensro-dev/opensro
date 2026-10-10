/*
===========================================================================

fortress-production-hud.ts - the smith's and trainer's production window state

The talk row (5D8C86) sends the 0x71E1 query; unlike the tax window, this
one opens only when the query answers (754A40 case 0x0D/0x11 calls
CGInterface_SetFortressMakeItemWindowVisible). The order, the count box
(MsgBoxMakeItem) and the cancel box (MsgBoxMakeItemCancel) live here; the
UI draws from this owner every frame and sends what a confirmed box asks.

===========================================================================
*/
import type { FortressState } from "@/engine/foundation/gameplay/fortress";
import {
	type FortressForgeItem,
	type FortressProductionOrder,
	type FortressStaff,
	fortressProductionCount,
	fortressProductionRemaining
} from "@/engine/foundation/gameplay/fortress-production";

// CIFFortressMakeItemWnd_OnCreate (65AD70): the list shows seven 32 px rows.
export const FORTRESS_PRODUCTION_ROWS = 7;

/*
================
FortressProductionQuestion

The open box: make (kind 0xC, the count edit starting empty) or cancel
(kind 0xD, the order's count shown disabled).
================
*/
export type FortressProductionQuestion =
	| { readonly kind: "make"; readonly item: FortressForgeItem; readonly count: string; }
	| { readonly kind: "cancel"; readonly order: FortressProductionOrder; };

/*
================
createFortressProductionHud

A window belongs to the staff member it was opened at; changing target or
ending the conversation closes it, as the talk window hides it.
================
*/
export function createFortressProductionHud() {
	let npc: number | null = null, staff: FortressStaff = "smith", fortress = 0, queryId = 0;
	let pending = false, open = false, top = 0;
	let order: FortressProductionOrder | null = null, question: FortressProductionQuestion | null = null;
	return {
		/*
		================
		request

		The query is in flight; the window waits for its answer.
		================
		*/
		request( gid: number, who: FortressStaff, id: number ) {
			this.close();
			npc = gid;
			staff = who;
			fortress = id;
			pending = true;
			return ++queryId;
		},
		/*
		================
		observe

		Folds each new 0xB1E1 answer for this staff member's actions. A
		refused query opens nothing; the worker reports the refusal.
		================
		*/
		observe( state: FortressState | undefined, target: number | undefined, talking: boolean ) {
			if ( !pending && !open ) return;
			if ( target !== npc || !talking ) {
				this.close();
				return;
			}
			const snapshot = state?.production?.[staff];
			if ( !snapshot ) return;
			if ( pending ) {
				const query = snapshot.query;
				if ( !query || query.id !== queryId ) return;
				if ( query.reply.result !== 1 ) {
					this.close();
					return;
				}
				if ( query.reply.fortress !== fortress ) return;
				pending = false;
				open = true;
			}
			if ( snapshot.fortress === fortress ) order = snapshot.order;
		},
		/*
		================
		npc

		The staff member whose window is shown, or null.
		================
		*/
		npc(): number | null {
			return open ? npc : null;
		},
		/*
		================
		staff
		================
		*/
		staff(): FortressStaff {
			return staff;
		},
		/*
		================
		fortress
		================
		*/
		fortress() {
			return fortress;
		},
		/*
		================
		order
		================
		*/
		order() {
			return order;
		},
		/*
		================
		done

		65A5E0 counts the order down locally and shows it complete at zero,
		before the server says so.
		================
		*/
		done( nowMs: number ) {
			return !!order && (order.done || fortressProductionRemaining( order, nowMs ) === 0);
		},
		/*
		================
		top
		================
		*/
		top() {
			return top;
		},
		/*
		================
		scroll
		================
		*/
		scroll( delta: number, rows: number ) {
			top = Math.max( 0, Math.min( Math.max( 0, rows - FORTRESS_PRODUCTION_ROWS ), top + delta ) );
		},
		/*
		================
		askMake

		The row's make button (65C6D0 opens kind 0xC; 52C870 mode 0xA starts
		the edit empty).
		================
		*/
		askMake( item: FortressForgeItem ) {
			if ( open && !order ) question = { kind: "make", item, count: "" };
		},
		/*
		================
		askCancel

		CIFFortressMakeItemWnd_OnCancelClicked (656DC0).
		================
		*/
		askCancel() {
			if ( open && order ) question = { kind: "cancel", order };
		},
		/*
		================
		edit
		================
		*/
		edit( value: string ) {
			if ( question?.kind === "make" ) question = { ...question, count: fortressProductionCount( value ) };
		},
		/*
		================
		question
		================
		*/
		question(): FortressProductionQuestion | null {
			return question;
		},
		/*
		================
		takeQuestion
		================
		*/
		takeQuestion(): FortressProductionQuestion | null {
			const asked = question;
			question = null;
			return asked;
		},
		/*
		================
		close
		================
		*/
		close() {
			npc = null;
			pending = false;
			open = false;
			order = null;
			question = null;
			top = 0;
		}
	};
}
