/*
===========================================================================

refresh_guide_asset_packs.mjs - publish the native guide data and images

The guide reads both catalogs and the localized menu dictionary. Publishing
only the catalogs leaves Help labels and item descriptions on an old
revision, so all four data files ride with the inline images.

===========================================================================
*/
import path from "node:path";
import { buildQuestDataAsset } from "./build/data/buildQuestDataAsset.mjs";
import { refreshPrecompressedSidecars } from "./build/generatedManifestSidecars.mjs";
import { buildGuideImageResources } from "./build/shared/cifResources.mjs";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { buildTextResources } from "./build/shared/textResources.mjs";
import { publicRoot } from "./build/world/paths.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

const GUIDE_DATA = [
	"/assets/data/event-guide-catalog.json",
	"/assets/data/questData.json",
	"/assets/text/texthelp.en.json",
	"/assets/text/textdataname.en.json"
];

await withGeneratedAssetsLock( "Native guide data and inline-image publication", async () => {
	await buildTextResources();
	buildQuestDataAsset();
	const images = await buildGuideImageResources();
	await refreshPrecompressedSidecars( GUIDE_DATA.map( file => path.join( publicRoot, file ) ), {
		onlyWhenStale: true
	} );
	const data = GUIDE_DATA.map( file => file + ".gz" );
	const updates = await publishLooseFamily( {
		name: "guide",
		files: [ ...data, ...images.copiedImages ],
		defaultGroup: file => data.includes( file ) ? "game-data" : "native-ui"
	} );
	const built = updates.reduce( ( count, update ) => count + update.builtPackCount, 0 );
	console.log(
		`Guide publication complete: ${images.references.length} inline image references, ${built} packs rebuilt.`
	);
} );
