/*
===========================================================================

generatedAssetMembership.test.mjs - every published file is accounted for

Checks the generated public tree against its two manifests: every web
manifest entry is a pack member, a file the pack garbage rule retains under
/assets/packs/, or a documented loose exception; the tree on disk and the
web manifest agree in both directions; precompressed sidecars are fresh; and
each generated asset family lands in its expected pack group.

Needs the full asset build (.generated/client-public).

===========================================================================
*/

import { CLIENT_PUBLIC_ROOT } from "../../lib/generatedRoot.mjs";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { test } from "node:test";
import { evaluatePrecompressedFreshness } from "../../checks/check_precompressed_freshness.mjs";
import { PRECOMPRESSED_ASSET_SUFFIXES } from "../../build/jsonAssetCompression.mjs";
import {
	computeWebManifestHash,
	isRegisterableAssetFile,
	listFilesUnder,
	statFilesByPath,
	toPublicAssetPath,
	verifyWebAssetManifestContent
} from "../../build/webManifest.mjs";
import { readPublishedAssetJson } from "../../lib/publishedAsset.mjs";
import { collectPackGarbage } from "../../build/assetPackGarbage.mjs";
import { collectDedicatedModelGroups } from "../../build/assetPackOwnership.mjs";
import { loadEnglishCompletions } from "../../build/shared/englishCompletions.mjs";

const publicRoot = CLIENT_PUBLIC_ROOT;
const webManifestPath = path.join( publicRoot, "assets/manifest.json" );
const packManifestPath = path.join( publicRoot, "assets/packs/manifest.json" );
const uiPreloadManifestPath = path.join( publicRoot, "assets/ui/preload-images.json" );

const EXPLICIT_LOOSE_ASSETS = new Set( [
	"/assets/fonts/sro-chat.ttf",
	"/assets/fonts/sro-default.ttf",
	"/assets/fonts/sro-english.ttf",
	"/assets/textdata/abusefilter.txt",
	// Raw native config bytes for the console-command registry (sub_68d9c0 twin):
	// copied verbatim by resourcePipeline.mjs and prefetched at mission mount,
	// same loose-text contract as abusefilter.txt.
	"/assets/config/command.txt"
] );

test("generated public assets have explicit pack membership or a documented loose exception", async () => {
	const { webPaths, packAssetsByPath } = await loadGeneratedAssetMembership();
	const retainedPackFile = await loadRetainedPackFiles();
	const missing = [];

	for ( const publicPath of webPaths ) {
		const lowerPath = publicPath.toLowerCase();
		const gzipSidecar = packAssetsByPath.get( `${lowerPath}.gz` );
		if (
			packAssetsByPath.has( lowerPath ) ||
			retainedPackFile( lowerPath ) ||
			gzipSidecar?.group === "game-data" ||
			gzipSidecar?.group === "developer-labs" ||
			gzipSidecar?.group === "outdoor-world" ||
			EXPLICIT_LOOSE_ASSETS.has( lowerPath )
		) {
			continue;
		}

		missing.push( publicPath );
	}

	assert.deepEqual( missing, [] );
});

// Everything above iterates the manifests, so a file that was never registered at all is
// invisible to it: two cursors were dropped into public/assets without re-running the
// resource build, appeared in neither manifest, and were fetched live after world reveal.
// The three tests below walk the tree on disk instead and require it to agree with the
// manifest exactly, in both directions, using the builder's own membership predicate.

test("every source asset on disk is registered in the generated web manifest", async () => {
	const { unregistered } = await loadAssetTreeMembership();

	assert.deepEqual( unregistered, [] );
});

test("every generated web manifest entry still has a file behind it on disk", async () => {
	const { absent } = await loadAssetTreeMembership();

	assert.deepEqual( absent, [] );
});

test("the asset tree walk excludes nothing beyond the manifest itself and precompressed sidecars", async () => {
	const { excludedCount, unexplainedExclusions, orphanSidecars } = await loadAssetTreeMembership();

	// A guard can be made to pass by excluding enough of the tree, so name every excluded
	// file's reason rather than trusting the totals.
	assert.deepEqual( unexplainedExclusions, [] );
	// A sidecar is exempt only because the asset it was derived from is itself registered.
	// One with nothing beside it is an unregistered file hiding inside that exemption.
	assert.deepEqual( orphanSidecars, [] );
	assert.ok( excludedCount > 0, "expected the walk to reach the precompressed sidecars it exempts" );
});

