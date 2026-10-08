/*
===========================================================================

publicationLedger.mjs - which build owner produced each public asset

The incremental build reuses whatever is on disk, so a file an older
pipeline wrote (a PNG a newer builder now ships as .texture, a family that
moved) stays in the public tree and the pack sweep would ship it forever.
The ledger closes that gap: every process that publishes into
client-public/assets claims the files it wrote OR kept this run, and the
pack tail soft-archives every swept file no owner claimed before packing
(auditClaims), and the release packager refuses an index with unclaimed
assets (verifyIndexClaims).

One ledger file per owner (the outdoor build, the resource build, each
focused family and publisher) lives beside the generated tree it describes
(generatedPath( "publication-ledger" )), so every worktree that shares the
tree shares the record. An owner's complete run replaces its file; a
partial run (one region, one family flag) merges into it.

Claims are recorded by the shared publication helpers (atomic publish, JSON
writers, converted images, block textures), so a builder that goes through
them needs no ledger code; a builder that skips work because its output is
current claims that output explicitly with claimPublicFile, claimPublicPaths
or claimKeptOutput.

===========================================================================
*/
import { mkdir, readdir, readFile, rename, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { CLIENT_PUBLIC_ROOT, generatedPath } from "../../lib/generatedRoot.mjs";
import { PUBLISH_FAMILIES, REFRESH_FAMILIES } from "../../tasks/assets.mjs";
import { archiveGeneratedArtifact } from "../artifacts/generatedArtifactArchive.mjs";

const LEDGER_FORMAT = "sro-publication-ledger";
const LEDGER_VERSION = 1;
const ASSETS_ROOT = path.join( CLIENT_PUBLIC_ROOT, "assets" );
// The pack tail owns everything under packs/ and the web manifests itself.
const PACK_TAIL_PREFIXES = [ "/assets/packs/" ];
const PACK_TAIL_FILES = new Set( [ "/assets/manifest.json" ] );
// The two builds always own records; a focused refresh of files the
// resource build also writes may add its own.
const CORE_OWNERS = [ "resource-build", "outdoor-world" ];
const OPTIONAL_OWNERS = [ "world-map" ];

let current = null;

/*
================
ledgerRoot
================
*/
export function ledgerRoot() {
	return generatedPath( "publication-ledger" );
}

/*
================
toPublicPath

The /assets/... path of an absolute file under the public tree, or null
for anything outside it or owned by the pack tail.
================
*/
export function toPublicPath( absolutePath ) {
	const relative = path.relative( ASSETS_ROOT, path.resolve( absolutePath ) );
	if ( !relative || relative.startsWith( ".." ) || path.isAbsolute( relative ) ) return null;
	const publicPath = "/assets/" + relative.split( path.sep ).join( "/" );
	if ( PACK_TAIL_FILES.has( publicPath ) || PACK_TAIL_PREFIXES.some( prefix => publicPath.startsWith( prefix ) ) ) {
		return null;
	}
	return publicPath;
}

/*
================
beginPublication

Starts recording claims for one owner in this process. complete=false marks
a partial run whose claims merge into the owner's previous record.
================
*/
export function beginPublication( owner, { complete = true } = {} ) {
	if ( !/^[a-z0-9][a-z0-9.-]*$/.test( owner ) ) throw new Error( `Invalid publication owner: ${owner}` );
	if ( current ) throw new Error( `Publication ${current.owner} is still open` );
	current = { owner, complete, files: new Set() };
}

/*
================
isPublicationOpen
================
*/
export function isPublicationOpen() {
	return current !== null;
}

/*
================
claimPublicFile

Records that the open owner produced or kept one file. Outside an open
publication (a test, a one-off tool) this is a no-op.
================
*/
export function claimPublicFile( absolutePath ) {
	if ( !current ) return;
	const publicPath = toPublicPath( absolutePath );
	if ( publicPath ) current.files.add( publicPath.toLowerCase() );
}

/*
================
claimPublicPaths

The same for /assets/... paths a builder already holds.
================
*/
export function claimPublicPaths( publicPaths ) {
	if ( !current ) return;
	for ( const publicPath of publicPaths ) {
		claimPublicFile( path.join( CLIENT_PUBLIC_ROOT, publicPath.replace( /^\/+/, "" ) ) );
	}
}

// A JSON string value that is a public path; paths never contain a quote or backslash.
const REFERENCED_PUBLIC_PATH = /"(\/assets\/[^"\\]+)"/g;

