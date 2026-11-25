/*
===========================================================================

formerIds.mjs - a monster row names the ids it once had

The pin moves a monster off a released id when an item the players hold
shipped on the same id (published_ids.settle_shared, #3994). The website
links every monster by id, so the catalogue row names each id it left
(formerIds): the site redirects an old link to the row, and the host's
catalogue gate accepts the lost id because the same monster names it.
The pin is the record; former_ids.py reads it through its Python owner.

===========================================================================
*/
import { fileURLToPath } from "node:url";
import { runPython } from "../shared/pythonRun.mjs";

/*
================
readFormerIds

codename -> [ids] from the pin's "former" map of characters.
================
*/
export async function readFormerIds() {
	const { stdout } = await runPython( [ fileURLToPath( new URL( "./former_ids.py", import.meta.url ) ) ], {
		task: "catalogue former ids"
	} );
	return JSON.parse( stdout );
}

/*
================
fillFormerIds

Sets formerIds on each monster row the pin moved. A former id that is a
monster's current id, or one two monsters name, refuses: the site could
not tell which page an old link means.
================
*/
export function fillFormerIds( monsters, former ) {
	const current = new Set( monsters.map( monster => monster.id ) );
	const claimed = new Map();
	for ( const monster of monsters ) {
		const ids = former[monster.codename];
		if ( !ids || !ids.length ) continue;
		for ( const id of ids ) {
			if ( current.has( id ) ) {
				throw new Error( `former id ${id} of ${monster.codename} is a current monster id` );
			}
			if ( claimed.has( id ) ) {
				throw new Error( `former id ${id} is named by ${claimed.get( id )} and ${monster.codename}` );
			}
			claimed.set( id, monster.codename );
		}
		monster.formerIds = [ ...ids ].sort( ( a, b ) => a - b );
	}
	return monsters;
}
