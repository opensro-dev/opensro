/*
===========================================================================

refresh_overlay_asset_packs.mjs - publish party status and fortress overlays

The party member status icons, the fortress markers and the party control
buttons are chosen by code, not by any resinfo layout, so the CIF build does
not reach them. Each folder contributes only the files its selector keeps.

===========================================================================
*/
import { readdir } from "node:fs/promises";
import { convertedImageFolder, publishConvertedImage } from "./build/shared/convertedImages.mjs";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

const OVERLAY_FOLDERS = [
	{ folder: "/assets/images/Media_extracted/icon/", keep: file => file === "buf_effect.png" },
	{ folder: "/assets/images/Media_extracted/icon/stateodd/", keep: () => true },
	{
		folder: "/assets/images/Media_extracted/icon/etc/",
		keep: file => file.startsWith( "mark_" ) || file === "fort_jangan.png"
	},
	{
		folder: "/assets/images/Media_extracted/interface/ifcommon/",
		keep: file => file.startsWith( "quickparty_move_" ) || file.startsWith( "com_kindred_" )
	}
];

await withGeneratedAssetsLock( "Party status and fortress overlay asset publication", async () => {
	const files = [];
	for ( const { folder, keep } of OVERLAY_FOLDERS ) {
		for ( const file of await readdir( convertedImageFolder( folder ) ) ) {
			// A <stem>.ddj.png collision is published under its plain name.
			if ( !file.endsWith( ".png" ) || file.endsWith( ".ddj.png" ) || !keep( file ) ) continue;
			files.push( await publishConvertedImage( folder + file ) );
		}
	}
	await publishLooseFamily( { name: "overlays", files, defaultGroup: "native-ui" } );
	console.log( "Published party status, fortress and party control assets." );
} );
