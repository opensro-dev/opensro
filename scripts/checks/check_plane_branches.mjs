/*
===========================================================================

check_plane_branches.mjs - caves and the open world have one plane owner

#1646: the behaviour of the two planes lives in one plane owner per side,
so the outdoor and cave versions of a job cannot drift apart. Callers ask
the owner instead of testing the dungeon region bit:

  server  simulation.Plane / Planes.For(region), implemented in
          movement/plane.go, and simulation.PlaneFeatures
  client  foundation/world/plane.ts: planeKindOf and featuresAt (#1793)

A reference to the bit (Go `IsDungeonRegion` / `DungeonRegionBit`, TypeScript
`isDungeonRegion` / `DUNGEON_REGION_BIT`) in the server's Go sources or the
client's src (tests excluded) is refused outside the owners below unless the
file's count is at or below its entry in plane-branches-baseline.txt, a
shrinking ledger (ledgerRatchet.mjs owns its rules). Each migration PR lowers
the ledger with `--update`.

The server's entries are gone (steps 1-3): on the server the region bit is
plane-owner-only. The client's files ratchet down under #1793; when they
are gone too, the ledger file stays, header only, because the checker reads
it.

===========================================================================
*/

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { evaluateLedgerRatchet, formatLedger, parseLedger } from "./ledgerRatchet.mjs";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", ".." );
const baselinePath = path.join( rebuildRoot, "scripts", "checks", "plane-branches-baseline.txt" );
const LEDGER_NAME = "plane-branches-baseline.txt";

const BASELINE_HEADER = [
	"# Server Go and client src files that branch on the dungeon region bit outside",
	"# the plane owners (#1646, #1793). Format: `<path> <count>`. This ledger may only shrink; run",
	"# `node scripts/checks/check_plane_branches.mjs --update` after migrating a file."
];

// The owners that may read the region bit.
export const PLANE_OWNERS = new Set( [
	// The world-space arithmetic (#1279): the bit's definition and the
	// plane-aware delta, distance and adjacency.
	"apps/server/internal/game/world/geometry.go",
	// The plane owner: PlaneKindOf, and the two implementations.
	"apps/server/internal/game/world/simulation/plane.go",
	"apps/server/internal/game/world/movement/plane.go",
	// The interest buckets: position arithmetic keyed by the plane.
	"apps/server/internal/game/world/simulation/mover_interest_index.go",
	// The client's region arithmetic (the bit's definition) and its plane
	// owner (#1793).
	"apps/client-next/src/engine/foundation/world/world-space.ts",
	"apps/client-next/src/engine/foundation/world/plane.ts"
] );

const RATCHET_WORDS = {
	introduce: "dungeon region-bit branches outside the plane owner",
	ledger: LEDGER_NAME,
	updateCommand: "node scripts/checks/check_plane_branches.mjs --update"
};

const GO_SOURCE = /^apps\/server\/.*\.go$/;
const GO_TEST = /_test\.go$/;
const CLIENT_SOURCE = /^apps\/client-next\/src\/.*\.(?:ts|tsx|js|mjs)$/;
// A reference, not only a call: a predicate stored as a value (or an
// import of it) branches too. Go and TypeScript spell the bit differently.
const PLANE_BRANCH = /\bIsDungeonRegion\b|\bDungeonRegionBit\b|\bisDungeonRegion\b|\bDUNGEON_REGION_BIT\b/g;

/*
================
countPlaneBranches

Counts region-bit branches on the non-comment lines of a Go file: every
reference to IsDungeonRegion or DungeonRegionBit. Owners count zero.
================
*/
export function countPlaneBranches( text, relativePath = "" ) {
	if ( PLANE_OWNERS.has( relativePath ) ) {
		return 0;
	}
	let count = 0;
	const withoutBlockComments = text.replace( /\/\*[\s\S]*?\*\//g, "" );
	for ( const line of withoutBlockComments.split( /\r?\n/ ) ) {
		const code = line.replace( /\/\/.*$/, "" );
		const matches = code.match( PLANE_BRANCH );
		if ( matches ) {
			count += matches.length;
		}
	}
	return count;
}

/*
================
parseBaseline
================
*/
export function parseBaseline( text ) {
	return parseLedger( text, LEDGER_NAME );
}

/*
================
formatBaseline
================
*/
export function formatBaseline( entries ) {
	return formatLedger( BASELINE_HEADER, entries );
}

/*
================
evaluatePlaneRatchet
================
*/
export function evaluatePlaneRatchet( actual, baseline, exists, update = false ) {
	return evaluateLedgerRatchet( actual, baseline, exists, update, RATCHET_WORDS );
}

/*
================
trackedPlaneSources

The server's Go sources (tests excluded) and the client's src, the owners
aside.
================
*/
function trackedPlaneSources() {
	return execFileSync( "git", [ "ls-files", "-z", "--", "apps/server", "apps/client-next/src" ], {
		cwd: rebuildRoot,
		encoding: "utf8"
	} )
		.split( "\0" )
		.filter( ( file ) =>
			(GO_SOURCE.test( file ) && !GO_TEST.test( file ) || CLIENT_SOURCE.test( file )) && !PLANE_OWNERS.has( file )
		);
}

/*
================
main
================
*/
function main() {
	const update = process.argv.includes( "--update" );
	const baseline = parseBaseline( readFileSync( baselinePath, "utf8" ) );
	const actual = new Map();

	for ( const file of trackedPlaneSources() ) {
		const fullPath = path.join( rebuildRoot, file );
		if ( !existsSync( fullPath ) ) {
			continue;
		}
		const count = countPlaneBranches( readFileSync( fullPath, "utf8" ), file );
		if ( count > 0 ) {
			actual.set( file, count );
		}
	}

	const { problems, nextBaseline, loweredCount, removedCount } = evaluatePlaneRatchet(
		actual,
		baseline,
		( file ) => existsSync( path.join( rebuildRoot, file ) ),
		update
	);

	if ( update && problems.length === 0 ) {
		writeFileSync( baselinePath, formatBaseline( nextBaseline ) );
		process.stdout.write(
			`plane-branches-baseline: ${loweredCount} lowered, ${removedCount} removed, ${nextBaseline.size} remain\n`
		);
	}

	if ( problems.length > 0 ) {
		process.stderr.write( `${problems.join( "\n\n" )}\n` );
		process.exit( 1 );
	}

	const activeBaseline = update ? nextBaseline : baseline;
	const totalOccurrences = [ ...activeBaseline.values() ].reduce( ( sum, n ) => sum + n, 0 );
	process.stdout.write(
		`plane-branches: ${activeBaseline.size} file(s) (${totalOccurrences} branch(es)) remain in the baseline ledger\n`
	);
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) {
	main();
}
