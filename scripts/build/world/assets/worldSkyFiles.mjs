/*
===========================================================================

worldSkyFiles.mjs - which published world files carry a sky, without reading them all

Two families (flares, star-rng in families/looseFamilies.mjs) rewrite the
sky of every published world file that has one. Six of the 12,523 world
JSON files do (the map-loader region bundles that embed their own sky and
the outdoor shared render resources), but the world format names no index
of them, so each pass read all 3.7 GB to find the six: 36 s of the
families' 73 s on release 67b727e6c.

The verdict "this content carries a sky" belongs to the content, so it is
kept by content hash: the file hash cache (shared/fileHashCache.mjs, which
the pack pass just before the families has already filled for these
files) gives a file's sha256 from a stat, and the ledger here maps a
sha256 to its verdict. A file is read only when its hash or its verdict is
unknown, and every sky file is read fresh, since its bytes are what the
caller parses. The ledger is rewritten from each sweep, so it holds
exactly the verdicts of the tree it last saw.

A content change that keeps a file's size and mtime is invisible here, as
it is to the pack pass (the file hash cache's own caveat).
SRO_BUILD_HASH_CACHE=0 reads every file, as before.

===========================================================================
*/
import { readdir, readFile, stat } from "node:fs/promises";
import path from "node:path";
import { buildStatePath } from "../../../lib/generatedRoot.mjs";
import { fileHashCacheDisabled, openFileHashCache } from "../../shared/fileHashCache.mjs";
import { readJsonOrUndefined } from "../../shared/jsonOut.mjs";
import { writeStateJson } from "../../shared/stateFile.mjs";

const LEDGER_FORMAT = "sro-world-sky-ledger";
const LEDGER_VERSION = 1;
// The top-level key a sky-carrying world file spells. The world writers are
// JSON.stringify, which never escapes a key's characters.
export const SKY_KEY = Buffer.from( '"sky"' );

/*
================
openLedger

The sha256 -> carries-a-sky verdicts of the last sweep; empty when absent,
of another format, or when the hash cache is bypassed.
================
*/
async function openLedger( ledgerPath ) {
	if ( fileHashCacheDisabled() ) return Object.create( null );
	const parsed = await readJsonOrUndefined( ledgerPath );
	if ( parsed?.format === LEDGER_FORMAT && parsed?.version === LEDGER_VERSION && parsed.verdicts ) {
		return Object.assign( Object.create( null ), parsed.verdicts );
	}
	return Object.create( null );
}

/*
================
worldSkyFiles

Every .json file under worldRoot whose bytes contain SKY_KEY, in readdir
order, each as { file, bytes } with the bytes read by this call. options
name other cache and ledger files (tests); by default both live in the
build state beside the generated tree.
================
*/
export async function worldSkyFiles( worldRoot, options = {} ) {
	const hashCache = options.hashCache ?? (await openFileHashCache( options.hashCachePath ));
	const ledgerPath = options.ledgerPath ?? buildStatePath( "world-sky-ledger.json" );
	const known = await openLedger( ledgerPath );
	const verdicts = Object.create( null );
	const found = [];
	for ( const name of await readdir( worldRoot, { recursive: true } ) ) {
		if ( !name.endsWith( ".json" ) ) continue;
		const file = path.join( worldRoot, name );
		const fileStat = await stat( file );
		const sha256 = hashCache.peekFileHash( file, fileStat );
		if ( sha256 !== undefined && known[sha256] === false ) {
			verdicts[sha256] = false;
			continue;
		}
		const bytes = await readFile( file );
		const has = bytes.includes( SKY_KEY );
		verdicts[hashCache.noteFileBytes( file, fileStat, bytes )] = has;
		if ( has ) found.push( { file, bytes } );
	}
	await hashCache.save();
	if ( !fileHashCacheDisabled() ) {
		await writeStateJson( ledgerPath, { format: LEDGER_FORMAT, version: LEDGER_VERSION, verdicts } );
	}
	return found;
}
