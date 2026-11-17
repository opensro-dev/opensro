/*
===========================================================================

skill-notices.ts - cast refusals as notices

The 0xB245 cast refusal routed through 689420 category 4.

===========================================================================
*/

import { resolveNativeNotice } from "./native-notice";
import type { SystemNotice } from "./system-notices";

/*
================
skillNotice

776830 -> 689420 category 4.
================
*/
export function skillNotice(
	opcode: number,
	p: Uint8Array,
	country?: number,
	war = false,
	pkProhibited = false
): SystemNotice | null {
	if ( opcode !== 0xb245 || p[0] === 1 ) return null;
	if ( p.length !== 2 || p[0] !== 2 ) throw Error( "Invalid cast refusal" );
	const result = resolveNativeNotice( 4, p[1]!, { country, war, pkProhibited } );
	return result.kind === "notice" ? result.notice : null;
}