// Everything above is a MEMBERSHIP guard: it proves every file is registered and every
// registration has a file. A registered file whose BYTES change keeps its path and stays green
// in all of them. The manifest records size+sha256 for all of its entries and until the tests
// below nothing compared either against disk at test time -- which is exactly the shape of the
// stale-sidecar defect shipped on 2026-07-25, where correct membership hid wrong content.

test("every registered asset still has the size the web manifest recorded", async () => {
	const { missing, sizeMismatches } = await loadManifestContentAudit();

	// Size is read from a fresh stat and never from the hash cache, so this half of the content
	// check cannot be fooled by a modification that preserves mtime.
	assert.deepEqual( sizeMismatches, [] );
	assert.deepEqual( missing, [] );
});

test("every registered asset still hashes to the sha256 the web manifest recorded", async () => {
	const { hashMismatches, verified } = await loadManifestContentAudit();

	assert.deepEqual( hashMismatches, [] );
	assert.ok( verified > 0, "expected the audit to reach the registered assets it verifies" );
});

test("the generated web manifest's own manifestHash still describes its contents", async () => {
	const webManifest = await readJson( webManifestPath );

	// Without this the two guards above could be silenced by editing the recorded hashes rather
	// than fixing the bytes: manifestHash covers {version, files}, so a doctored entry breaks it.
	assert.equal( computeWebManifestHash( webManifest.version, webManifest.files ), webManifest.manifestHash );
});

// Sidecar staleness lives here rather than in check_precompressed_freshness.mjs's CLI because
// that CLI is not in the `check` chain and nothing imported it, so the staleness half -- a
// sidecar older than its source, i.e. the 2026-07-25 defect itself -- was gated by nothing that
// runs. This file is already in `test:assets`, and the scan reuses the sweep above, so the gate
// costs no additional walk. The orphan half is covered by the exclusion test above.
test("no precompressed sidecar is older than the asset it shadows", async () => {
	const { publicFiles, statsByPath } = await loadPublicTreeScan();
	const { scanned, stale } = evaluatePrecompressedFreshness( publicFiles, statsByPath );

	assert.deepEqual( stale.map( describeStaleSidecar ), [] );
	assert.ok( scanned > 0, "expected the sweep to reach the precompressed sidecars it checks" );
});

test("generated pack artifacts are not packed as ordinary assets", async () => {
	const { packIndex } = await loadGeneratedAssetMembership();
	const forbidden = packIndex.assets
		.map( ( asset ) => normalizePublicPath( asset.path ) )
		.filter(
			( publicPath ) =>
				publicPath.toLowerCase().startsWith( "/assets/packs/" ) ||
				/^\/assets\/manifest\.json\.(?:br|gz|zst)$/i.test( publicPath )
		);

	assert.deepEqual( forbidden, [] );
});

test("native texture containers have image-family ownership expectations", () => {
	assert.equal( expectedPackGroup( "/assets/world/dungeon/wall.texture", new Set() ), "world-textures" );
	assert.equal(
		expectedPackGroup( "/assets/images/Particles_extracted/textures/spark.TEXTURE", new Set() ),
		"particle-textures"
	);
});

test("generated asset families land in their expected pack groups", async () => {
	const { packIndex, packAssetsByPath, uiPreloadImages } = await loadGeneratedAssetMembership();
	const mismatches = [];
	// The dedicated model groups (equipment, Hwan, pets) come from
	// the ownership authority, as in the build; they take precedence there too.
	const { groups: dedicatedGroups } = await collectDedicatedModelGroups(
		publicRoot,
		new Set( packAssetsByPath.keys() )
	);
	const dedicatedGroup = new Map(
		dedicatedGroups.flatMap( ( group ) =>
			group.files.map( ( file ) => [ normalizePublicPath( file ).toLowerCase(), group.name ] )
		)
	);

	for ( const asset of packIndex.assets ) {
		const publicPath = normalizePublicPath( asset.path );
		const expectedGroup = dedicatedGroup.get( publicPath.toLowerCase() ) ??
			expectedPackGroup( publicPath, uiPreloadImages );
		if ( expectedGroup && asset.group !== expectedGroup ) {
			mismatches.push( `${publicPath}: expected ${expectedGroup}, got ${asset.group}` );
		}
	}

	for ( const imagePath of uiPreloadImages ) {
		assert.equal( packAssetsByPath.get( imagePath )?.group, "native-ui", `${imagePath} should be in native-ui` );
	}

	assert.deepEqual( mismatches, [] );
});

