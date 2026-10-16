/*
===========================================================================

petItemIcons.mjs - publish owned degree-specific pet gear inventory icons

Port-only: three editable nine-cell source sheets become27 native-size PNGs.
Full CIF builds and the focused pet-loot family share this publication step.

===========================================================================
*/
import path from "node:path";
import { fileURLToPath } from "node:url";
import { runPython } from "./pythonRun.mjs";
import { writeIntoPublicTree } from "./publicWrite.mjs";
import { publicRoot } from "../world/paths.mjs";
import { PORT_ITEMS } from "./portItemRows.mjs";

/*
================
buildPetItemIcons
================
*/
export async function buildPetItemIcons() {
	const script = fileURLToPath( new URL( "./pet_item_icons.py", import.meta.url ) );
	const { stdout } = await runPython( [ script ], { task: "Pet gear icon export" } );
	const icons = JSON.parse( stdout );
	if ( icons.length !== PORT_ITEMS.length ) throw Error( "Incomplete pet icon export" );
	const files = [];
	for ( const item of PORT_ITEMS ) {
		const icon = icons.find( row => row.slot === item.slot.toLowerCase() && row.degree === item.degree );
		if ( !icon ) throw Error( `Missing pet icon ${item.codename}` );
		const publicPath = "/assets/images/Media_extracted/icon/" +
			item.icon.replaceAll( "\\", "/" ).replace( /\.ddj$/, ".png" );
		await writeIntoPublicTree( path.join( publicRoot, publicPath.slice( 1 ) ), Buffer.from( icon.png, "base64" ) );
		files.push( publicPath );
	}
	return files;
}
