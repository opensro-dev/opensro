/*
===========================================================================

refresh_native_font_asset_packs.mjs - republish the native UI font atlas

Rebuilds the font atlas and repacks its JSON and PNG in the startup-resident
game-data and game-images groups, then archives the packs they superseded.

===========================================================================
*/
import { mergeAssetPackGroupUpdates, publishAssetPackManifest } from "./build/assetPackPublication.mjs";
import { mkdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { buildFontResources } from "./build/fonts.mjs";
import { refreshGeneratedManifestSidecars, refreshPrecompressedSidecars } from "./build/generatedManifestSidecars.mjs";
import { patchAssetPackGroupFromLooseFiles } from "./build/sparseAssetPackGroupRefresh.mjs";
import { buildWebAssetManifest } from "./build/webManifest.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";
import { archiveGeneratedArtifact } from "./build/artifacts/generatedArtifactArchive.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, ".." );
const publicRoot = path.join( rebuildRoot, ".generated", "client-public" );
const packsRoot = path.join( publicRoot, "assets", "packs" );
const packManifestPath = path.join( packsRoot, "manifest.json" );
const refreshRoot = path.join( packsRoot, "incremental", "native-font" );
const atlasJsonPath = path.join( publicRoot, "assets", "fonts", "native-ui-font-atlas.json" );

const DELTAS = [
	{
		groupName: "game-data",
		looseFiles: [ "/assets/fonts/native-ui-font-atlas.json.gz" ]
	},
	{
		groupName: "game-images",
		looseFiles: [ "/assets/fonts/native-ui-font-atlas.png" ]
	}
];

await withGeneratedAssetsLock( "native-font asset-pack refresh", async () => {
	await timed( "font publication", buildFontResources );
	await timed(
		"font JSON sidecars",
		() => refreshPrecompressedSidecars( [ atlasJsonPath ], { onlyWhenStale: true } )
	);

	const previous = JSON.parse( await readFile( packManifestPath, "utf8" ) );
	const refreshedByGroup = new Map();

	for ( const delta of DELTAS ) {
		requireStartupGroup( previous, delta.groupName );
		const refreshed = await timed(
			`sparse ${delta.groupName} pack refresh`,
			() =>
				patchAssetPackGroupFromLooseFiles( {
					publicRoot,
					outputRoot: path.join( refreshRoot, delta.groupName ),
					previousIndex: previous,
					groupName: delta.groupName,
					looseFiles: delta.looseFiles
				} )
		);
		refreshedByGroup.set( delta.groupName, refreshed );
	}

	const merged = mergeAssetPackGroupUpdates( previous, [ ...refreshedByGroup.values() ] );
	const changed = merged !== previous;

	validateDeltas( merged );
	await mkdir( packsRoot, { recursive: true } );
	if ( changed ) {
		await publishAssetPackManifest( publicRoot, packManifestPath, Buffer.from( JSON.stringify( merged ), "utf8" ), {
			logLabel: "native-font-pack-overlay"
		} );
	}

	for ( const [groupName, refreshed] of refreshedByGroup ) {
		await timed(
			`superseded ${groupName} pack cleanup`,
			() => removeSupersededPackFiles( requireStartupGroup( previous, groupName ), refreshed.groups[0] )
		);
	}

	const webManifest = await timed( "web manifest refresh", buildWebAssetManifest );
	await timed( "manifest sidecar refresh", () =>
		refreshGeneratedManifestSidecars( {
			publicRoot,
			onlyWhenStale: true,
			brotliQuality: 4,
			gzipLevel: 3,
			zstdLevel: 3
		} ) );

	const totals = [ ...refreshedByGroup.values() ].reduce(
		( sum, refreshed ) => ({
			built: sum.built + refreshed.builtPackCount,
			reused: sum.reused + refreshed.reusedPackCount,
			changed: sum.changed + refreshed.changedAssetCount,
			hydrated: sum.hydrated + refreshed.hydratedAssetCount
		}),
		{ built: 0, reused: 0, changed: 0, hydrated: 0 }
	);
	console.log(
		`Native-font closure OK: ${totals.built} pack(s) rebuilt, ${totals.reused} reused, ` +
			`${totals.changed} asset delta(s), ${totals.hydrated} packed member(s) hydrated; ` +
			`${webManifest.files.length} web assets.`
	);
} );

/*
================
requireStartupGroup
================
*/
function requireStartupGroup( index, groupName ) {
	const group = index.groups?.find( ( candidate ) => candidate.name === groupName );
	if ( !group || group.assetCount === 0 || group.packs?.length === 0 ) {
		throw new Error( `The main asset manifest has no populated ${groupName} group.` );
	}
	if ( group.load !== "startup" ) {
		throw new Error( `${groupName} must be startup-resident; found load=${JSON.stringify( group.load )}.` );
	}
	return group;
}

/*
================
validateDeltas
================
*/
function validateDeltas( index ) {
	const assets = new Map( index.assets.map( ( asset ) => [ asset.path.toLowerCase(), asset ] ) );
	const missing = [];
	for ( const delta of DELTAS ) {
		for ( const publicPath of delta.looseFiles ) {
			const asset = assets.get( publicPath.toLowerCase() );
			if ( !asset || asset.group !== delta.groupName ) missing.push( publicPath );
		}
	}
	if ( missing.length > 0 ) {
		throw new Error( `Native-font pack closure is incomplete: ${missing.join( ", " )}` );
	}
}

/*
================
removeSupersededPackFiles
================
*/
async function removeSupersededPackFiles( previousGroup, refreshedGroup ) {
	const currentPaths = new Set(
		refreshedGroup.packs.flatMap( ( pack ) =>
			[ pack.path, pack.zstdPath ].filter( Boolean ).map( normalizePublicPath )
		)
	);
	for ( const pack of previousGroup.packs ) {
		for ( const publicPath of [ pack.path, pack.zstdPath ].filter( Boolean ) ) {
			if ( currentPaths.has( normalizePublicPath( publicPath ) ) ) continue;
			await archiveGeneratedArtifact( resolvePackFile( publicPath ), {
				scopeRoot: publicRoot,
				reason: "superseded-native-font-pack"
			} );
		}
	}
}

/*
================
resolvePackFile
================
*/
function resolvePackFile( publicPath ) {
	const resolved = path.resolve( publicRoot, normalizePublicPath( publicPath ).replace( /^\/+/, "" ) );
	const relative = path.relative( packsRoot, resolved );
	if ( relative.startsWith( ".." ) || path.isAbsolute( relative ) ) {
		throw new Error( `Refusing to remove pack outside ${packsRoot}: ${resolved}` );
	}
	return resolved;
}

/*
================
normalizePublicPath
================
*/
function normalizePublicPath( value ) {
	return `/${String( value ).replaceAll( "\\", "/" ).replace( /^\/+/, "" )}`.replace( /\/{2,}/g, "/" );
}

/*
================
timed
================
*/
async function timed( label, task ) {
	const startedAt = performance.now();
	console.log( `[native-font-refresh] ${label}: start` );
	try {
		const result = await task();
		console.log(
			`[native-font-refresh] ${label}: done (${((performance.now() - startedAt) / 1000).toFixed( 1 )}s)`
		);
		return result;
	} catch ( error ) {
		console.error(
			`[native-font-refresh] ${label}: failed after ${((performance.now() - startedAt) / 1000).toFixed( 1 )}s`
		);
		throw error;
	}
}
