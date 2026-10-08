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
current claims that output explicitly with claimPublicFile or
claimPublicPaths (shared/publicWrite.mjs writes and claims in one call).
Claims are checked against what the claimed manifests name
(referencedFiles): a named file no step claimed exists only because of an
earlier run on this machine, so a fresh clone would not have it, and the
build, the ledger report and the packager refuse it.

===========================================================================
*/
import { containedPublicFile } from "./assetPaths.mjs";
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
	// files maps a lower-cased public path to its spelling on disk.
	// walked: files claimKeptOutput has already followed in this run.
	current = { owner, complete, files: new Map(), walked: new Set() };
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
	if ( publicPath && !current.files.has( publicPath.toLowerCase() ) ) {
		current.files.set( publicPath.toLowerCase(), publicPath );
	}
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
claimKeptOutput

A builder that keeps an earlier output because it is current claims it and
everything it names, transitively (a kept region bundle names its lightmaps,
a kept object index its meshes and textures): a complete run replaces the
owner's record, so a kept output that claimed only itself would leave its
references to the audit as files no step produced. A builder that recorded
the output's references when it wrote it passes them (namedPublicPaths) and
the output is not read again.

One walk serves the whole run: every region bundle names the shared object
index, which names every mesh, so a fresh walk per region re-read the same
5,844 files (277 MiB) 2,123 times (835 s). A file already walked in this
publication was claimed with everything it names, and claims only grow until
the run commits, so a later walk stops there.
================
*/
/** @param {string} absolutePath @param {Iterable<string>} [named] */
export async function claimKeptOutput( absolutePath, named ) {
	if ( !current ) return;
	claimPublicFile( absolutePath );
	const publicPath = toPublicPath( absolutePath );
	if ( !publicPath || current.walked.has( publicPath.toLowerCase() ) ) return;
	current.walked.add( publicPath.toLowerCase() );
	if ( named === undefined ) {
		const references = await referencedFiles(
			new Map( [ [ publicPath.toLowerCase(), publicPath ] ] ),
			current.walked
		);
		claimPublicPaths( references.values() );
		return;
	}
	const start = new Map();
	for ( const reference of named ) {
		const key = reference.toLowerCase();
		if ( current.walked.has( key ) ) continue;
		current.walked.add( key );
		start.set( key, reference );
	}
	claimPublicPaths( start.values() );
	claimPublicPaths( (await referencedFiles( start, current.walked )).values() );
}

/*
================
namedPublicPaths

The /assets/... paths a JSON text names, in the spelling it wrote: what
claimKeptOutput follows, recorded by a builder when it writes the text.
================
*/
export function namedPublicPaths( text ) {
	return [ ...new Set( Array.from( text.matchAll( REFERENCED_PUBLIC_PATH ), ( [, reference] ) => reference ) ) ];
}

// A JSON string value that is a public path; paths never contain a quote or backslash.
const REFERENCED_PUBLIC_PATH = /"(\/assets\/[^"\\]+)"/g;

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
	const merged = new Set( files.values() );
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
withPublication

Runs task as its own owner inside another open publication (the full build
running a focused family): the open owner is set aside, task's claims are
committed as owner's complete record, and the outer owner resumes. A failed
task records nothing.
================
*/
export async function withPublication( owner, task ) {
	const outer = current;
	current = null;
	beginPublication( owner );
	try {
		const result = await task();
		await commitPublication();
		return result;
	} finally {
		current = outer;
	}
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

Every owner's record: { owners: Map<owner, Map<lower-cased path, spelling>>,
claimed: Set<lower-cased path> }. The open publication of this process
counts with what it has claimed so far, replacing its committed record: the
full build audits its own claims before it commits them.
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
		owners.set( record.owner, new Map( record.files.map( file => [ file.toLowerCase(), file ] ) ) );
	}
	if ( current ) {
		const previous = current.complete ? [] : owners.get( current.owner ) ?? [];
		owners.set( current.owner, new Map( [ ...previous, ...current.files ] ) );
	}
	for ( const files of owners.values() ) for ( const file of files.keys() ) claimed.add( file );
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
referencedFiles

