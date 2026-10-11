/*
===========================================================================

cos-distance-notice.ts - the COS tether notice (0x342F)

CPSMission_OnCosTradeDistanceError0x342F (75DF40): the server refused a
step that led the player too far from a COS that holds it. One byte names
the COS: 1 the parked trade transport (100 m), 2 the capture-quest monster
(30 m). Each formats its distance into the localized text, shows the
formatted text as the notice banner and prints it in the system chat, the
same presentation as the category 12 code 8 COS error (68A489).

===========================================================================
*/
import type { SystemNotice } from "./system-notices";

export const COS_DISTANCE_ERROR = 0x342f;

// 75DF40's swprintf arguments.
const TRADE_CART_METRES = 100;
const CAPTURED_MONSTER_METRES = 30;

/*
================
cosDistanceNotice

Decode one 0x342F; any reason byte but 1 and 2 shows nothing, as natively.
================
*/
export function cosDistanceNotice( p: Uint8Array ): SystemNotice | null {
	if ( p.length !== 1 ) throw Error( "Invalid COS distance error length" );
	const reason = p[0];
	const key = reason === 1 ?
		"UIIT_MSG_COSERR_TOO_FAR_FROM_TRADECART" :
		reason === 2 ?
		"UIIT_MSG_QUEST_ERR_TOO_FAR_FROM_MONSTER" :
		"";
	if ( !key ) return null;
	const metres = reason === 1 ? TRADE_CART_METRES : CAPTURED_MONSTER_METRES;
	return {
		key,
		value: metres,
		arguments: [ String( metres ) ],
		nativeType: 0,
		banner: true,
		bannerUsesFormattedKey: true
	};
}
