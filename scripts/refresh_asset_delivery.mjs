import { CLIENT_PUBLIC_ROOT } from "./lib/generatedRoot.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";
import { buildWebAssetManifest } from "./build/webManifest.mjs";
import { refreshGeneratedManifestSidecars } from "./build/generatedManifestSidecars.mjs";
import { readFile } from "node:fs/promises";
await withGeneratedAssetsLock( "Generated lossless asset delivery", async () => {
	console.log( "Deriving delivery products from the installed publication..." );
	await buildWebAssetManifest();
	await refreshGeneratedManifestSidecars( { onlyWhenStale: true } );
	const index = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/packs/manifest.json", "utf8" ) );
	const rows = index.assets.filter( e => e.stored );
	console.log(
		JSON.stringify( {
			storedMembers: rows.length,
			identityBytes: rows.reduce( ( n, e ) => n + e.length, 0 ),
			storedBytes: rows.reduce( ( n, e ) => n + e.stored.length, 0 ),
			animationManifests: index.assets.filter( e => e.animationSources ).length
		} )
	);
} );
