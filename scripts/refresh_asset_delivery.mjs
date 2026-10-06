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
	const rows = index.assets.filter( e => e.transport );
	console.log(
		JSON.stringify( {
			compressedMembers: rows.length,
			identityBytes: rows.reduce( ( n, e ) => n + e.length, 0 ),
			compressedBytes: rows.reduce( ( n, e ) => n + e.transport.length, 0 ),
			animationManifests: index.assets.filter( e => e.animationSources ).length
		} )
	);
} );
