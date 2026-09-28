/*
===========================================================================

party-options.ts - one source of sharing rules for party UI and requests

An empty social record is present even before a party exists. Its zero bits
are not the player's chosen formation settings. A live party's rules take
precedence until that party ends; then the local formation choices apply.

===========================================================================
*/

import type { SocialState } from "../gameplay/social";

/*
================
effectivePartyOptions

Native 63B7F0 reads the same party-data sharing fields as invitations and
registration. The port stores formation choices separately from live state,
so resolve the owner before either rendering or serializing those bits.
================
*/
export function effectivePartyOptions( social: SocialState | undefined, formation: number ): number {
	return social?.members.length ? social.options : formation;
}
