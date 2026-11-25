/*
===========================================================================

party-summon.ts - the party summon refusal lines (0xB4C6 kind 0)

A refused party summon answers {0, u16 code} through the same item-use
error router the v1.696 client ships (the shared ItemUse_ErrorLineForCode
table): each code names the UIIT line the HUD prints. The codes the
native table routes are evidenced; the store/trading and Free PvP
lines exist as text (_5 and _9) but no code routes to them, and the
router's busy code (0x1c0b) has no summon line of its own, so those
answers print nothing - the roster row already read Impossible.

The window's own texts are UIIT_STT_PARTY_SUMMON_* (the product text
catalog completes them from the later client, textResources.mjs).

===========================================================================
*/

/*
================
partySummonRefusalKey

The UIIT line a refusal code prints, or null when the native router
routes the code nowhere a summon line exists.
================
*/
export function partySummonRefusalKey( code: number ): string | null {
	switch ( code ) {
		case 0x1c37:
			return "UIIT_STT_PARTY_SUMMON_IMPOSSILBE_1"; // outside of a town
		case 0x1c25:
			return "UIIT_STT_PARTY_SUMMON_IMPOSSILBE_2"; // players allowed full
		case 0x1c2c:
			return "UIIT_STT_PARTY_SUMMON_IMPOSSILBE_3"; // level requirement
		case 0x1c2e:
			return "UIIT_STT_PARTY_SUMMON_IMPOSSILBE_4"; // job outfit
		case 0x1c30:
			return "UIIT_STT_PARTY_SUMMON_IMPOSSILBE_6"; // on a transport
		case 0x1c39:
			return "UIIT_STT_PARTY_SUMMON_IMPOSSILBE_7"; // guild war
		case 0x1c36:
			return "UIIT_STT_PARTY_SUMMON_IMPOSSILBE_8"; // dead
		case 0x1c3a:
			return "UIIT_STT_PARTY_SUMMON_WAIT_ERR"; // the request is pending
		default:
			return null;
	}
}
