/*
===========================================================================

worldRoot.mjs - where the local world lives, for every checkout

`pnpm local` used to keep its world (accounts, characters, cluster
identity) under apps/server/.state of whichever checkout started it. A
run from a git worktree therefore booted a second, empty world, and the
characters of the first looked deleted. This owner gives the world one
home: the main checkout's apps/server/.state, the same hub a worktree
already shares its built assets through (AGENTS.md "Worktrees").

SRO_LOCAL_STATE_ROOT (an absolute path) names another world instead. A
test, a smoke run or a load tool sets it to a scratch directory, so
nothing automated ever writes into the world a person plays in.

What stays with the checkout that runs: the binaries it builds and the
workload identity minted for one run (.state/local). Those are products
of that checkout's sources, not of the world.

===========================================================================
*/
import { existsSync } from "node:fs";
import path from "node:path";
import { resolveMainCheckout } from "../lib/generatedRoot.mjs";

export const LOCAL_WORLD_ROOT_ENV = "SRO_LOCAL_STATE_ROOT";

/*
================
resolveLocalWorldRoot

The directory holding cluster/, agent/, shards/ and backups/ for a
local run started from checkout: the override when set, else the main
checkout's apps/server/.state. A relative override is refused: it would
resolve against whatever directory the runner happened to start in,
which is the mistake this owner exists to end.
================
*/
export function resolveLocalWorldRoot( checkout, env = process.env ) {
	const override = env[LOCAL_WORLD_ROOT_ENV]?.trim();
	if ( override ) {
		if ( !path.isAbsolute( override ) ) {
			throw new Error( `${LOCAL_WORLD_ROOT_ENV} must be an absolute path, got ${JSON.stringify( override )}` );
		}
		return path.resolve( override );
	}
	return path.join( resolveMainCheckout( checkout ), "apps", "server", ".state" );
}

/*
================
countWorldCharacters

How many characters the authority in authorityDir holds, or null when
that cannot be read (no authority yet, or a Node without node:sqlite).
The runner prints it before anything opens the store, so an empty world
is named at the start instead of discovered at the character select.
The read is read-only and happens before any child starts.
================
*/
export async function countWorldCharacters( authorityDir ) {
	const database = path.join( authorityDir, "state.db" );
	if ( !existsSync( database ) ) {
		return null;
	}
	// node:sqlite announces itself as experimental on stderr once per
	// process; the runner's console is the player's, so keep it quiet.
	const emitWarning = process.emitWarning;
	process.emitWarning = () => {};
	try {
		const { DatabaseSync } = await import( "node:sqlite" );
		const store = new DatabaseSync( database, { readOnly: true } );
		try {
			return Number( store.prepare( "SELECT count(*) AS n FROM characters" ).get().n );
		} finally {
			store.close();
		}
	} catch {
		return null;
	} finally {
		process.emitWarning = emitWarning;
	}
}

/*
================
describeWorld

The one line a run prints about its world: where it is, whether another
checkout owns it, and what it holds.
================
*/
export function describeWorld( worldRoot, checkoutStateDir, characters ) {
	const shared = path.resolve( worldRoot ) === path.resolve( checkoutStateDir ) ?
		"" :
		" (shared, not this checkout's)";
	const holds = characters === null ?
		"no world yet: a new, empty one is created now" :
		characters === 0 ?
		"0 characters: an empty world" :
		`${characters} character${characters === 1 ? "" : "s"}`;
	return `Local world: ${worldRoot}${shared} - ${holds}`;
}