/*
================
claimKeptOutput

A builder that keeps an existing output instead of rewriting it claims the
output and every /assets/... path the output's text references: the client
loads exactly those paths, so they stay live as long as the output does.
================
*/
export function claimKeptOutput( absolutePath, text ) {
	if ( !current ) return;
	claimPublicFile( absolutePath );
	claimPublicPaths( [ ...text.matchAll( REFERENCED_PUBLIC_PATH ) ].map( match => match[1] ) );
}

/*
================
commitPublication

Writes the open owner's record and closes it. A complete run replaces the
previous record; a partial run merges into it.
================
*/
export async function commitPublication() {
	if ( !current ) throw new Error( "No open publication" );
	const { owner, complete, files } = current;
	current = null;
	const target = path.join( ledgerRoot(), `${owner}.json` );
	const merged = new Set( files );
	if ( !complete ) { for ( const file of (await readOwner( target ))?.files ?? [] ) merged.add( file ); }
	await mkdir( ledgerRoot(), { recursive: true } );
	const temporary = `${target}.${process.pid}.tmp`;
	const record = { format: LEDGER_FORMAT, version: LEDGER_VERSION, owner, files: [ ...merged ].sort() };
	await writeFile( temporary, JSON.stringify( record ) );
	await rename( temporary, target );
	return { owner, files: merged.size };
}

/*
================
abandonPublication

Drops the open owner's claims without writing: a failed run must not
replace a good record with a partial one.
================
*/
export function abandonPublication() {
	current = null;
}

/*
================
readOwner
================
*/
async function readOwner( file ) {
	try {
		const record = JSON.parse( await readFile( file, "utf8" ) );
		if ( record.format !== LEDGER_FORMAT || record.version !== LEDGER_VERSION || !Array.isArray( record.files ) ) {
			throw new Error( `Invalid publication ledger ${file}` );
		}
		return record;
	} catch ( error ) {
		if ( error.code === "ENOENT" ) return null;
		throw error;
	}
}

/*
================
readClaims

Every owner's record: { owners: Map<owner, Set<path>>, claimed: Set<path> }.
The open publication of this process counts with what it has claimed so
far, replacing its committed record: the full build audits its own claims
before it commits them.
================
*/
export async function readClaims() {
	const owners = new Map(), claimed = new Set();
	let names = [];
	try {
		names = await readdir( ledgerRoot() );
	} catch ( error ) {
		if ( error.code !== "ENOENT" ) throw error;
	}
	for ( const name of names.filter( name => name.endsWith( ".json" ) ).sort() ) {
		const record = await readOwner( path.join( ledgerRoot(), name ) );
		owners.set( record.owner, new Set( record.files ) );
	}
	if ( current ) {
		const previous = current.complete ? new Set() : owners.get( current.owner ) ?? new Set();
		owners.set( current.owner, new Set( [ ...previous, ...current.files ] ) );
	}
	for ( const files of owners.values() ) for ( const file of files ) claimed.add( file );
	return { owners, claimed };
}

const PRECOMPRESSED = /\.(?:gz|br|zst)$/;

/*
================
isClaimed

A precompressed sidecar (.gz/.br/.zst) belongs to whoever claimed its base,
and a base belongs to whoever claimed its packed .gz member: the JSON
compressor derives one from the other and owns neither.
================
*/
export function isClaimed( publicPath, claimed ) {
	const key = publicPath.toLowerCase();
	if ( claimed.has( key ) ) return true;
	if ( PRECOMPRESSED.test( key ) && claimed.has( key.replace( PRECOMPRESSED, "" ) ) ) return true;
	return claimed.has( key + ".gz" );
}

/*
================
expectedOwners

The owners a complete tree has records for: the two builds, and every
focused family and standalone publisher of the task table. The world-map
refresh republishes files the resource build also writes, so its record is
optional.
================
*/
export function expectedOwners() {
	return [
		...CORE_OWNERS,
		...[ ...REFRESH_FAMILIES, ...PUBLISH_FAMILIES ].map( family => `family-${family}` )
	];
}

