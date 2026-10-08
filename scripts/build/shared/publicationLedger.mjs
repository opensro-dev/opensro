/*
===========================================================================

publicationLedger.mjs - which build owner produced each public asset

The incremental build reuses whatever is on disk, so a file an older
pipeline wrote (a PNG a newer builder now ships as .texture, a family that
moved) stays in the public tree and the pack sweep would ship it forever.
The ledger closes that gap: every process that publishes into
client-public/assets claims the files it wrote OR kept this run, and the
pack tail compares the swept tree against the union of all claims.

One ledger file per owner (the outdoor build, the resource build, each
focused family and publisher) lives beside the generated tree it describes
(generatedPath( "publication-ledger" )), so every worktree that shares the
tree shares the record. An owner's complete run replaces its file; a
partial run (one region, one family flag) merges into it.

Claims are recorded by the shared publication helpers (atomic publish, JSON
writers, converted images, block textures), so a builder that goes through
them needs no ledger code; a builder that skips work because its output is
current claims that output explicitly with claimPublicFiles.

===========================================================================
*/
import { mkdir, readdir, readFile, rename, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { CLIENT_PUBLIC_ROOT, generatedPath } from "../../lib/generatedRoot.mjs";

const LEDGER_FORMAT = "sro-publication-ledger";
const LEDGER_VERSION = 1;
const ASSETS_ROOT = path.join( CLIENT_PUBLIC_ROOT, "assets" );
// The pack tail owns everything under packs/ and the web manifests itself.
const PACK_TAIL_PREFIXES = [ "/assets/packs/" ];
const PACK_TAIL_FILES = new Set( [ "/assets/manifest.json" ] );

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
		const files = new Set( record.files );
		owners.set( record.owner, files );
		for ( const file of files ) claimed.add( file );
	}
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
auditClaims

Compares the pack groups' files with the ledger and writes the report
(generatedPath( "unclaimed-assets.json" )): every unclaimed file with its
bytes, and totals per top folders. Report only; nothing is moved.
================
*/
export async function auditClaims( groups, publicRoot = CLIENT_PUBLIC_ROOT ) {
	const { owners, claimed } = await readClaims();
	const unclaimed = [];
	for ( const group of groups ) {
		for ( const publicPath of group.files ) {
			if ( isClaimed( publicPath, claimed ) ) continue;
			const bytes = (await stat( path.join( publicRoot, publicPath.replace( /^\/+/, "" ) ) )).size;
			unclaimed.push( { path: publicPath, group: group.name, bytes } );
		}
	}
	const folders = new Map();
	for ( const row of unclaimed ) {
		const folder = row.path.split( "/" ).slice( 0, 5 ).join( "/" );
		const total = folders.get( folder ) ?? { folder, files: 0, bytes: 0 };
		total.files++;
		total.bytes += row.bytes;
		folders.set( folder, total );
	}
	const report = {
		owners: [ ...owners ].map( ( [owner, files] ) => ({ owner, files: files.size }) ),
		files: unclaimed.length,
		bytes: unclaimed.reduce( ( sum, row ) => sum + row.bytes, 0 ),
		folders: [ ...folders.values() ].sort( ( a, b ) => b.bytes - a.bytes ),
		unclaimed
	};
	await writeFile( generatedPath( "unclaimed-assets.json" ), JSON.stringify( report, null, "	" ) );
	return report;
}