test("packed game-data bytes match their current generated source files", async ( t ) => {
	const { packIndex } = await loadGeneratedAssetMembership();
	const mismatches = [];
	const gameDataAssets = packIndex.assets.filter( ( entry ) => entry.group === "game-data" );
	const firstSourcePath = path.join(
		publicRoot,
		...normalizePublicPath( gameDataAssets[0].path ).slice( 1 ).split( "/" )
	);
	const hasLooseAuthority = await readFile( firstSourcePath ).then( () => true, ( error ) => {
		if ( error?.code === "ENOENT" ) return false;
		throw error;
	} );
	assert.ok(
		hasLooseAuthority,
		`loose game-data authority ${firstSourcePath} is missing; run pnpm assets build full`
	);

	for ( const asset of gameDataAssets ) {
		const publicPath = normalizePublicPath( asset.path );
		const sourcePath = path.join( publicRoot, ...publicPath.slice( 1 ).split( "/" ) );
		const bytes = await readFile( sourcePath );
		const sha256 = createHash( "sha256" ).update( bytes ).digest( "hex" );

		if ( bytes.length !== asset.length || sha256 !== asset.sha256 ) {
			mismatches.push(
				`${publicPath}: pack manifest ${asset.length}/${asset.sha256}, source ${bytes.length}/${sha256}`
			);
		}
	}

	assert.deepEqual( mismatches, [] );
});

test("generated message-tip catalog contains native right-box guide rows and is packed as game-data", async () => {
	const [catalog, { packAssetsByPath }] = await Promise.all( [
		readJson( path.join( publicRoot, "assets/text/messagetips.en.json" ) ),
		loadGeneratedAssetMembership()
	] );

	assert.equal( catalog.format, "sro-messagetipdata" );
	assert.equal( catalog.language, "en" );
	assert.deepEqual( catalog.unresolvedTextKeys, [] );

	const rowsByTextKey = new Map( catalog.rows.map( ( row ) => [ row.textKey, row ] ) );
	assert.equal(
		rowsByTextKey.get( "SRO_MSGTIP_11" )?.text,
		"[Guide] The stronger the monster, the more experience you get but the riskier."
	);
	assert.equal(
		rowsByTextKey.get( "SRO_MSGTIP_25" )?.text,
		"[Guide] If your power is high, your physical balance is increased while your magical balance is decreased."
	);
	assert.equal( rowsByTextKey.get( "SRO_MSGTIP_11" )?.type, 3 );
	assert.equal( rowsByTextKey.get( "SRO_MSGTIP_11" )?.minLevel, 1 );
	assert.equal( rowsByTextKey.get( "SRO_MSGTIP_11" )?.maxLevel, 40 );
	// Retail ships this tip without English; the product localization layer
	// (englishCompletions/texthelp.json) supplies it.
	assert.equal(
		rowsByTextKey.get( "SRO_MSGTIP_110" )?.text,
		loadEnglishCompletions( "texthelp.txt" ).SRO_MSGTIP_110?.english
	);
	assert.equal(
		packAssetsByPath.get( "/assets/text/messagetips.en.json" )?.group ??
			packAssetsByPath.get( "/assets/text/messagetips.en.json.gz" )?.group,
		"game-data"
	);
});

test("pack files listed by the pack manifest are present in the generated web manifest", async () => {
	const { packIndex, webPathSet } = await loadGeneratedAssetMembership();
	const missing = packIndex.groups
		.flatMap( ( group ) => group.packs )
		.filter( ( pack ) => !webPathSet.has( normalizePublicPath( pack.path ).toLowerCase() ) )
		.map( ( pack ) => normalizePublicPath( pack.path ) );

	assert.deepEqual( missing, [] );
});

