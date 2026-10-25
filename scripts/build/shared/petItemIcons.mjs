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
	const items = PORT_ITEMS.filter( item => item.seal === 0 );
	if ( icons.length !== items.length ) throw Error( "Incomplete pet icon export" );
	const files = [];
	for ( const item of items ) {
		const icon = icons.find( row => row.slot === item.slot.toLowerCase() && row.degree === item.degree );
		if ( !icon ) throw Error( `Missing pet icon ${item.codename}` );
		const publicPath = "/assets/images/Media_extracted/icon/" +
			item.icon.replaceAll( "\\", "/" ).replace( /\.ddj$/, ".png" );
		await writeIntoPublicTree( path.join( publicRoot, publicPath.slice( 1 ) ), Buffer.from( icon.png, "base64" ) );
		files.push( publicPath );
	}
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
