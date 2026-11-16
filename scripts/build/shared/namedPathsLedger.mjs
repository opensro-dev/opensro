/*
===========================================================================

namedPathsLedger.mjs - the public paths a JSON output names, without reading it

The claim audit and every kept output walk the claimed JSON outputs for
the /assets/... paths they name (publicationLedger.mjs referencedFiles).
On a warm build that read every claimed JSON in full, 8,662 files and
3 GB, to find 68,612 references in 4.6 MB of them.

What a file names belongs to its bytes, so it is kept by content hash, as
worldSkyFiles.mjs keeps its sky verdicts: a hash cache of its own answers
a file's sha256 from its size and mtime, and the ledger maps a sha256 to
the paths those bytes name. A file is read only when its hash or its
entry is unknown. The ledger is void when the path pattern changes. A
save keeps every entry, aging the ones this process did not ask about,
and drops an entry unseen for MAX_UNSEEN_SAVES saves: a test that walks
three files through the default state must not shrink the build's ledger
to three entries (the fileHashCache.mjs save note), yet content that is
gone must not pile up. Both files are opened once per process and saved
when the event loop drains; a process that exits early only loses that
run's entries.

A content change that keeps a file's size and mtime is invisible here, as
it is to every fileHashCache user. SRO_BUILD_HASH_CACHE=0 reads every
file and saves no ledger.

===========================================================================
*/
import { readFile, stat } from "node:fs/promises";
import { buildStatePath } from "../../lib/generatedRoot.mjs";
import { fileHashCacheDisabled, openFileHashCache } from "./fileHashCache.mjs";
import { readJsonOrUndefined } from "./jsonOut.mjs";
import { writeStateJson } from "./stateFile.mjs";

const LEDGER_FORMAT = "sro-named-paths-ledger";
const LEDGER_VERSION = 1;
// Saves an entry may go unasked before it is dropped: a few full builds.
const MAX_UNSEEN_SAVES = 8;

// A JSON string value that is a public path; paths never contain a quote or backslash.
const REFERENCED_PUBLIC_PATH = /"(\/assets\/[^"\\]+)"/g;

let processLedger = null;

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

/*
================
openNamedPathsLedger

A ledger over its own hash cache and ledger files. options.statePath and
options.hashCachePath name other files (tests); by default both live in
the build state beside the generated tree.
================
*/
export async function openNamedPathsLedger( options = {} ) {
	const statePath = options.statePath ?? buildStatePath( "named-paths-ledger.json" );
	const hashCache = await openFileHashCache(
		options.hashCachePath ?? buildStatePath( "named-paths-hash-cache.json" )
	);
	// sha256 -> { paths, unseen }: unseen counts saves since a process asked.
	const known = Object.create( null );
	const parsed = fileHashCacheDisabled() ? undefined : await readJsonOrUndefined( statePath );
	if (
		parsed?.format === LEDGER_FORMAT && parsed?.version === LEDGER_VERSION &&
		parsed?.pattern === REFERENCED_PUBLIC_PATH.source && parsed.named
	) {
		Object.assign( known, parsed.named );
	}
	const seen = new Set();
	return {
		/*
		The paths absolutePath names, or undefined when it does not exist.
		*/
		async namedPaths( absolutePath ) {
			let fileStat;
			try {
				fileStat = await stat( absolutePath );
			} catch ( error ) {
				if ( error?.code === "ENOENT" ) return undefined;
				throw error;
			}
			const digest = hashCache.peekFileHash( absolutePath, fileStat );
			if ( digest && known[digest] ) {
				seen.add( digest );
				return known[digest].paths;
			}
			const bytes = await readFile( absolutePath );
			const paths = namedPublicPaths( bytes.toString( "utf8" ) );
			const sha256 = hashCache.noteFileBytes( absolutePath, fileStat, bytes );
			known[sha256] = { paths, unseen: 0 };
			seen.add( sha256 );
			return paths;
		},
		async save() {
			await hashCache.save();
			if ( fileHashCacheDisabled() ) return;
			const named = Object.create( null );
			for ( const [digest, entry] of Object.entries( known ) ) {
				const unseen = seen.has( digest ) ? 0 : entry.unseen + 1;
				if ( unseen <= MAX_UNSEEN_SAVES ) named[digest] = { paths: entry.paths, unseen };
			}
			await writeStateJson( statePath, {
				format: LEDGER_FORMAT,
				version: LEDGER_VERSION,
				pattern: REFERENCED_PUBLIC_PATH.source,
				named
			} );
		}
	};
}

/*
================
processNamedPathsLedger

The ledger this process shares, opened on first use and saved when the
event loop drains.
================
*/
export function processNamedPathsLedger() {
	if ( !processLedger ) {
		processLedger = openNamedPathsLedger();
		process.once( "beforeExit", () => processLedger.then( ledger => ledger.save() ) );
	}
	return processLedger;
}