/*
================
loadGeneratedAssetMembership
================
*/
async function loadGeneratedAssetMembership() {
	const [webManifest, packIndex, uiPreloadManifest] = await Promise.all( [
		readJson( webManifestPath ),
		readJson( packManifestPath ),
		readJson( uiPreloadManifestPath )
	] );
	const webPaths = webManifest.files.map( ( file ) => normalizePublicPath( file.path ) ).sort( ( left, right ) =>
		left.localeCompare( right )
	);
	const webPathSet = new Set( webPaths.map( ( publicPath ) => publicPath.toLowerCase() ) );
	const packAssetsByPath = new Map(
		packIndex.assets.map( ( asset ) => [ normalizePublicPath( asset.path ).toLowerCase(), asset ] )
	);
	const uiPreloadImages = new Set(
		uiPreloadManifest.images.map( ( image ) => normalizePublicPath( image.path ).toLowerCase() )
	);

	return {
		webManifest,
		packIndex,
		uiPreloadManifest,
		webPaths,
		webPathSet,
		packAssetsByPath,
		uiPreloadImages
	};
}

let publicTreeScanPromise;

/*
================
loadPublicTreeScan

One walk + one stat sweep of .generated/client-public, shared by every test below: membership
(which needs the file list), content drift (sizes) and sidecar freshness (mtimes). Walking
this tree is the expensive part -- 45k files, and far worse when another lane is using the
disk -- so it happens exactly once per run rather than once per guard.
================
*/
function loadPublicTreeScan() {
	publicTreeScanPromise ??= scanPublicTree();
	return publicTreeScanPromise;
}

/*
================
scanPublicTree
================
*/
async function scanPublicTree() {
	const publicFiles = await listFilesUnder( publicRoot );
	const assetsRoot = path.join( publicRoot, "assets" );
	return {
		publicFiles,
		assetFiles: publicFiles.filter( ( filePath ) => filePath.startsWith( assetsRoot + path.sep ) ),
		statsByPath: await statFilesByPath( publicFiles )
	};
}

let assetTreeMembershipPromise;

/*
================
loadAssetTreeMembership

One walk of .generated/client-public/assets shared by the three tree tests.
================
*/
function loadAssetTreeMembership() {
	assetTreeMembershipPromise ??= computeAssetTreeMembership();
	return assetTreeMembershipPromise;
}

/*
================
computeAssetTreeMembership
================
*/
async function computeAssetTreeMembership() {
	const [{ assetFiles: treeFiles }, webManifest, packIndex] = await Promise.all( [
		loadPublicTreeScan(),
		readJson( webManifestPath ),
		readJson( packManifestPath )
	] );
	const treeFileSet = new Set( treeFiles.map( ( filePath ) => path.resolve( filePath ) ) );

	const onDisk = new Set();
	const orphanSidecars = [];
	const unexplainedExclusions = [];
	let excludedCount = 0;

	for ( const filePath of treeFiles ) {
		const publicPath = normalizePublicPath( toPublicAssetPath( filePath ) );
		if ( isRegisterableAssetFile( filePath ) ) {
			onDisk.add( publicPath );
			continue;
		}

		excludedCount += 1;
		const suffix = PRECOMPRESSED_ASSET_SUFFIXES.find( ( candidate ) => filePath.endsWith( candidate ) );
		if ( suffix ) {
			if ( !treeFileSet.has( path.resolve( filePath.slice( 0, -suffix.length ) ) ) ) {
				orphanSidecars.push( publicPath );
			}
			continue;
		}
		if ( path.resolve( filePath ) === path.resolve( webManifestPath ) ) {
			continue;
		}
		unexplainedExclusions.push( publicPath );
	}

	const registered = new Set( webManifest.files.map( ( file ) => normalizePublicPath( file.path ) ) );

	return {
		excludedCount,
		orphanSidecars: orphanSidecars.sort(),
		unexplainedExclusions: unexplainedExclusions.sort(),
		unregistered: [ ...onDisk ].filter( ( publicPath ) => !registered.has( publicPath ) ).sort(),
		absent: [ ...registered ].filter( ( publicPath ) => !onDisk.has( publicPath ) ).sort()
	};
}

let manifestContentAuditPromise;

/*
================
loadManifestContentAudit

One content audit shared by the size and sha256 guards.
================
*/
function loadManifestContentAudit() {
	manifestContentAuditPromise ??= loadPublicTreeScan().then( ( { statsByPath } ) =>
		verifyWebAssetManifestContent( { stats: statsByPath } )
	);
	return manifestContentAuditPromise;
}