Every /assets/... file the claimed JSON outputs name, transitively: the
client loads exactly what its manifests and catalogs reference. spellings
maps each claimed lower-cased path to its spelling on disk; returns the
referenced paths that are not themselves claimed, lower-cased path to the
spelling the manifest wrote. seen (default: the claimed paths) holds what is
already walked; a caller sharing it across calls walks each file once.
================
*/
async function referencedFiles( spellings, seen = new Set( spellings.keys() ) ) {
	const referenced = new Map();
	const pending = [ ...spellings.values() ];
	while ( pending.length > 0 ) {
		const publicPath = pending.pop();
		if ( !publicPath.toLowerCase().endsWith( ".json" ) ) continue;
		let text;
		try {
			text = await readFile( path.join( CLIENT_PUBLIC_ROOT, publicPath.slice( 1 ) ), "utf8" );
		} catch ( error ) {
			if ( error.code === "ENOENT" ) continue;
			throw error;
		}
		for ( const reference of namedPublicPaths( text ) ) {
			const key = reference.toLowerCase();
			if ( seen.has( key ) ) continue;
			seen.add( key );
			referenced.set( key, reference );
			pending.push( reference );
		}
	}
	return referenced;
}

/*
================
ledgerStatus

Splits the records into the claims of known owners, the files their
manifests name that no owner claimed, the expected owners with no record,
and records of owners that no longer exist (a renamed or removed family),
which claim nothing.
================
*/
async function ledgerStatus() {
	const { owners } = await readClaims();
	const expected = expectedOwners();
	const known = new Set( [ ...expected, ...OPTIONAL_OWNERS ] );
	const spellings = new Map();
	for ( const [owner, files] of owners ) {
		if ( known.has( owner ) ) { for ( const [key, spelling] of files ) spellings.set( key, spelling ); }
	}
	return {
		owners,
		claimed: new Set( spellings.keys() ),
		referenced: await referencedFiles( spellings ),
		missingOwners: expected.filter( owner => !owners.has( owner ) ),
		retiredOwners: [ ...owners.keys() ].filter( owner => !known.has( owner ) )
	};
}

/*
================
classify

Sorts public files against the ledger: claimed files are this pipeline's
output; local-only files are named by a claimed manifest yet produced by
no current step (they exist only because of an earlier run on this
machine); everything else is unclaimed garbage.
================
*/
function classify( rows, status ) {
	const localOnly = [], unclaimed = [];
	for ( const row of rows ) {
		if ( isClaimed( row.path, status.claimed ) ) continue;
		(isClaimed( row.path, status.referenced ) ? localOnly : unclaimed).push( row );
	}
	return { localOnly, unclaimed };
}

/*
================
auditClaims

Compares the pack groups' files with the ledger. With every expected owner
recorded:
- a file a claimed manifest names but no current step produced fails the
  build: a fresh clone would not have it, so the release would break for
  everyone but this machine;
- every other file no owner claimed is soft-archived (moved through
  archiveGeneratedArtifact, never deleted) and dropped from its group, and
  records of retired owners are archived too.
With an owner missing nothing moves and nothing fails: the files of a
family that never ran cannot be told from garbage. Writes the report to
generatedPath( "unclaimed-assets.json" ) and returns it with the groups to
pack. options.archiveRoot overrides the archive location (tests).
================
*/
/** @param {{ publicRoot?: string, archiveRoot?: string }} [options] */
export async function auditClaims( groups, options = {} ) {
	const { publicRoot = CLIENT_PUBLIC_ROOT, archiveRoot } = options;
	const status = await ledgerStatus();
	const rows = [];
	for ( const group of groups ) {
		for ( const publicPath of group.files ) {
			if ( isClaimed( publicPath, status.claimed ) ) continue;
			const bytes = (await stat( containedPublicFile( publicRoot, publicPath ) )).size;
			rows.push( { path: publicPath, group: group.name, bytes } );
		}
	}
	const { localOnly, unclaimed } = classify( rows, status );
	const complete = status.missingOwners.length === 0;
	const report = {
		complete,
		archived: complete && localOnly.length === 0,
		missingOwners: status.missingOwners,
		retiredOwners: status.retiredOwners,
		owners: [ ...status.owners ].map( ( [owner, files] ) => ({ owner, files: files.size }) ),
		localOnly,
		files: unclaimed.length,
		bytes: unclaimed.reduce( ( sum, row ) => sum + row.bytes, 0 ),
		folders: folderTotals( unclaimed ),
		unclaimed
	};
	await writeFile( generatedPath( "unclaimed-assets.json" ), JSON.stringify( report, null, "\t" ) );
	if ( complete && localOnly.length > 0 ) {
		throw new Error(
			`${localOnly.length} file(s) are named by a manifest but no current build step produced them, ` +
				"so a fresh clone would not have them (write them through shared/publicWrite.mjs or claim the " +
				`step's kept output; .generated/unclaimed-assets.json lists them): ` +
				localOnly.slice( 0, 8 ).map( row => row.path ).join( ", " )
		);
	}
	if ( report.archived ) {
		for ( const row of unclaimed ) {
			// The whole compressed family goes: a stale base left behind would
			// have its packed .gz regenerated by the next build's JSON compressor.
			for ( const member of compressedFamily( row.path ) ) {
				if ( member !== row.path && isClaimed( member, status.claimed ) ) continue;
				await archiveGeneratedArtifact( containedPublicFile( publicRoot, member ), {
					scopeRoot: publicRoot,
					archiveRoot,
					reason: "unclaimed-public-asset"
				} );
			}
		}
		for ( const owner of status.retiredOwners ) {
			await archiveGeneratedArtifact( path.join( ledgerRoot(), `${owner}.json` ), {
				scopeRoot: ledgerRoot(),
				archiveRoot,
				reason: "retired-publication-owner"
			} );
		}
	}
	const archived = new Set( report.archived ? unclaimed.map( row => row.path ) : [] );
	return {
		...report,
		groups: groups.map( group => ({ ...group, files: group.files.filter( file => !archived.has( file ) ) }) )
	};
}

