/*
===========================================================================

englishCorrections.mjs - reviewed corrections to authored v1.150 English

The client catalogs and server projection share this policy. Match both the
symbol and original cell so changed source data cannot silently receive an
obsolete correction. Native captures and the extracted archive stay intact.

The reviewed table is englishCorrections.json: { file: { key: { before,
after, why? } } }. `before` is the exact retail English cell, `after` the
product text, `why` the evidence when the change is more than spelling.

===========================================================================
*/

import { readFileSync } from "node:fs";
import path from "node:path";

const CORRECTIONS = JSON.parse( readFileSync( path.join( import.meta.dirname, "englishCorrections.json" ), "utf8" ) );

/*
================
correctedEnglish

Accept already-corrected projections too; applying the build twice must not
change output. New source wording requires a human review of this table.
================
*/
export function correctedEnglish( fileName, key, english ) {
	const correction = CORRECTIONS[fileName.toLowerCase()]?.[key];
	if ( !correction ) return english;
	if ( english === correction.after ) return english;
	if ( english !== correction.before ) throw new Error( `Stale English correction ${fileName}:${key}` );
	return correction.after;
}
