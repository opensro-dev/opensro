/*
===========================================================================

refresh_quick_status_asset_packs.mjs - publish quick status HP/MP and alarm

The quick HP/MP gauges and the low-health alarm sound are loaded by code;
the images join native-ui and the sound joins game-audio unless a group
already owns them.

===========================================================================
*/
import { buildAlarmSoundResource } from "./build/shared/audioResources.mjs";
import { publishConvertedImage } from "./build/shared/convertedImages.mjs";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

await withGeneratedAssetsLock( "Quick status and alarm asset publication", async () => {
	const files = [];
	for ( const kind of [ "hp", "mp" ] ) {
		files.push(
			await publishConvertedImage( `/assets/images/Media_extracted/interface/ifcommon/quick_${kind}.png` )
		);
	}
	const sound = await buildAlarmSoundResource();
	files.push( sound );
	await publishLooseFamily( {
		name: "quick-status",
		files,
		defaultGroup: file => file === sound ? "game-audio" : "native-ui"
	} );
	console.log( "Published quick status HP/MP images and native alarm sound." );
} );
