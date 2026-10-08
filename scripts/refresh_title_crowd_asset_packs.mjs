/*
===========================================================================

refresh_title_crowd_asset_packs.mjs - republish the title-crowd VAT packs

Repacks the title-crowd VAT artifacts sparsely and reconciles game-data from
its loose authority (the roster), then archives the packs they superseded.

===========================================================================
*/
// First: it sizes libuv's thread pool before anything starts it.
import "./build/shared/buildParallelism.mjs";
import { reconcileAssetPackGroupFromLooseAuthority } from "./build/assetPackGroupAuthority.mjs";
import { listPublicAssetFiles } from "./build/assetPacks.mjs";
import { refreshPackGroups } from "./build/shared/packGroupRefresh.mjs";
import { publicRoot } from "./build/world/paths.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

await withGeneratedAssetsLock( "title-crowd asset-pack refresh", async () => {
	const vatFiles = await listPublicAssetFiles( {
		publicRoot,
		roots: [ "/assets/char/vat" ],
		extensions: [ ".bin", ".json" ]
	} );
	if ( vatFiles.length === 0 ) throw new Error( "No published title-crowd VAT artifacts were found." );
	const { updates, totals, webManifest } = await refreshPackGroups( {
		name: "title-crowd",
		deltas: [
			{ groupName: "title-crowd-vat", files: vatFiles, sameMembership: true },
			{
				groupName: "game-data",
				files: [ "/assets/char/roster.json.gz" ],
				sameMembership: true,
				// The roster is reconciled from every loose authority of the group.
				rebuild: ( previousIndex, outputRoot ) =>
					reconcileAssetPackGroupFromLooseAuthority( {
						publicRoot,
						outputRoot,
						previousIndex,
						groupName: "game-data"
					} )
			}
		]
	} );
	console.log(
		`Title-crowd closure OK: ${totals.built} pack(s) rebuilt, ${totals.reused} reused, ` +
			`${totals.changed} asset delta(s), ${totals.hydrated} member(s) hydrated; ` +
			`${updates[1].authorityFileCount} game-data authorities checked, ${webManifest.files.length} web assets.`
	);
} );