/*
================
ledgerStatus

Splits the records into the claims of known owners, the expected owners
with no record, and records of owners that no longer exist (a renamed or
removed family), which claim nothing.
================
*/
async function ledgerStatus() {
	const { owners } = await readClaims();
	const expected = expectedOwners();
	const known = new Set( [ ...expected, ...OPTIONAL_OWNERS ] );
	const claimed = new Set();
	for ( const [owner, files] of owners ) {
		if ( known.has( owner ) ) { for ( const file of files ) claimed.add( file ); }
	}
	return {
		owners,
		claimed,
		missingOwners: expected.filter( owner => !owners.has( owner ) ),
		retiredOwners: [ ...owners.keys() ].filter( owner => !known.has( owner ) )
	};
}

/*
================
auditClaims

Compares the pack groups' files with the ledger. With every expected owner
recorded, each file no owner claimed is soft-archived (moved through
archiveGeneratedArtifact, never deleted) and dropped from its group, and
records of retired owners are archived too. With an owner missing nothing
moves: the files of a family that never ran cannot be told from garbage.
Writes the report to generatedPath( "unclaimed-assets.json" ) and returns
it with the groups to pack. options.archiveRoot overrides the archive
location (tests).
================
*/
export async function auditClaims( groups, { publicRoot = CLIENT_PUBLIC_ROOT, archiveRoot } = {} ) {
	const status = await ledgerStatus();
	const unclaimed = [];
	for ( const group of groups ) {
		for ( const publicPath of group.files ) {
			if ( isClaimed( publicPath, status.claimed ) ) continue;
			const bytes = (await stat( publicFilePath( publicRoot, publicPath ) )).size;
			unclaimed.push( { path: publicPath, group: group.name, bytes } );
		}
	}
	const complete = status.missingOwners.length === 0;
	if ( complete ) {
		for ( const row of unclaimed ) {
			await archiveGeneratedArtifact( publicFilePath( publicRoot, row.path ), {
				scopeRoot: publicRoot,
				archiveRoot,
				reason: "unclaimed-public-asset"
			} );
		}
		for ( const owner of status.retiredOwners ) {
			await archiveGeneratedArtifact( path.join( ledgerRoot(), `${owner}.json` ), {
				scopeRoot: ledgerRoot(),
				archiveRoot,
				reason: "retired-publication-owner"
			} );
		}
	}
	const archived = new Set( complete ? unclaimed.map( row => row.path ) : [] );
	const folders = new Map();
	for ( const row of unclaimed ) {
		const folder = row.path.split( "/" ).slice( 0, 5 ).join( "/" );
		const total = folders.get( folder ) ?? { folder, files: 0, bytes: 0 };
		total.files++;
		total.bytes += row.bytes;
		folders.set( folder, total );
	}
	const report = {
		complete,
		archived: complete,
		missingOwners: status.missingOwners,
		retiredOwners: status.retiredOwners,
		owners: [ ...status.owners ].map( ( [owner, files] ) => ({ owner, files: files.size }) ),
		files: unclaimed.length,
		bytes: unclaimed.reduce( ( sum, row ) => sum + row.bytes, 0 ),
		folders: [ ...folders.values() ].sort( ( a, b ) => b.bytes - a.bytes ),
		unclaimed
	};
	await writeFile( generatedPath( "unclaimed-assets.json" ), JSON.stringify( report, null, "\t" ) );
	return {
		...report,
		groups: groups.map( group => ({ ...group, files: group.files.filter( file => !archived.has( file ) ) }) )
	};
}

/*
================
verifyIndexClaims

The release gate: every expected owner has a record and every asset of a
published pack index is claimed. Returns the problems; empty means the
tree holds only what the current pipeline produces.
================
*/
export async function verifyIndexClaims( index ) {
	const status = await ledgerStatus();
	const problems = status.missingOwners.map( owner => `no publication record for ${owner}` );
	const unclaimed = index.assets.filter( asset => !isClaimed( asset.path, status.claimed ) );
	if ( unclaimed.length > 0 ) {
		problems.push(
			`${unclaimed.length} packed asset(s) claimed by no build owner, e.g. ` +
				unclaimed.slice( 0, 5 ).map( asset => asset.path ).join( ", " )
		);
	}
	return problems;
}

/*
================
publicFilePath
================
*/
function publicFilePath( publicRoot, publicPath ) {
	return path.join( publicRoot, publicPath.replace( /^\/+/, "" ) );
}
