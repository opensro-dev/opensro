/*
===========================================================================

port-shop-copy.ts - shop tab labels the port adds (port-only, not native)

A shop tab names its label by a text symbol, and the v1.150 tables have
no row for a tab the port synthesizes. Jangan's alchemy shop (server
commerce/alchemyshop.go, SRO_JANGAN_ALCHEMY_SHOP; #4684) sells the
degree 10 stones on a tab of their own, and the client's degree tab names
(SN_TAB_MAGICTABLET_1 to _6, "1st degree" to "6th degree") stop at the
6th, so the label is carried here in their voice. The shop window
(ui.ts hudCopy) and the site catalogue (buildCatalogue.mjs) both read it.

===========================================================================
*/

// Port tab-label symbol -> its English text.
const PORT_SHOP_TAB_COPY = {
	UIIT_PORT_ALCHEMY_D10_STONES: "10th degree"
} as const;

/*
================
portShopTabCopy

The text of a port shop tab symbol; "" for any other symbol.
================
*/
export function portShopTabCopy( symbol: string ): string {
	return Object.hasOwn( PORT_SHOP_TAB_COPY, symbol ) ?
		PORT_SHOP_TAB_COPY[symbol as keyof typeof PORT_SHOP_TAB_COPY] :
		"";
}
