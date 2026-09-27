/*
===========================================================================

buildNameFilterAsset.mjs - publish the native character-name filter

The v1.150 client checks new character names against the retail
abusefilter.txt word list. The browser client fetches it as
/assets/textdata/abusefilter.txt, which the game-data pack group already
claims (assetPackGroups.mjs). This step is the only producer of that file:
it copies the retail list byte for byte from the licensed extraction, so a
full build publishes it without the old standalone publisher.

===========================================================================
*/

import { readFile } from "node:fs/promises";
import { mkdirSync } from "node:fs";
import path from "node:path";
import { publishBytesAtomically } from "../shared/atomicPublish.mjs";
import { publicRoot, retailTextdataRoot } from "../world/paths.mjs";

const NAME_FILTER_FILE = "abusefilter.txt";
const NAME_FILTER_PUBLIC_DIRECTORY = path.join( "assets", "textdata" );

/*
================
buildNameFilterAsset

Copies the retail name filter into the published tree unchanged. The text
is CP949 and must not be re-encoded, so it is copied as bytes. A missing
source skips with a warning, like the other textdata builders; the pack
group then has nothing to claim.
================
*/
export async function buildNameFilterAsset( {
	sourceRoot = retailTextdataRoot,
	targetRoot = publicRoot
} = {} ) {
	const sourcePath = path.join( sourceRoot, NAME_FILTER_FILE );
	let bytes;
	try {
		bytes = await readFile( sourcePath );
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) {
			console.warn( `[nameFilter] ${sourcePath} missing - skipping` );
			return { written: false, bytes: 0 };
		}
		throw error;
	}

	const targetDirectory = path.join( targetRoot, NAME_FILTER_PUBLIC_DIRECTORY );
	mkdirSync( targetDirectory, { recursive: true } );
	await publishBytesAtomically( path.join( targetDirectory, NAME_FILTER_FILE ), bytes, {
		logLabel: "name-filter"
	} );
	return { written: true, bytes: bytes.length };
}
