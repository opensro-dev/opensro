// Shared JMX asset I/O for build pipelines (characters, mounts, world objects, …).
//
// Design rule: never infer .bmt / .ddj locations from folder naming conventions.
// Every JMXVRES 0109 (.bsr) file already lists its material-set paths; every
// JMXVBMT 0102 (.bmt) lists texture refs resolved relative to the set. Follow
// those pointers, then map converted PNGs under rebuild/assets/images/.

import path from "node:path";
import { existsSync } from "node:fs";
import { readFile } from "node:fs/promises";
import { extractedRoot, imageSourceRoot, normalizeAssetPath } from "../world/paths.mjs";
import { parseJmxBmtMaterialSet, resolveBmtTexturePath } from "../world/objects/formats.mjs";

/** Root of extracted game data (prim/, res/, …). */
export const dataRoot = path.join( extractedRoot, "Data_extracted" );

/** Absolute path for a game-relative asset (e.g. res/cos/t_horse1.bsr). */
export function dataAssetPath( gamePath ) {
	return path.join( dataRoot, ...normalizeAssetPath( gamePath ).split( "/" ) );
}

/** Read a game-relative asset from Data_extracted. */
export async function loadDataAsset( gamePath ) {
	return readFile( dataAssetPath( gamePath ) );
}

/** Map a game-relative .ddj (or other texture ref) to its converted PNG on disk. */
export function convertedTexturePath( textureGamePath ) {
	const source = normalizeAssetPath( textureGamePath );
	// The converter preserves the source extension for DDJ/TGA stem collisions.
	const qualified = path.join( imageSourceRoot, "Data_extracted", ...`${source}.png`.split( "/" ) );
	if ( existsSync( qualified ) ) return qualified;
	const normalized = source.replace( /\.ddj$/i, ".png" );
	return path.join( imageSourceRoot, "Data_extracted", ...normalized.split( "/" ) );
}

/**
 * Resolve materials for one or more .bmt paths referenced by a resource (.bsr).
 * Returns Map<materialName, { textureName, texturePath, pngPath, colors }>.
 * Later entries do not overwrite earlier ones (first wins).
 */
export async function loadMaterialTextures( materialPaths, options = {} ) {
	const onWarning = options.onWarning ?? (( msg ) => console.warn( msg ));
	const materials = new Map();

	for ( const materialSetPath of materialPaths ) {
		if ( !materialSetPath ) continue;
		try {
			const bmt = parseJmxBmtMaterialSet( await loadDataAsset( materialSetPath ), materialSetPath );
			for ( const mat of bmt.materials ) {
				if ( materials.has( mat.name ) ) continue;
				// Some sets (tree/bldg .bmt) carry texture-less materials; keep the
				// colour constants and emit no pngPath instead of aborting the set.
				const texturePath = resolveBmtTexturePath( materialSetPath, mat.textureName );
				materials.set( mat.name, {
					materialIndex: mat.materialIndex,
					materialSetPath,
					textureName: mat.textureName,
					texturePath,
					pngPath: texturePath ? convertedTexturePath( texturePath ) : null,
					// CPrimMtrl flags; bit 0x200 = texture alpha channel is transparency (hair
					// cutouts) - exporters map it to glTF alphaMode MASK. Bit 0x1 is unrelated;
					// many garment textures store specular data in alpha and must stay opaque.
					flags: mat.flags,
					// Native D3D ALPHAREF (BMT float, 0..255 scale; 0 = discard nothing).
					alphaRef: mat.alphaRef ?? 0,
					// Native CPrimMtrl colours. Character-create CRT materials feed ambient/diffuse
					// directly into sub_a91970 (c10/c11), so exporters must preserve them instead of
					// treating every textured glTF material as white.
					colors: mat.colors
				} );
			}
		} catch ( error ) {
			onWarning( `[jmx] could not load material set ${materialSetPath}: ${error?.message ?? error}` );
		}
	}

	return materials;
}