/*
================
indexClaimReport

Read-only: the published pack index's assets no build owner claims (split
into local-only files a manifest names and unclaimed garbage), with totals
per folder, and the expected owners that have no record. Nothing moves;
`pnpm assets ledger` prints it and the release packager refuses on
anything but an empty report (verifyIndexClaims).
================
*/
export async function indexClaimReport( index ) {
	const status = await ledgerStatus();
	const { localOnly, unclaimed } = classify(
		index.assets.map( asset => ({ path: asset.path, group: asset.group, bytes: asset.length }) ),
		status
	);
	return {
		missingOwners: status.missingOwners,
		retiredOwners: status.retiredOwners,
		owners: [ ...status.owners ].map( ( [owner, files] ) => ({ owner, files: files.size }) ),
		localOnly,
		files: unclaimed.length,
		bytes: unclaimed.reduce( ( sum, row ) => sum + row.bytes, 0 ),
		folders: folderTotals( unclaimed ),
		unclaimed
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
	const report = await indexClaimReport( index );
	const problems = report.missingOwners.map( owner => `no publication record for ${owner}` );
	if ( report.localOnly.length > 0 ) {
		problems.push(
			`${report.localOnly.length} packed asset(s) named by a manifest but produced by no current build step, e.g. ` +
				report.localOnly.slice( 0, 5 ).map( row => row.path ).join( ", " )
		);
	}
	if ( report.files > 0 ) {
		problems.push(
			`${report.files} packed asset(s) claimed by no build owner, e.g. ` +
				report.unclaimed.slice( 0, 5 ).map( row => row.path ).join( ", " )
		);
	}
	return problems;
}

/*
================
compressedFamily

A public path with its base and every precompressed sidecar of that base
(x.json, x.json.gz, x.json.br, x.json.zst); a path with no sidecar suffix
is its own base. archiveGeneratedArtifact skips members that do not exist.
================
*/
function compressedFamily( publicPath ) {
	const base = publicPath.replace( PRECOMPRESSED, "" );
	return [ base, ...[ ".gz", ".br", ".zst" ].map( suffix => base + suffix ) ];
}

/*
================
folderTotals

Unclaimed rows summed per folder (the first four path segments), largest first.
================
*/
function folderTotals( rows ) {
	const folders = new Map();
	for ( const row of rows ) {
		const folder = row.path.split( "/" ).slice( 0, 5 ).join( "/" );
		const total = folders.get( folder ) ?? { folder, files: 0, bytes: 0 };
		total.files++;
		total.bytes += row.bytes;
		folders.set( folder, total );
	}
	return [ ...folders.values() ].sort( ( left, right ) => right.bytes - left.bytes );
}
