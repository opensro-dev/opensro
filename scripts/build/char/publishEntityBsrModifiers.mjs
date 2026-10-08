import { entityMaterialMetadata } from "./entityMaterialMetadata.mjs";
import fs from "node:fs";
import path from "node:path";
import { parseCharacterBsr } from "./formats.mjs";
import { dataAssetPath } from "../shared/jmxAssetIO.mjs";
import { publicRoot } from "../world/paths.mjs";
import { readPublishedAssetBytesSync } from "../../lib/publishedAsset.mjs";
import { writeJsonIfChangedSync } from "../shared/jsonOut.mjs";
import { writeIntoPublicTreeSync } from "../shared/publicWrite.mjs";

// Metadata-only rebuild: retain byte-identical meshes and the VAT owner's
// enrichment. Full model builders publish the same original BSR payload.
export async function publishEntityBsrModifiers() {
	const cache = new Map(), paths = [], updated = new Map();
	for ( const domain of [ "itemdrop", "npc" ] ) {
		const url = `/assets/${domain}/manifest.json`,
			manifest = JSON.parse( readPublishedAssetBytesSync( url, publicRoot ) );
		for ( const [key, row] of Object.entries( manifest.models ) ) {
			const bsr = row.bsr ?? `res/${key}`;
			if ( !cache.has( bsr ) ) {
				cache.set( bsr, parseCharacterBsr( fs.readFileSync( dataAssetPath( bsr ) ), bsr ) );
			}
			const modifiers = cache.get( bsr );
			row.modifierSets = modifiers.modifierSets;
			row.particleModifiers = modifiers.particleModifiers;
			row.materialModifiers = modifiers.materialModifiers;
			row.textureModifiers = modifiers.textureModifiers;
			if ( modifiers.materialModifiers.length || modifiers.textureModifiers.length ) {
				for (
					const [slot, url] of [
						[ domain === "npc" ? 0 : undefined, row.glb ],
						...Object.entries( row.materialVariants ?? {} ).map( (
							[slot, url]
						) => [ Number( slot ), url ] )
					]
				) {
					if ( !updated.has( url ) ) {
						const original = readPublishedAssetBytesSync( url, publicRoot ),
							bytes = await entityMaterialMetadata(
								original,
								fs.readFileSync( dataAssetPath( bsr ) ),
								bsr,
								slot
							);
						writeIntoPublicTreeSync( path.join( publicRoot, url ), bytes );
						updated.set( url, bytes.length );
						paths.push( url );
					}
					if ( url === row.glb ) row.bytes = updated.get( url );
				}
			}
		}
		writeJsonIfChangedSync( path.join( publicRoot, url ), manifest );
		paths.push( url );
	}
	return paths;
}
