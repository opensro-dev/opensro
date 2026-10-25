/*
===========================================================================

check_world_space.mjs - one owner for outdoor sector and dungeon plane math

apps/client-next/src/engine/foundation/world/world-space.ts owns the client's
outdoor 1920-unit sector grid and 0x8000 dungeon-bit predicates, mirroring the
Go server's worldgeom package (apps/server/internal/game/world/geometry.go).

Outside the owner, raw `* 1920` / `1920 *` sector multiplication and raw
`0x8000` region-bit tests are refused unless the file's occurrence count is at
or below its entry in scripts/checks/world-space-baseline.txt. The ledger only
shrinks:

- a file not in the ledger that has any occurrence fails the gate;
- a file whose count exceeds its ledger entry fails the gate;
- a ledger entry whose count has decreased (or dropped to zero) fails the gate
  until `--update` lowers or removes the entry;
- a ledger entry whose file no longer exists fails the gate.

Non-region 0x8000 bit masks (hawk fatal bit, auto-potion enable bit, terrain
chunk key bias, float16 sign bit, fortress hammer type flag, pet affix param
base, stall action mask, audio skip floor) are documented in NON_REGION_8000_FILES
so they do not pollute the world-space ledger.

===========================================================================
*/

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { evaluateLedgerRatchet, formatLedger, parseLedger } from "./ledgerRatchet.mjs";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", ".." );
const baselinePath = path.join( rebuildRoot, "scripts", "checks", "world-space-baseline.txt" );

const BASELINE_HEADER = [
	"# Files in apps/client-next/src with open-coded `* 1920` / `1920 *` sector math",
	"# or raw `0x8000` region-bit tests predating world-space.ts (#1279).",
	"# Format: `<path> <count>`. This ledger may only shrink; run",
	"# `node scripts/checks/check_world_space.mjs --update` after migrating a file."
];

// The client owner of REGION_SIZE (1920) and DUNGEON_REGION_BIT (0x8000).
export const WORLD_SPACE_OWNERS = new Set( [
	"apps/client-next/src/engine/foundation/world/world-space.ts"
] );

// Client files where 0x8000 is a domain bit mask unrelated to regionId's
// dungeon bit. `* 1920` / `1920 *` is still checked in these files.
export const NON_REGION_8000_FILES = new Set( [
	// HAWK_FATAL_BIT = 0x8000 (attack result fatal bit on hawk summon state).
	"apps/client-next/src/engine/foundation/gameplay/attached-effects.ts",
	// Auto-potion bit 15 enable flag in the 16-bit packed setting word.
	"apps/client-next/src/engine/foundation/gameplay/auto-potion.ts",
	// Signed chunk-coordinate packing bias (cx + 0x8000) * 0x10000 + (cz + 0x8000).
	"apps/client-next/src/engine/foundation/rendering/terrain-interaction.ts",
	// IEEE-754 binary16 sign bit (half & 0x8000).
	"apps/client-next/src/engine/foundation/rendering/texel-readback.ts",
	// Fortress hammer item typeFlags mask ((item.typeFlags & 0xf800) === 0x8000).
	"apps/client-next/src/engine/foundation/ui/item-tooltip-stats.ts",
	// PET_AFFIX_PARAM_BASE = 0x8000 (compound pet affix parameter encoding).
	"apps/client-next/src/engine/foundation/ui/pet-training.ts",
	// STALL_ACTION_MASK = 0x8000 (actor action mask bit).
	"apps/client-next/src/engine/runtime/characters/actor-motion.ts",
	// MIN_SKIP = -0x8000 (int16 lower bound for sound sample offset).
	"apps/client-next/src/engine/runtime/characters/sounds/sounds.ts"
] );

const SOURCE = /\.(?:ts|tsx|js|mjs)$/;
const REGION_MULTIPLY = /\*\s*1920\b|\b1920\s*\*/g;
const DUNGEON_BIT_LITERAL = /\b0x8000\b/gi;

/*
================
countWorldSpaceLiterals

Counts `* 1920` / `1920 *` and (unless the file is in NON_REGION_8000_FILES)
raw `0x8000` literals on non-comment lines of `text`.
================
*/
export function countWorldSpaceLiterals( text, relativePath = "" ) {
	if ( WORLD_SPACE_OWNERS.has( relativePath ) ) {
		return 0;
	}
	const skip8000 = NON_REGION_8000_FILES.has( relativePath );
	let count = 0;
	const withoutBlockComments = text.replace( /\/\*[\s\S]*?\*\//g, "" );
	for ( const line of withoutBlockComments.split( /\r?\n/ ) ) {
		if ( /^\s*(?:\/\/|\/\*|\*|#)/.test( line ) ) {
			continue;
		}
		const code = line.replace( /\/\/.*$/, "" );
		const mulMatches = code.match( REGION_MULTIPLY );
		if ( mulMatches ) {
			count += mulMatches.length;
		}
		if ( !skip8000 ) {
			const bitMatches = code.match( DUNGEON_BIT_LITERAL );
			if ( bitMatches ) {
				count += bitMatches.length;
			}
		}
	}
	return count;
}

// What the shared ledger ratchet reports for this gate.
const RATCHET_WORDS = {
	introduce: "raw * 1920 / 0x8000 outside world-space.ts",
	ledger: "world-space-baseline.txt",
	updateCommand: "node scripts/checks/check_world_space.mjs --update"
};

/*
================
parseBaseline
================
*/
export function parseBaseline( text ) {
	return parseLedger( text, "world-space-baseline.txt" );
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
evaluateWorldSpaceRatchet

The shared ledger ratchet (ledgerRatchet.mjs) with this gate's words.
================
*/
export function evaluateWorldSpaceRatchet( actual, baseline, exists, update = false ) {
	return evaluateLedgerRatchet( actual, baseline, exists, update, RATCHET_WORDS );
}

/*
================
trackedClientSources
================
*/
function trackedClientSources() {
	return execFileSync( "git", [ "ls-files", "-z", "--", "apps/client-next/src" ], {
		cwd: rebuildRoot,
		encoding: "utf8"
	} )
		.split( "\0" )
		.filter( ( file ) => file && SOURCE.test( file ) && !WORLD_SPACE_OWNERS.has( file ) );
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

	for ( const file of trackedClientSources() ) {
		const fullPath = path.join( rebuildRoot, file );
		if ( !existsSync( fullPath ) ) {
			continue;
		}
		const count = countWorldSpaceLiterals( readFileSync( fullPath, "utf8" ), file );
		if ( count > 0 ) {
			actual.set( file, count );
		}
	}

	const { problems, nextBaseline, loweredCount, removedCount } = evaluateWorldSpaceRatchet(
		actual,
		baseline,
		( file ) => existsSync( path.join( rebuildRoot, file ) ),
		update
	);

	if ( update && problems.length === 0 ) {
		writeFileSync( baselinePath, formatBaseline( nextBaseline ) );
		process.stdout.write(
			`world-space-baseline: ${loweredCount} lowered, ${removedCount} removed, ${nextBaseline.size} remain\n`
		);
	}

	if ( problems.length > 0 ) {
		process.stderr.write( `${problems.join( "\n\n" )}\n` );
		process.exit( 1 );
	}

	const activeBaseline = update ? nextBaseline : baseline;
	const totalOccurrences = [ ...activeBaseline.values() ].reduce( ( sum, n ) => sum + n, 0 );
	process.stdout.write(
		`world-space: ${activeBaseline.size} file(s) (${totalOccurrences} occurrence(s)) remain in the baseline ledger\n`
	);
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) {
	main();
}
