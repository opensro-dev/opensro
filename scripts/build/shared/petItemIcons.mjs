/*
===========================================================================

petItemIcons.mjs - publish owned pet gear icons and the skill-family atlas

Port-only: original painted gear and skill atlases exported to native UI sizes.
Full CIF builds and the focused pet-loot family share this publication step.

===========================================================================
*/
import path from "node:path";
import { fileURLToPath } from "node:url";
import { runPython } from "./pythonRun.mjs";
import { copyIntoPublicTree, writeIntoPublicTree } from "./publicWrite.mjs";
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
	const paths = new Set( PORT_ITEMS.map( item => item.icon ) );
	const files = [];
	for ( const icon of icons ) {
		const degree = String( icon.degree ).padStart( 2, "0" );
		const nativePath = `item\\pet\\${icon.slot}_${degree}.ddj`;
		if ( !paths.delete( nativePath ) ) throw Error( `Unexpected pet icon ${nativePath}` );
		const publicPath = "/assets/images/Media_extracted/icon/" +
			nativePath.replaceAll( "\\", "/" ).replace( ".ddj", ".png" );
		await writeIntoPublicTree( path.join( publicRoot, publicPath.slice( 1 ) ), Buffer.from( icon.png, "base64" ) );
		files.push( publicPath );
	}
	if ( paths.size ) throw Error( "Incomplete pet icon export: " + [ ...paths ].join( ", " ) );
	for ( const name of [ "pet-skills", "pet-skills-1", "pet-skills-2" ] ) {
		const atlas = `/assets/images/Media_extracted/icon/port/${name}.png`;
		await copyIntoPublicTree(
			fileURLToPath( new URL( `../../../docs/pet-expansion/assets/${name}.png`, import.meta.url ) ),
			path.join( publicRoot, atlas.slice( 1 ) )
		);
		files.push( atlas );
	}
	return files;
}
