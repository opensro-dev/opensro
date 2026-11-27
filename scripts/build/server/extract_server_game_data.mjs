/*
===========================================================================

extract_server_game_data.mjs - unpack a server game-data archive's files

	node scripts/build/server/extract_server_game_data.mjs ARCHIVE OUT [--prefix textdata/]

The published-id pin is taken from the server textdata a release served
(scripts/pin_published_identities.py). That textdata ships inside the
release's server.srogz, and release.py repin unpacks it with this
command (#4337). The archive is checked in full before a byte is written.

===========================================================================
*/

import path from "node:path";
import { extractServerGameDataArchive } from "./serverGameDataArchive.mjs";

const USAGE = "usage: extract_server_game_data.mjs ARCHIVE OUT [--prefix PREFIX]";

/*
================
parseArguments
================
*/
function parseArguments( argv ) {
	const positional = [];
	let prefix = "";
	for ( let index = 0; index < argv.length; index += 1 ) {
		if ( argv[index] === "--prefix" ) {
			prefix = argv[index + 1] ?? "";
			index += 1;
		} else {
			positional.push( argv[index] );
		}
	}
	if ( positional.length !== 2 ) throw new Error( USAGE );
	return { archive: path.resolve( positional[0] ), out: path.resolve( positional[1] ), prefix };
}

const { archive, out, prefix } = parseArguments( process.argv.slice( 2 ) );
const written = await extractServerGameDataArchive( archive, out, prefix );
if ( written === 0 ) throw new Error( `${archive} has no file under ${JSON.stringify( prefix )}` );
console.log( `extracted ${written} files${prefix ? ` under ${prefix}` : ""} to ${out}` );
