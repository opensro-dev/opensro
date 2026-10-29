/*
===========================================================================

publishEntityBsrModifiers.mjs - refresh entity resource modifiers in place

Preserve published geometry and VAT while refreshing authored BSR metadata,
including secondary stall booth resources owned by the NPC publisher.

===========================================================================
*/

import { CROW_EGG_BSR } from "./crowEgg.mjs";
import { entityMaterialMetadata } from "./entityMaterialMetadata.mjs";
import fs from "node:fs";
import path from "node:path";
import { parseCharacterBsr } from "./formats.mjs";
import { compoundBodyBsr } from "./itemAttachments.mjs";
import { dataAssetPath } from "../shared/jmxAssetIO.mjs";
import { publicRoot } from "../world/paths.mjs";
import { readPublishedAssetBytesSync } from "../../lib/publishedAsset.mjs";
import { writeJsonIfChangedSync } from "../shared/jsonOut.mjs";
import { writeIntoPublicTreeSync } from "../shared/publicWrite.mjs";
import { PORT_PET_DROP_MODEL } from "../shared/portItemRows.mjs";

// A compound resource (.cpd, the Qin-Shi tomb flamemaster, #2024).
const COMPOUND_RESOURCE = /\.cpd$/i;

/*
================
entitySource

The BSR whose modifiers a manifest resource carries: the resource itself,
or a compound's body branch, which is what the npc builder bakes into a
compound row (assembleCompoundCharacter).
================
*/
export async function entitySource( resourcePath ) {
	if ( !COMPOUND_RESOURCE.test( resourcePath ) ) {
		return { path: resourcePath, buffer: fs.readFileSync( dataAssetPath( resourcePath ) ) };
	}
	const body = await compoundBodyBsr( resourcePath );
	return { path: body.bsrPath, buffer: body.buffer };
}

// Metadata-only rebuild: retain byte-identical meshes and the VAT owner's
// enrichment. Full model builders publish the same original BSR payload.
/*
================
publishEntityBsrModifiers
================
*/
export async function publishEntityBsrModifiers() {
	const cache = new Map(), paths = [], updated = new Map();
	for ( const domain of [ "itemdrop", "npc" ] ) {
		const url = `/assets/${domain}/manifest.json`,
			manifest = JSON.parse( readPublishedAssetBytesSync( url, publicRoot ) );
		// The NPC manifest keeps each BSR's bake once (npcManifest.mjs, v9): the
		// modifiers and the rewritten GLB size belong to that resource, not to
		// each reference that aliases it.
		const rows = domain === "npc" ?
			Object.entries( { ...manifest.resources, ...manifest.boothModels } ).map( (
				[bsr, resource]
			) => [ bsr, resource, bsr ] ) :
			Object.entries( manifest.models ).map( ( [key, row] ) => [ key, row, row.bsr ?? `res/${key}` ] );
		for ( const [, row, bsr] of rows ) {
			// Owned GLB, not a retail BSR: its builder owns the borrowed chest glow.
			if (
				domain === "itemdrop" && bsr === `res/${PORT_PET_DROP_MODEL}` ||
				domain === "npc" && bsr === CROW_EGG_BSR
			) continue;
			if ( !cache.has( bsr ) ) {
				const source = await entitySource( bsr );
				cache.set( bsr, { ...source, modifiers: parseCharacterBsr( source.buffer, source.path ) } );
			}
			const { buffer: sourceBuffer, path: sourcePath, modifiers } = cache.get( bsr );
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
							bytes = await entityMaterialMetadata( original, sourceBuffer, sourcePath, slot );
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
