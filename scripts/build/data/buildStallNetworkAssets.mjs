/*
===========================================================================

buildStallNetworkAssets.mjs - publish the stall network's category tables

The v1.150 client loads textdata/fmncategorytreedata.txt (the category
tree CIFStallNetwork shows) and textdata/fmntidgroupmapdata.txt (an item's
TypeID tuple -> category, 7E1330) at startup (Client_LoadGameDataTables
722E20). The browser client fetches both as /assets/textdata/<name>, which
the game-data pack group claims beside the name filter
(assetPackGroups.mjs). They are UTF-16 text and are copied as bytes.

===========================================================================
*/

import { readFile } from "node:fs/promises";
import { mkdirSync } from "node:fs";
import path from "node:path";
import { publishBytesAtomically } from "../shared/atomicPublish.mjs";
import { publicRoot, retailTextdataRoot } from "../world/paths.mjs";

export const STALL_NETWORK_FILES = [ "fmncategorytreedata.txt", "fmntidgroupmapdata.txt" ];
const STALL_NETWORK_PUBLIC_DIRECTORY = path.join( "assets", "textdata" );

/*
================
buildStallNetworkAssets

Copies both tables into the published tree unchanged. A missing source
skips with a warning, like the other textdata builders.
================
*/
export async function buildStallNetworkAssets( {
	sourceRoot = retailTextdataRoot,
	targetRoot = publicRoot
} = {} ) {
	const targetDirectory = path.join( targetRoot, STALL_NETWORK_PUBLIC_DIRECTORY );
	let written = 0, bytes = 0;
	for ( const name of STALL_NETWORK_FILES ) {
		const sourcePath = path.join( sourceRoot, name );
		let data;
		try {
			data = await readFile( sourcePath );
		} catch ( error ) {
			if ( error?.code === "ENOENT" ) {
				console.warn( `[stallNetwork] ${sourcePath} missing - skipping` );
				continue;
			}
			throw error;
		}
		mkdirSync( targetDirectory, { recursive: true } );
		await publishBytesAtomically( path.join( targetDirectory, name ), data, { logLabel: "stall-network" } );
		written++;
		bytes += data.length;
	}
	return { written, bytes };
}
