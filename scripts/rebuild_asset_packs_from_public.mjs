/*
===========================================================================

rebuild_asset_packs_from_public.mjs - `pnpm assets repack`

Rebuilds every asset pack from the published loose tree, through the same
ordered tail as the full build (packPublicTree.mjs). A repack may run on a
compacted tree, so it never retires sidecars whose loose base is gone.

===========================================================================
*/
import { packPublicTree } from "./build/packPublicTree.mjs";
import { buildUiImagePreloadManifest } from "./build/uiImagePreload.mjs";
import { copyMissionMinimapTileImages } from "./build/world/assets/copyMissionMinimapTileImages.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

await withGeneratedAssetsLock( "browser asset pack rebuild", async () => {
	const timings = {};
	const timed = async ( label, task ) => {
		const startedAt = performance.now();
		const result = await task();
		timings[label] = `${((performance.now() - startedAt) / 1000).toFixed( 1 )}s`;
		return result;
	};

	const uiImagePreload = await timed( "uiImagePreload", () => buildUiImagePreloadManifest() );
	const missionMinimapTiles = await timed( "minimapTiles", () => copyMissionMinimapTileImages() );
	const { assetPacks, manifest } = await packPublicTree( {
		timed,
		retireSidecars: false,
		groupInputs: {
			uiImagePreloadPaths: uiImagePreload.images.map( ( image ) => image.path ),
			missionMinimapTilePaths: missionMinimapTiles.map( ( tile ) => tile.publicPath )
		}
	} );

	const summary = Object.fromEntries(
		assetPacks.groups.map( ( group ) => [
			group.name,
			{ assets: group.assetCount, packs: group.packs.map( ( pack ) => pack.path ) }
		] )
	);
	console.log( JSON.stringify(
		{
			packCount: assetPacks.packCount,
			builtPacks: assetPacks.builtPackCount,
			reusedPacks: assetPacks.reusedPackCount,
			assetCount: assetPacks.assetCount,
			webManifestFileCount: manifest.files.length,
			webManifestHash: manifest.manifestHash.slice( 0, 12 ),
			timings,
			summary
		},
		null,
		2
	) );
} );