/*
================
describeStaleSidecar
================
*/
function describeStaleSidecar( entry ) {
	const lagDays = ((entry.assetMs - entry.oldestSidecarMs) / 86_400_000).toFixed( 2 );
	return `${toPublicAssetPath( entry.assetPath )} [${
		entry.suffixes.join( "," )
	}]: sidecar is ${lagDays} day(s) older than the asset`;
}

/*
================
readJson
================
*/
async function readJson( filePath ) {
	return readPublishedAssetJson( filePath, publicRoot );
}

/*
================
expectedPackGroup
================
*/
function expectedPackGroup( publicPath, uiPreloadImages ) {
	const lowerPath = publicPath.toLowerCase();

	if ( uiPreloadImages.has( lowerPath ) ) {
		return "native-ui";
	}
	if (
		lowerPath.startsWith( "/assets/images/media_extracted/minimap/" ) ||
		lowerPath.startsWith( "/assets/images/media_extracted/minimap_d/" )
	) {
		return "mission-minimap";
	}
	if ( lowerPath.startsWith( "/assets/world/outdoor/" ) ) {
		return "outdoor-world";
	}
	if (
		lowerPath.startsWith( "/assets/char/vat/" ) && (lowerPath.endsWith( ".bin" ) || lowerPath.endsWith( ".json" ))
	) {
		return "title-crowd-vat";
	}
	if (
		lowerPath.startsWith( "/assets/npc/vat/" ) && (lowerPath.endsWith( ".bin" ) || lowerPath.endsWith( ".json" ))
	) {
		return "mission-npc-vat";
	}
	if ( lowerPath === "/assets/npc/animation-catalog.json" || lowerPath === "/assets/npc/animation-catalog.json.gz" ) {
		return "developer-labs";
	}
	if ( lowerPath.endsWith( ".glb" ) ) {
		return "game-models";
	}
	if ( lowerPath.startsWith( "/assets/audio/" ) && (lowerPath.endsWith( ".mp3" ) || lowerPath.endsWith( ".wav" )) ) {
		return "game-audio";
	}
	if ( lowerPath.endsWith( ".json" ) || lowerPath.endsWith( ".json.gz" ) ) {
		return "game-data";
	}
	if ( lowerPath.startsWith( "/assets/anim/" ) && (lowerPath.endsWith( ".ban" ) || lowerPath.endsWith( ".bin" )) ) {
		return "game-data";
	}
	if ( isImageLikeAssetPath( lowerPath ) ) {
		// Image families have distinct owners with the same startup cache
		// protection; interface chrome remains in game-images.
		if ( lowerPath.startsWith( "/assets/world/" ) ) return "world-textures";
		if ( lowerPath.startsWith( "/assets/images/map_extracted/tile2d/" ) ) return "map-tiles";
		if ( lowerPath.startsWith( "/assets/images/media_extracted/icon/" ) ) return "ui-icons";
		if ( lowerPath.startsWith( "/assets/images/particles_extracted/textures/" ) ) {
			return "particle-textures";
		}
		return "game-images";
	}

	return undefined;
}

/*
================
isImageLikeAssetPath
================
*/
function isImageLikeAssetPath( publicPath ) {
	return /\.(?:png|jpe?g|dds|webp|cur|texture)$/i.test( publicPath );
}

/*
================
loadRetainedPackFiles

A file under /assets/packs/ is accounted for exactly when the pack garbage
rule keeps it: the index and its delivery sidecar, every pack, every
per-asset transport file, and the control files of a directory that still
holds a live pack. Using that one rule keeps this test and the collector
from disagreeing about what belongs.
================
*/
async function loadRetainedPackFiles() {
	const { garbage } = await collectPackGarbage( { publicRoot } );
	const retired = new Set(
		garbage.map( ( entry ) => normalizePublicPath( path.relative( publicRoot, entry.file ) ).toLowerCase() )
	);
	return ( lowerPath ) => lowerPath.startsWith( "/assets/packs/" ) && !retired.has( lowerPath );
}

/*
================
normalizePublicPath
================
*/
function normalizePublicPath( value ) {
	return `/${String( value ).replaceAll( "\\", "/" ).replace( /^\/+/, "" )}`.replace( /\/{2,}/g, "/" );
}
