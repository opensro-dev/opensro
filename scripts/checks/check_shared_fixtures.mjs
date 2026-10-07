/*
===========================================================================

Shared Native Fixture Gate

Some native reference captures validate both ports against the same machine
code: the browser client's TypeScript and the Go server. The client's tools
generate them, so the client copy is canonical. The Go module keeps its own
byte-identical copy under testdata/, because apps/server is also built and
tested on its own and must not read files outside its module.

This gate fails when a copy differs from its canonical file. After
regenerating a canonical capture, run with `--sync` to refresh the copies.

A capture only one side reads belongs to that side alone (for example
native-spawn-skill-reference.json lives only in the server's testdata).

===========================================================================
*/

import { copyFileSync, existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", ".." );
const CLIENT_NATIVE = "apps/client-next/tests/fixtures/native";
const SERVER_MOVEMENT = "apps/server/internal/game/world/movement/testdata";

const SHARED_FIXTURES = [
	{
		canonical: `${CLIENT_NATIVE}/native-contact-reference.json`,
		copy: `${SERVER_MOVEMENT}/native-contact-reference.json`
	},
	{
		canonical: `${CLIENT_NATIVE}/native-owned-start-reference.json`,
		copy: `${SERVER_MOVEMENT}/native-owned-start-reference.json`
	},
	{
		canonical: `${CLIENT_NATIVE}/native-portal-reference.json`,
		copy: `${SERVER_MOVEMENT}/native-portal-reference.json`
	},
	{
		canonical: `${CLIENT_NATIVE}/native-vertex-direction-reference.json`,
		copy: `${SERVER_MOVEMENT}/native-vertex-direction-reference.json`
	},
	{
		canonical: `${CLIENT_NATIVE}/native-mover-step-reference.json`,
		copy: "apps/server/internal/game/world/simulation/testdata/native-mover-step-reference.json"
	}
];

/*
================
main
================
*/
function main() {
	const sync = process.argv.includes( "--sync" );
	const problems = [];

	for ( const { canonical, copy } of SHARED_FIXTURES ) {
		const canonicalPath = path.join( rebuildRoot, canonical );
		const copyPath = path.join( rebuildRoot, copy );
		if ( !existsSync( canonicalPath ) ) {
			problems.push( `canonical fixture is missing: ${canonical}` );
			continue;
		}
		const same = existsSync( copyPath ) && readFileSync( canonicalPath ).equals( readFileSync( copyPath ) );
		if ( same ) {
			continue;
		}
		if ( sync ) {
			copyFileSync( canonicalPath, copyPath );
			process.stdout.write( `synced ${copy}\n` );
			continue;
		}
		problems.push( `${copy} differs from ${canonical}` );
	}

	if ( problems.length > 0 ) {
		process.stderr.write(
			`${problems.join( "\n" )}\n` +
				"Run `node scripts/checks/check_shared_fixtures.mjs --sync` after regenerating a canonical capture.\n"
		);
		process.exit( 1 );
	}
	process.stdout.write( `shared-fixtures: ${SHARED_FIXTURES.length} server copies match their canonical captures\n` );
}

main();
