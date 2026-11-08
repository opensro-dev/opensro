/*
===========================================================================

hwan-skin.ts - the Blue Berserk's gauge art (port-only, not native)

The v1.150 data ships a second set of the Berserk gauge's pictures, named
pmi_jahwan_02* beside the plain pmi_jahwan*: the same sizes, in blue (circle
sheet, face, glow, burn sprites and the three button states). The later
client shows them for a character whose Blue Berserk level is above 0; this
client does the same with the level the server tells (hwan-level.ts), by
naming the blue file in place of the plain one.

===========================================================================
*/

const PLAIN = /(interface\/playerminiinfo\/pmi_jahwan)(?!_02)(?=[._])/;

/*
================
hwanSkin

The gauge picture to draw: the blue file of a plain pmi_jahwan* path for a
Blue Berserk level above 0, and every other path (or level 0) unchanged.
================
*/
export function hwanSkin( path: string, level: number | undefined ): string {
	return level && level > 0 ? path.replace( PLAIN, "$1_02" ) : path;
}
