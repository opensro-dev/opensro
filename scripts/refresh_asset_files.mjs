/*
===========================================================================

refresh_asset_files.mjs - repack a few published files without the full repack

	pnpm task assets:refresh:files -- assets/data/teleportData.json [...]

A developer who changed one or two published files (a rebaked teleport
catalogue, a re-converted image) needs them in the packs the client loads,
and nothing else. `assets:repack` rebuilds every pack from the loose tree
(about 15 minutes on a full variant tree); this rebuilds only the groups
that hold the named files, through the same owner the focused families use
(ownedPackRefresh.mjs: merge with its schema guard, closure check,
superseded packs soft-archived, web manifest), in seconds.

Each file must already be written loose in the public tree: this packs, it
never produces. A file keeps the group that already holds it; a new image
takes imagePackGroup's group, a new model the dedicated group its document
names or else game-models (the full build's sweep), and any other new file
is refused, since only its builder knows where it belongs. Nothing is claimed in the
publication ledger: the files stay owned by the builders that wrote them,
and the ones no builder claims are named, as the next full build archives
them unless their builder publishes them by then.

===========================================================================
*/
import { readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { collectDedicatedModelGroups, imagePackGroup, isImageAsset } from "./build/assetPackOwnership.mjs";
import { normalizePublicAssetPath } from "./build/shared/assetPaths.mjs";
import { minifyJsonBytes } from "./build/jsonAssetCompression.mjs";
import { compressGzipSync, DEFAULT_GZIP_LEVEL } from "./build/shared/compressionUtils.mjs";
import { isMainScript } from "./build/shared/fsUtils.mjs";
import { refreshOwnedPackFiles } from "./build/shared/ownedPackRefresh.mjs";
import { PACK_INDEX_PATH } from "./build/shared/packGroupRefresh.mjs";
import { currentClaims, isClaimed } from "./build/shared/publicationLedger.mjs";
import { CLIENT_PUBLIC_ROOT } from "./lib/generatedRoot.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

// The publisher name: the incremental pack folder, the logs, the archive reason.
const PUBLISHER = "files";

// The compressed twin the packs carry for a JSON (jsonCompression).
const COMPRESSED_TWIN = ".gz";

// The full build's sweep for every model no dedicated group claims
// (assetPackGroups.mjs).
const GENERIC_MODEL_GROUP = "game-models";
const MODEL_PATTERN = /\.(?:glb|vat\.bin)$/i;

// The pack tail's own outputs: never pack members.
const PACK_TAIL = [ "/assets/packs/", "/assets/manifest.json" ];

/*
================
publicFiles

The named files as the public paths the packs hold, each written loose in
the public tree. members is the lower-cased paths of the pack index: a
JSON the packs carry only as its compressed twin (teleportData.json ships
as teleportData.json.gz) resolves to that twin. A leading slash is
optional (Git Bash rewrites "/assets/..." arguments into its install
folder). Throws on a path outside /assets/, a pack tail output or a file
that is not on disk.
================
*/
export async function publicFiles( names, members, publicRoot = CLIENT_PUBLIC_ROOT ) {
	if ( names.length === 0 ) throw new Error( "Name at least one published file (/assets/...)" );
	const files = [];
	for ( const name of names ) {
		// Throws on anything outside /assets/.
		const named = normalizePublicAssetPath( name.startsWith( "/" ) ? name : "/" + name );
		const twin = named + COMPRESSED_TWIN;
		const file = !members.has( named.toLowerCase() ) && members.has( twin.toLowerCase() ) ? twin : named;
		if ( PACK_TAIL.some( prefix => file.toLowerCase().startsWith( prefix ) ) ) {
			throw new Error( `${name}: the pack tail's own output, not a pack member` );
		}
		const info = await stat( path.join( publicRoot, file.slice( 1 ) ) ).catch( () => null );
		if ( !info?.isFile() ) throw new Error( `${file}: not written in the public tree (run its builder first)` );
		files.push( file );
	}
	return files;
}

/*
================
refreshCompressedTwin

The packs carry a JSON as its gzip twin, which the build's JSON pass
(jsonAssetCompression.mjs) derives from the minified file. A builder run
on its own rewrites only the JSON, so the twin is derived again here the
same way (minified in place, gzip at the pass's level): packing the stale
twin would ship the old content. Returns whether the twin changed.
================
*/
export async function refreshCompressedTwin( twinPath ) {
	const jsonPath = twinPath.slice( 0, -COMPRESSED_TWIN.length );
	const raw = await readFile( jsonPath ).catch( () => null );
	if ( !raw ) return false;
	const minified = minifyJsonBytes( raw );
	if ( !raw.equals( minified ) ) await writeFile( jsonPath, minified );
	const compressed = compressGzipSync( minified, { level: DEFAULT_GZIP_LEVEL } );
	const previous = await readFile( twinPath ).catch( () => null );
	if ( previous?.equals( compressed ) ) return false;
	await writeFile( twinPath, compressed );
	return true;
}

/*
================
newFileGroup

The group a file no pack holds yet joins: an image goes where
imagePackGroup puts it; a model where its document's dedicated group
(dedicated, lower-cased path -> group) puts it, else game-models, as the
full build sweeps it; anything else has no owner here.
================
*/
export function newFileGroup( file, dedicated = new Map() ) {
	if ( isImageAsset( file ) ) return imagePackGroup( file );
	if ( MODEL_PATTERN.test( file ) ) return dedicated.get( file.toLowerCase() ) ?? GENERIC_MODEL_GROUP;
	return undefined;
}

/*
================
dedicatedOwners

lower-cased path -> dedicated model group for the named files, from the
documents the full build reads (collectDedicatedModelGroups).
================
*/
async function dedicatedOwners( files ) {
	const { groups } = await collectDedicatedModelGroups(
		CLIENT_PUBLIC_ROOT,
		new Set( files.map( file => file.toLowerCase() ) )
	);
	return new Map( groups.flatMap( group => group.files.map( file => [ file.toLowerCase(), group.name ] ) ) );
}

/*
================
main
================
*/
async function main() {
	// Everything that reads or writes the shared public tree runs under the
	// generated-assets lock: a full build or another refresh in any worktree
	// may be rewriting the pack index, a JSON or its twin at the same time.
	const files = await withGeneratedAssetsLock( "Asset files refresh", async () => {
		const index = JSON.parse( await readFile( PACK_INDEX_PATH, "utf8" ) );
		const members = new Set( index.assets.map( row => row.path.toLowerCase() ) );
		const files = await publicFiles( process.argv.slice( 2 ), members );
		const dedicated = await dedicatedOwners( files );
		for ( const file of files.filter( file => file.toLowerCase().endsWith( ".json" + COMPRESSED_TWIN ) ) ) {
			if ( await refreshCompressedTwin( path.join( CLIENT_PUBLIC_ROOT, file.slice( 1 ) ) ) ) {
				console.log( `${file}: derived again from its JSON` );
			}
		}
		const { updates } = await refreshOwnedPackFiles( {
			name: PUBLISHER,
			files,
			defaultGroup: file => newFileGroup( file, dedicated ),
			manifestSidecars: {}
		} );
		const built = updates.reduce( ( count, update ) => count + (update.builtPackCount ?? 0), 0 );
		console.log( `Repacked ${files.length} file(s); ${built} pack(s) rebuilt.` );
		return files;
	} );
	const claimed = await currentClaims();
	for ( const file of files.filter( file => !isClaimed( file, claimed ) ) ) {
		console.warn(
			`${file}: no builder claims it; the next full build archives it unless its builder publishes it.`
		);
	}
}

if ( isMainScript( import.meta.url ) ) await main();
