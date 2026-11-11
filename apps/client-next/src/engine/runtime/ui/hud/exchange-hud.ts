/*
===========================================================================

exchange-hud.ts - the exchange window's gold entry

CIFExchange's money button (resinfo\ifexchange.txt id 15) puts the typed
amount on the table; the text is clamped to the carried gold as it is
typed. The UI draws from this owner every frame. The window opening is
an edge the UI answers by showing the inventory tab, as
CGInterface_SetExchangeWindowVisible (69FBB0) does; closing hides only
the exchange.

The owner also holds the anti-fraud lines' edges (#3135): each line is
raised once per exchange, and both re-arm when the window closes.

===========================================================================
*/
import { EXCHANGE_DECEPTION_COMPARE_KEY, EXCHANGE_DECEPTION_REAFFIRM_KEY } from "@/engine/foundation/gameplay/exchange";

/*
================
createExchangeHud
================
*/
export function createExchangeHud() {
	let gold = "";
	let open = false;
	let compareShown = false;
	let reaffirmShown = false;
	return {
		/*
		================
		opened

		True on the frame the exchange window opens.
		================
		*/
		opened( now: boolean ): boolean {
			const rising = now && !open;
			open = now;
			return rising;
		},
		/*
		================
		gold
		================
		*/
		gold(): string {
			return gold;
		},
		/*
		================
		type

		Digits only, at most the carried balance.
		================
		*/
		type( raw: string, carried: number ) {
			const digits = raw.replace( /[^0-9]/g, "" ).slice( 0, 12 );
			gold = digits ? String( Math.min( carried, Number( digits ) ) ) : "";
		},
		/*
		================
		warnings

		The newly raised anti-fraud lines (inference, #3135; the rule is
		exchangePartnerIsStranger's): COMPARE once when an exchange with
		a stranger stands open, REAFFIRM once when both offers lock and
		the next confirm approves. stranger is null while the partner's
		name is unresolved - the compare edge waits rather than guessing,
		then latches either way. Call with open false to re-arm.
		================
		*/
		warnings( open: boolean, stranger: boolean | null, bothLocked: boolean ): readonly string[] {
			if ( !open ) {
				compareShown = false;
				reaffirmShown = false;
				return [];
			}
			const lines: string[] = [];
			if ( !compareShown && stranger !== null ) {
				compareShown = true;
				if ( stranger ) lines.push( EXCHANGE_DECEPTION_COMPARE_KEY );
			}
			if ( bothLocked && !reaffirmShown ) {
				reaffirmShown = true;
				lines.push( EXCHANGE_DECEPTION_REAFFIRM_KEY );
			}
			return lines;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			gold = "";
		}
	};
}
