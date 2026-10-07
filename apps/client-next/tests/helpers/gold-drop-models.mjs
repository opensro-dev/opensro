/*
===========================================================================

gold-drop-models.mjs - baseline world-entry catalog for presentation tests

Provides every required gold model. Tests of missing catalog entries remove
one explicitly, while unrelated presentation tests use a valid catalog.

===========================================================================
*/

/*
================
goldDropModels
================
*/
export function goldDropModels() {
	return Object.fromEntries( [ "ing", "small", "normal", "large" ].map( tier => [
		`item/etc/drop_ch_money_${tier}.bsr`,
		{
			glb: `/assets/itemdrop/${tier === "ing" ? "fanfare" : tier}.glb`,
			clips: [ "stand" ],
			clipLoop: false
		}
	] ) );
}
