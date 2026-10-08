/*
===========================================================================

refresh_world_map_asset_packs.mjs - republish the world-map image closure

Converts and publishes every image the world-map tables and layout reach,
repacks game-images sparsely and soft-archives the packs it superseded.

===========================================================================
*/
// First: it sizes libuv's thread pool before anything starts it.
import "./build/shared/buildParallelism.mjs";
import { buildWorldMapImageResources, imagePublicPath } from "./build/shared/cifResources.mjs";
import { runConvertImages } from "./build/shared/convertImagesRunner.mjs";
import { refreshPackGroups, timedStep } from "./build/shared/packGroupRefresh.mjs";
import { beginPublication, claimPublicPaths, commitPublication } from "./build/shared/publicationLedger.mjs";
import { collectWorldMapImageReferences } from "./build/shared/worldMapImageReferences.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

const timed = timedStep( "world-map-refresh" );

await withGeneratedAssetsLock( "world-map asset-pack refresh", async () => {
	const closure = await timed( "dependency closure", collectWorldMapImageReferences );
	const conversion = await timed( "targeted image conversion", () => runConvertImages( closure.references ) );
	if ( conversion.status !== 0 ) {
		throw new Error( `World-map image conversion failed with exit status ${conversion.status}.` );
	}
	// The world map's images are this publisher's own ledger record.
	beginPublication( "world-map" );
	const published = await timed( "strict publication", () => buildWorldMapImageResources() );
	const looseFiles = published.references.map( imagePublicPath );
	claimPublicPaths( looseFiles );
	const { totals, webManifest } = await refreshPackGroups( {
		name: "world-map",
		timed,
		deltas: [ { groupName: "game-images", startup: true, files: looseFiles } ]
	} );
	await commitPublication();
	console.log(
		`World-map closure OK: ${closure.tileReferences.length} tiles, ` +
			`${closure.mapPageReferences.length} pages, ${closure.overlayReferences.length} overlays; ` +
			`${totals.built} pack(s) rebuilt, ${totals.reused} reused, ` +
			`${totals.changed} asset delta(s), ${webManifest.files.length} web assets.`
	);
} );
