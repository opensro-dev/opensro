/*
===========================================================================

cos-sale.ts - selling the active transport back at the stable keeper

The v1.150 client sells the player's active COS when its status portrait
(control message 0x27) is dropped on a store window. CIFMerchant_OnSlotTransfer
(5B6CA0) refuses in a fixed order before it opens the sale box
(CIFCOSStatus_OpenUseItemQuantityDialog 6A9F50); the box's OK sends 0x71DC
[u32 interaction target][u8 3] (6A9E30 -> 6FF980). This module owns that
order: the UI hands it what it knows and draws the verdict.

The server re-checks every rule (cossell.go), in its own order; the
client's comes first, so this is the order a player sees.

===========================================================================
*/
import type { CosRecord } from "@/engine/contracts/gameplay";
import type { CosReference } from "./cos-command";
import { MERCHANT_NOTICE_CATEGORY } from "./merchant";

// CGInterface_GetInteractionLock bit 0x400: the conversation NPC deals in
// transports (its talk capability, copied at 75AEBC).
export const COS_DEALER_CAPABILITY = 0x400;
// The 0x71DC sub-command the sale box sends.
export const COS_SALE_KIND = 3;

// {1, 0x45} UIIT_MSG_STRGERR_DEALER_DONT_TREAT_THIS_ITEM.
const DEALER_REFUSES = 0x45;
// Category 12, the COS notices.
const COS_NOTICE_CATEGORY = 0xc;
// UIIT_MSG_COSERR_CANT_SELL_RIDING_COS / _REQUESTED_COS / _COS_HAVE_ITEMS.
const CANT_SELL_RIDING = 0x9;
const CANT_SELL_REQUESTED = 0xa;
const CANT_SELL_HAVE_ITEMS = 0xb;

/*
================
CosSaleInput

What the drop knows: the conversation NPC's talk capabilities, whether the
player rides, and the active COS's record and reference (undefined when
there is none).
================
*/
export interface CosSaleInput {
	readonly capabilities: number;
	readonly riding: boolean;
	readonly record: CosRecord | undefined;
	readonly reference: CosReference | undefined;
}

export type CosSaleVerdict =
	| { readonly kind: "refuse"; readonly category: number; readonly code: number; }
	| { readonly kind: "confirm"; readonly gid: number; readonly refObjId: number; readonly price: number; };

/*
================
cosSaleVerdict

5B6CA0's chain for message 0x27:

	lock 0x400 clear               -> {1, 0x45}
	[player+0x434] == 1 (riding)   -> {0xC, 9}
	no record for the active COS   -> {0xC, 0xA}
	its bag holds anything         -> {0xC, 0xB}
	its row's CanSell (+0xA5) is 0 -> {0xC, 0xA}

and otherwise the sale box, at the row's SellPrice.
================
*/
export function cosSaleVerdict( input: CosSaleInput ): CosSaleVerdict {
	if ( (input.capabilities & COS_DEALER_CAPABILITY) === 0 ) {
		return { kind: "refuse", category: MERCHANT_NOTICE_CATEGORY, code: DEALER_REFUSES };
	}
	if ( input.riding ) return { kind: "refuse", category: COS_NOTICE_CATEGORY, code: CANT_SELL_RIDING };
	const record = input.record;
	if ( !record ) return { kind: "refuse", category: COS_NOTICE_CATEGORY, code: CANT_SELL_REQUESTED };
	if ( (record.inventory?.length ?? 0) > 0 ) {
		return { kind: "refuse", category: COS_NOTICE_CATEGORY, code: CANT_SELL_HAVE_ITEMS };
	}
	const price = input.reference?.sellPrice;
	if ( price === undefined ) return { kind: "refuse", category: COS_NOTICE_CATEGORY, code: CANT_SELL_REQUESTED };
	return { kind: "confirm", gid: record.gid, refObjId: record.refObjId, price };
}

/*
================
cosSaleRequest

The 0x71DC body: the interaction target's gid, then the sale kind.
================
*/
export function cosSaleRequest( npcGid: number ): Uint8Array {
	const payload = new Uint8Array( 5 );
	new DataView( payload.buffer ).setUint32( 0, npcGid >>> 0, true );
	payload[4] = COS_SALE_KIND;
	return payload;
}

/*
================
cosSaleResult

CPSMission_OnCosSellResult0xB1DC (7782E0): [1][kind] is a sale (the despawn
retires the actor); [2][code] is one category-12 notice. Anything else is
a malformed frame.
================
*/
export function cosSaleResult( payload: Uint8Array ): { readonly sold: true; } | { readonly sold: false; readonly code: number; } {
	if ( payload.length !== 2 || (payload[0] !== 1 && payload[0] !== 2) ) throw Error( "Invalid COS sale result" );
	return payload[0] === 1 ? { sold: true } : { sold: false, code: payload[1]! };
}
