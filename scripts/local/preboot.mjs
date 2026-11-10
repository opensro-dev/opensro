/*
===========================================================================

preboot.mjs - what `pnpm local` settles before any child starts

The local authority brought to the current schema and the client's
published-assets verdict the doctor prints (#3081). Both used to be
discovered by a child dying at boot: the GameWorld refused an authority
one schema behind with an exact-version error, and the doctor called a
stale pack manifest "built" because its directory existed. Here they
are named, fixed where a fix is safe (an older supported schema), and
refused where it is not (a schema the offline upgrader rejects).

===========================================================================
*/
import { spawnSync } from "node:child_process";
import { cpSync, existsSync, mkdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { collectAssetPackGroups } from "../build/assetPackGroups.mjs";

const repoRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", ".." );
const stateDir = path.join( repoRoot, "apps", "server", ".state" );

/*
================
backupStamp

The timestamp both kinds of aside-copy are named with.
================
*/
export function backupStamp( now ) {
	const pad = n => String( n ).padStart( 2, "0" );
	return `${now.getFullYear()}${pad( now.getMonth() + 1 )}${pad( now.getDate() )}-${pad( now.getHours() )}${
		pad( now.getMinutes() )
	}${pad( now.getSeconds() )}`;
}

/*
================
authorityDirFor

The authority directory the local GameWorld opens, resolved exactly as
the server resolves it (internal/data/store: SRO_AUTHORITY_STATE_DIR
wins, else .state/shards/<shard>/authority under its working
directory, which is apps/server). The settings file's environment
reaches the children over the process environment, so it wins here
too.
================
*/
export function authorityDirFor( settings, shardID ) {
	const fromSettings = settings.env.get( "SRO_AUTHORITY_STATE_DIR" )?.trim();
	const fromProcess = process.env.SRO_AUTHORITY_STATE_DIR?.trim();
	return fromSettings || fromProcess || path.join( stateDir, "shards", shardID, "authority" );
}

/*
================
authorityIsCurrent

The offline upgrader's verdict when an authority is already in the
current format (cmd/tools/sro-authority-upgrade): validation exits 0
either way, so this sentence is the classification seam. Any other
validated output is an older supported schema, and committing there is
idempotent, so a reworded third verdict degrades to a harmless commit.
================
*/
export function authorityIsCurrent( validateOutput ) {
	return validateOutput.includes( "already in the current format" );
}

/*
================
runUpgradeTool

One offline-upgrader invocation with its output captured. The runner
builds the tool next to the other binaries; tests inject their own run
over a fixture authority.
================
*/
function runUpgradeTool( tool, args ) {
	const result = spawnSync( tool, args, { encoding: "utf8" } );
	return { status: result.status ?? 1, stdout: result.stdout ?? "", stderr: result.stderr ?? "" };
}

/*
================
upgradeLocalAuthority

Bring a local authority left behind by a schema bump to the current
schema before anything opens it: validate with the offline upgrader,
copy the authority aside under .state/backups, then commit. A schema
the validator refuses (newer than the binary, too old to convert,
corrupt) is never touched - the tool's own error stops the run.
Returns what happened: "absent" (no authority yet; provisioning
creates one), "current" or "upgraded".
================
*/
export function upgradeLocalAuthority( options ) {
	const run = options.run ?? runUpgradeTool;
	const log = options.log ?? console.log;
	if ( !existsSync( options.dir ) ) {
		return "absent";
	}
	const validated = run( options.tool, [ "-authority-dir", options.dir ] );
	if ( validated.status !== 0 ) {
		throw new Error(
			`local authority: ${
				validated.stderr.trim() || validated.stdout.trim() || "the offline upgrader refused it"
			}`
		);
	}
	if ( authorityIsCurrent( validated.stdout ) ) {
		return "current";
	}
	const backup = path.join( options.backupsDir, `authority-${backupStamp( options.now ?? new Date() )}` );
	mkdirSync( options.backupsDir, { recursive: true } );
	cpSync( options.dir, backup, { recursive: true } );
	log( `local authority predates the current schema; copied it to ${backup} and upgrading it in place` );
	const committed = run( options.tool, [ "-authority-dir", options.dir, "-commit" ] );
	if ( committed.status !== 0 ) {
		throw new Error(
			`local authority upgrade failed, the pre-upgrade copy stays at ${backup}: ${
				committed.stderr.trim() || committed.stdout.trim()
			}`
		);
	}
	for ( const line of committed.stdout.split( /\r?\n/ ) ) {
		if ( line !== "" ) {
			log( line );
		}
	}
	log( `local authority upgraded to the current schema; the pre-upgrade copy stays at ${backup}` );
	return "upgraded";
}

/*
================
unpublishedStartupAssets

Null when everything the client requires at startup is in the published
pack manifest, else the doctor fix naming the missing paths. The
requirement set is the pack collector's own startup load groups over
the built tree (build/assetPackGroups.mjs is the one owner of that
membership; the two caller lists only re-partition images between
startup groups, so empty lists still name every startup path), and the
published set mirrors the client's lookup: an exact path, or a `.json`
path served by its `.json.gz` pack entry. This is the check that must
fail when a publisher adds a startup file (e.g. #2969's
event-interface.json) and the manifest predates it.
================
*/
export async function unpublishedStartupAssets( publicRoot ) {
	const publish = "run `pnpm assets build full`";
	const manifestPath = path.join( publicRoot, "assets", "packs", "manifest.json" );
	if ( !existsSync( manifestPath ) ) {
		return `${publish} (no published pack manifest yet)`;
	}
	let index;
	try {
		index = JSON.parse( readFileSync( manifestPath, "utf8" ) );
	} catch {
		return `${publish} (the published pack manifest is unreadable)`;
	}
	const published = new Set();
	for ( const asset of index.assets ?? [] ) {
		if ( typeof asset?.path !== "string" ) {
			continue;
		}
		const key = asset.path.toLowerCase();
		published.add( key );
		if ( key.endsWith( ".json.gz" ) ) {
			published.add( key.slice( 0, -".gz".length ) );
		}
	}
	let groups;
	try {
		({ groups } = await collectAssetPackGroups( {
			publicRoot,
			uiImagePreloadPaths: [],
			missionMinimapTilePaths: [],
			includeOutdoorWorld: false
		} ));
	} catch ( error ) {
		return `${publish} (cannot list the built assets: ${error.message})`;
	}
	const missing = new Set();
	for ( const group of groups ) {
		if ( group.load !== "startup" ) {
			continue;
		}
		for ( const file of group.files ) {
			if ( !published.has( file.toLowerCase() ) ) {
				missing.add( file );
			}
		}
	}
	if ( missing.size === 0 ) {
		return null;
	}
	const named = [ ...missing ].sort();
	return `${publish} (the published packs lack ${named.slice( 0, 5 ).join( ", " )}${
		named.length > 5 ? ` and ${named.length - 5} more` : ""
	})`;
}
