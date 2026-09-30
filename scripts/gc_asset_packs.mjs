/*
===========================================================================

gc_asset_packs.mjs - `pnpm assets gc`: retire unused asset-pack outputs

	node scripts/gc_asset_packs.mjs            report what would be retired
	node scripts/gc_asset_packs.mjs --apply    soft-archive it

Retired files move to temp/archives/generated-artifacts/ with their original
path and a provenance record (see assetPackGarbage.mjs); nothing is deleted.
The web manifest is rebuilt after anything is archived.
Emptying temp/ is always safe. Runs under the generated-assets lock, so it
never races a publisher.

===========================================================================
*/

import { collectPackGarbage } from "./build/assetPackGarbage.mjs";
import { buildWebAssetManifest } from "./build/webManifest.mjs";
import { publicRoot } from "./build/world/paths.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

const apply = process.argv.includes( "--apply" );

await withGeneratedAssetsLock( "retire unused asset packs", async () => {
	const { live, garbage, garbageBytes } = await collectPackGarbage( { publicRoot, apply } );
	const gigabytes = (garbageBytes / 2 ** 30).toFixed( 2 );
	const verb = apply ? "archived" : "would archive";
	console.log( `asset packs: ${live} live file(s); ${verb} ${garbage.length} unused file(s), ${gigabytes} GB` );
	if ( !apply && garbage.length > 0 ) {
		console.log( "run with --apply to move them to temp/archives/generated-artifacts/" );
	}
	// The web manifest lists every published file; rebuild it so it no longer
	// names what was just archived (publishers collect inside publication,
	// before their manifest rebuild, so they need no second pass).
	if ( apply && garbage.length > 0 ) await buildWebAssetManifest();
} );
