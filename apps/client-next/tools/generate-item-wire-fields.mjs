/*
===========================================================================

generate-item-wire-fields.mjs - the server's copy of ITEM_WIRE_FIELDS

Writes apps/server/internal/game/enterworld/itemwirefields.json from
the client-owned tuple in src/engine/contracts/item-wire-fields.ts
(#4448), sorted. The Go test beside the JSON and the client test
beside this script both fail when the copies drift; regenerate with:

  node tools/generate-item-wire-fields.mjs

===========================================================================
*/
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const sourcePath = fileURLToPath( new URL( "../src/engine/contracts/item-wire-fields.ts", import.meta.url ) );
const targetPath = fileURLToPath(
	new URL( "../../../apps/server/internal/game/enterworld/itemwirefields.json", import.meta.url )
);
const source = readFileSync( sourcePath, "utf8" );
const block = source.split( "export const ITEM_WIRE_FIELDS = [" )[1]?.split( "] as const;" )[0];
if ( !block ) {
	throw Error( "ITEM_WIRE_FIELDS tuple not found in " + sourcePath );
}
const names = [ ...block.matchAll( /"([A-Za-z][A-Za-z0-9_]*)"/g ) ].map( ( match ) => match[1] ).sort();
if ( new Set( names ).size !== names.length ) {
	throw Error( "ITEM_WIRE_FIELDS lists a name twice" );
}
writeFileSync( targetPath, JSON.stringify( names, null, "\t" ) + "\n" );
console.log( `wrote ${names.length} item wire fields to ${targetPath}` );
