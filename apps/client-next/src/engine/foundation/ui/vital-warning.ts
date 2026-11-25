/*
===========================================================================

vital-warning.ts - the low HP/MP gauge warning test

One pure predicate the HUD and the auto-potion path share: whether a
vital has fallen into the warning band the options ask about.

===========================================================================
*/

/*
================
vitalWarning

6B6D00 compares the rounded float gauge, not unrounded integer
percentages: the band is 0 < fround(current / maximum) < fround(.3),
and an unknown or empty vital never warns.
================
*/
export function vitalWarning( current: number | undefined, maximum: number | undefined ): boolean {
	if ( current === undefined || maximum === undefined || maximum <= 0 ) return false;
	const ratio = Math.fround( Math.min( current, maximum ) / maximum );
	return ratio > 0 && ratio < Math.fround( .3 );
}
