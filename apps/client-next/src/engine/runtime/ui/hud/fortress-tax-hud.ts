/*
===========================================================================

fortress-tax-hud.ts - the fortress manager's tax management window state

The manager's first row (5D8930 action 0x33 row 1) sends the 0x71E1 query
and shows CIFTaxManagement at once; the 0xB1E1 answers fill and update it
(754A40 cases 0..2). The slider and the two confirm boxes
(CIFTaxManagement_OpenConfirmMsgBox 665BA0) live here; the UI draws from
this owner every frame and sends what a confirmed box asks for.

===========================================================================
*/
import type { FortressState } from "@/engine/foundation/gameplay/fortress";

// 664C00 shows the slider position minus 20; the slider spans 0..40.
export const FORTRESS_TAX_MIN = -20;
export const FORTRESS_TAX_MAX = 20;

/*
================
FortressTaxContext

The query's answer (CIFTaxManagement_SetContext 665D80): the fortress, its
ratio and its treasury. Gold stays a decimal string, as the wire does.
================
*/
export interface FortressTaxContext {
	readonly fortress: number;
	readonly rate: number;
	readonly gold: string;
}

/*
================
FortressTaxQuestion

The open confirm box: MsgBoxTaxModify (kind 0xA) carries the current and
the slider's ratio; MsgBoxTaxLevy (kind 0xB) carries the amount being typed.
================
*/
export type FortressTaxQuestion =
	| { readonly kind: "rate"; readonly from: number; readonly to: number; }
	| { readonly kind: "collect"; readonly amount: string; };

/*
================
fortressTaxAmount

The levy edit is numeric and capped at the treasury (52C870 mode 9,
CIFEdit_SetNumericLimit64): digits only, an oversized draft becomes the
treasury.
================
*/
export function fortressTaxAmount( draft: string, gold: string ): string {
	const digits = draft.replace( /[^0-9]/g, "" ).replace( /^0+(?=\d)/, "" );
	if ( !digits ) return "";
	const limit = BigInt( /^\d+$/.test( gold ) ? gold : "0" );
	return BigInt( digits ) > limit ? limit.toString() : digits;
}

/*
================
createFortressTaxHud

A window belongs to the manager it was opened at; changing target or
ending the conversation closes it (sub_5dbe00 hides it with the talk
window).
================
*/
export function createFortressTaxHud() {
	let npc: number | null = null, fortress = 0, seen = 0, open = false;
	// 664DA0 creates the slider at position 0, which reads -20 until a query answers.
	let context: FortressTaxContext | null = null, draft = FORTRESS_TAX_MIN;
	let question: FortressTaxQuestion | null = null;
	return {
		/*
		================
		request

		The query is in flight; native shows the empty window immediately.
		================
		*/
		request( gid: number, id: number, sequence: number ) {
			npc = gid;
			fortress = id;
			seen = sequence;
			open = true;
			context = null;
			draft = FORTRESS_TAX_MIN;
			question = null;
		},
		/*
		================
		observe

		Folds each new 0xB1E1 answer for actions 0..2. A refusal changes
		nothing here; the worker reports it.
		================
		*/
		observe( state: FortressState | undefined, target: number | undefined, talking: boolean ) {
			if ( !open ) return;
			if ( target !== npc || !talking ) {
				this.close();
				return;
			}
			const service = state?.service;
			if ( !service || state?.serviceSequence === seen ) return;
			seen = state?.serviceSequence ?? 0;
			if ( service.result !== 1 || service.action > 2 ) return;
			if ( service.action === 0 ) {
				context = {
					fortress: service.fortress ?? fortress,
					rate: service.taxRate ?? 0,
					gold: service.gold ?? "0"
				};
				draft = context.rate;
			} else if ( context && service.action === 1 ) {
				// 665730 stores the ratio and refreshes, which moves the slider back.
				context = { ...context, rate: service.taxRate ?? context.rate };
				draft = context.rate;
			} else if ( context && service.action === 2 ) {
				// 665850 subtracts the collected gold from the shown treasury.
				const left = BigInt( context.gold ) - BigInt( service.gold ?? "0" );
				context = { ...context, gold: left.toString() };
			}
		},
		/*
		================
		npc
		================
		*/
		npc(): number | null {
			return open ? npc : null;
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
		context
		================
		*/
		context() {
			return context;
		},
		/*
		================
		draft

		The slider's ratio, -20..20.
		================
		*/
		draft() {
			return draft;
		},
		/*
		================
		slide
		================
		*/
		slide( rate: number ) {
			if ( !context || !Number.isFinite( rate ) ) return;
			draft = Math.max( FORTRESS_TAX_MIN, Math.min( FORTRESS_TAX_MAX, Math.round( rate ) ) );
		},
		/*
		================
		askRate

		CIFTaxManagement_OnChangeRateClicked (665D50).
		================
		*/
		askRate() {
			if ( context ) question = { kind: "rate", from: context.rate, to: draft };
		},
		/*
		================
		askCollect

		CIFTaxManagement_OnCollectClicked (665D30): the edit starts at the
		whole treasury (52A7E0).
		================
		*/
		askCollect() {
			if ( context ) question = { kind: "collect", amount: fortressTaxAmount( context.gold, context.gold ) };
		},
		/*
		================
		edit
		================
		*/
		edit( value: string ) {
			if ( question?.kind === "collect" && context ) {
				question = { kind: "collect", amount: fortressTaxAmount( value, context.gold ) };
			}
		},
		/*
		================
		question
		================
		*/
		question(): FortressTaxQuestion | null {
			return question;
		},
		/*
		================
		takeQuestion
		================
		*/
		takeQuestion(): FortressTaxQuestion | null {
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
			open = false;
			context = null;
			question = null;
			draft = FORTRESS_TAX_MIN;
		}
	};
}
