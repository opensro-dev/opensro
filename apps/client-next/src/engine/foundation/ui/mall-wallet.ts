/*
===========================================================================

mall-wallet.ts - the Silk wallet's captions and hover help (port-only)

The port's mall wallet holds three Silk balances (#4133), and players
could not tell them apart (#4480). This module is the one owner of what
each balance is called and what its hover help says; the classic Item
Mall (runtime/ui/ui.ts) and the renewal Item Mall
(runtime/ui/hud/renewal-mall-view.ts) both draw from it.

  * Silk: the starter Silk, staff grants and consignment market sales
    (store creditSilkTx). Mall goods it pays for are account-bound.
  * Earned Silk: the hourly play credit (action/betasilk.go). Purchases
    spend it first; a purchase paid wholly with it is tradable while the
    shard allows it (SRO_EARNED_SILK_TRADABLE, item-mall-layout.ts).
  * Bonus Silk: rewards such as referrals (store creditGiftSilkTx). The
    Item Mall takes it; the consignment market never does.

Not native: no v1.150 string names earned Silk or explains a balance, so
the earned caption and every help line are the port's own. Bonus Silk
keeps the client's own caption (UIIT_CTL_SILK_INQUIRY_GIFT_SILK, "Bonus
Silk"), the name the purchase dialog already prints.

The hourly rate and the cap are shard settings, so no help names them.

===========================================================================
*/

// The earned row's caption, cased like the client's "Bonus Silk".
export const EARNED_SILK_CAPTION = "Earned Silk";

// The client's own caption for the gift balance: "Bonus Silk".
export const BONUS_SILK_CAPTION_KEY = "UIIT_CTL_SILK_INQUIRY_GIFT_SILK";

export type SilkWalletKind = "silk" | "earned" | "bonus";

const SILK_HELP =
	"Silk: your main balance, from starter Silk, staff grants and your consignment market sales. Item Mall goods it pays for are account-bound.";
const EARNED_SILK_HELP = "Earned Silk: Silk you earn by playing in the world. Purchases spend it first.";
const EARNED_SILK_TRADABLE_HELP = " Goods paid for wholly with Earned Silk can be traded.";
const BONUS_SILK_HELP =
	"Bonus Silk: Silk from rewards such as referrals. Spend it in the Item Mall; the consignment market does not take it.";

/*
================
silkWalletHelp

The hover help for one wallet row. earnedTradable is the shard's earned
purchase rule (the mall state's earnedTradable): only while it holds does
the earned help promise tradable goods.
================
*/
export function silkWalletHelp( kind: SilkWalletKind, earnedTradable: boolean ): string {
	if ( kind === "silk" ) return SILK_HELP;
	if ( kind === "bonus" ) return BONUS_SILK_HELP;
	return earnedTradable ? EARNED_SILK_HELP + EARNED_SILK_TRADABLE_HELP : EARNED_SILK_HELP;
}
