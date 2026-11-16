/*
===========================================================================

test-cache-ids.mjs - test-cache input ids that hold in every checkout and tree

The test runner records each passing file with the content hash of every
input it read. With the inputs named by absolute path, a record written in
one worktree never matched in another, so every fresh worktree ran all 766
runtime files cold (177 s on 11-14) although its files were the same bytes.
An input inside the checkout is therefore named relative to it, and resolved
against whichever checkout validates the record.

The same holds one level up for the trees a run is pointed at: the built
asset tree, the game root, the server game data, the v1.696 root and the
asset pack baseline. A release builds each freeze into a fresh variant root
with these variables set, so absolute ids (and the paths in the cache key)
kept every release client check cold: 815 of 815 files ran on 11-17
(710 s). An input inside one of those trees is named relative to it
("@generated/...") and resolved against the tree the validating run uses.
The content hash still decides reuse: a hub pass is reused in a release
only where the release's own file hashes the same.

An input outside every root (Node itself, a temporary folder) keeps its
absolute path.

===========================================================================
*/
import path from "node:path";
import { GENERATED_ROOT_ENV, resolveGeneratedRoot } from "../../../../scripts/lib/generatedRoot.mjs";
import {
	SERVER_GAME_DATA_ROOT_ENV,
	gameRoot,
	resolveServerGameDataRoot
} from "../../../../scripts/build/world/paths.mjs";

// Marks a checkout-relative target; never the start of an absolute path.
export const CHECKOUT_PREFIX = "~/";

// The game root's variable, named here only to keep it out of the cache
// key; its value is read by its owner, scripts/build/world/paths.mjs.
const GAME_ROOT_ENV = "SRO_GAME_ROOT";
// Trees with no resolver of their own: an empty value means unset.
const V1696_ROOT_ENV = "SRO_V1696_ROOT";
const PACK_BASELINE_ENV = "SRO_ASSET_PACK_BASELINE";

/*
================
LOCATION_VARIABLES

The variables that only say where a tree is. Their files are recorded by
content under the tree's prefix, so their paths are no test input and stay
out of the cache key (run-tests.mjs cacheKey).
================
*/
export const LOCATION_VARIABLES = new Set( [
	GENERATED_ROOT_ENV,
	SERVER_GAME_DATA_ROOT_ENV,
	GAME_ROOT_ENV,
	V1696_ROOT_ENV,
	PACK_BASELINE_ENV
] );

const caseless = process.platform === "win32";

/*
================
cacheRoots

The [prefix, directory] pairs ids are named against, longest directory
first: the hub's .generated sits inside the hub checkout, and the default
game root holds every checkout, so the most specific tree must win. A tree
whose variable is unset has no pair; an id naming it then resolves to null
and its record reruns.
================
*/
export function cacheRoots( checkout, env = process.env ) {
	const pairs = [
		[ CHECKOUT_PREFIX, checkout ],
		[ "@generated/", resolveGeneratedRoot( env ) ],
		[ "@server-data/", resolveServerGameDataRoot( env ) ],
		[ "@game/", gameRoot ],
		[ "@v1696/", env[V1696_ROOT_ENV]?.trim() ],
		[ "@pack-baseline/", env[PACK_BASELINE_ENV]?.trim() ]
	];
	return pairs.filter( ( [, dir] ) => dir ).map( ( [prefix, dir] ) => [ prefix, path.resolve( dir ) ] )
		.sort( ( a, b ) => b[1].length - a[1].length );
}

/*
================
rootsOf

A bare checkout path stands for the checkout alone.
================
*/
function rootsOf( roots ) {
	return typeof roots === "string" ? [ [ CHECKOUT_PREFIX, path.resolve( roots ) ] ] : roots;
}

/*
================
encodeTarget

The id form of an absolute input path: relative to the most specific root
holding it, or the path itself when none does.
================
*/
export function encodeTarget( roots, target ) {
	const full = path.resolve( target );
	const compare = caseless ? full.toLowerCase() : full;
	for ( const [prefix, dir] of rootsOf( roots ) ) {
		const base = caseless ? dir.toLowerCase() : dir;
		// A file root (the pack baseline) is the target itself.
		if ( compare === base ) return prefix;
		if ( !compare.startsWith( base + path.sep ) ) continue;
		return prefix + full.slice( dir.length + 1 ).split( path.sep ).join( "/" );
	}
	return target;
}

/*
================
decodeTarget

The absolute path an id names under the validating run's roots, or null
when its tree is not set in this run.
================
*/
export function decodeTarget( roots, target ) {
	if ( path.isAbsolute( target ) ) return target;
	const slash = target.indexOf( "/" );
	const prefix = target.slice( 0, slash + 1 );
	const pair = rootsOf( roots ).find( ( [candidate] ) => candidate === prefix );
	if ( !pair ) return null;
	const rest = target.slice( slash + 1 );
	return rest ? path.join( pair[1], ...rest.split( "/" ) ) : pair[1];
}
