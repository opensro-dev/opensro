/*
===========================================================================

level-cap.ts - the shard's configured character level cap, as published

The server owns the cap (its progression package: native 90, the cap-110
expansion's 110) and publishes it in the world-entry blob when it differs
from the native one. Party registration and the quest guide bound their
level windows by it (#1749); until a world entry publishes one, the
native cap applies.

===========================================================================
*/

// The Legend III era cap (the server's progression.LevelCap).
export const NATIVE_LEVEL_CAP = 90;
// The cap-110 expansion's cap (the server's progression.ExpansionLevelCap).
export const EXPANSION_LEVEL_CAP = 110;

/*
================
publishedLevelCap

The cap a world-entry bootstrap publishes; absent means native. Any other
value is refused: a malformed server fact would corrupt every window
that trusts it, and the training override refuses the same way.
================
*/
export function publishedLevelCap( value: unknown ): number {
	if ( value === undefined ) {
		return NATIVE_LEVEL_CAP;
	}
	if ( value !== NATIVE_LEVEL_CAP && value !== EXPANSION_LEVEL_CAP ) {
		throw Error( "Invalid published level cap" );
	}
	return value;
}
