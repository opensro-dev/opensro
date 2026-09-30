/*
===========================================================================

refresh_native_audio_asset_packs.mjs - publish the native direct sounds

Rebuilds the direct-sound catalog and packs every sound it names.

===========================================================================
*/
import { buildNativeDirectSoundResources } from "./build/shared/audioResources.mjs";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

await withGeneratedAssetsLock( "Native direct audio publication", async () => {
	const catalog = await buildNativeDirectSoundResources();
	const files = [ ...new Set( catalog.rows.map( row => row.publicPath ) ) ];
	await publishLooseFamily( { name: "native-audio", files, defaultGroup: "game-audio" } );
	console.log( `Published ${files.length} native direct sounds through asset packs.` );
} );
