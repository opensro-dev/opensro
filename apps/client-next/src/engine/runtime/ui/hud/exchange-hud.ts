/*
===========================================================================

exchange-hud.ts - the exchange window's gold entry

CIFExchange's money button (resinfo\ifexchange.txt id 15) puts the typed
amount on the table; the text is clamped to the carried gold as it is
typed. The UI draws from this owner every frame.

===========================================================================
*/

/*
================
createExchangeHud
================
*/
export function createExchangeHud() {
	let gold = "";
	return {
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
		reset
		================
		*/
		reset() {
			gold = "";
		}
	};
}
