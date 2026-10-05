/*
===========================================================================

exchange-hud.ts - the exchange window's gold entry

CIFExchange's money button (resinfo\ifexchange.txt id 15) puts the typed
amount on the table; the text is clamped to the carried gold as it is
typed. The UI draws from this owner every frame. The window opening is
an edge the UI answers by showing the inventory tab, as
CGInterface_SetExchangeWindowVisible (69FBB0) does; closing hides only
the exchange.

===========================================================================
*/

/*
================
createExchangeHud
================
*/
export function createExchangeHud() {
	let gold = "";
	let open = false;
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
		reset
		================
		*/
		reset() {
			gold = "";
		}
	};
}
