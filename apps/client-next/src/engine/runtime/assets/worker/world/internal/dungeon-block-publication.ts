/*
===========================================================================

dungeon-block-publication.ts - the dungeon block publication contract

The world decoder accepts a region bundle only when its dungeon blocks
are the publisher's product: indices in authored order, ambient colours
in range, and every authored Light1 entry (DunBlockLightEntry, read by
CRTDunBlock_ReadFromArchive 0x00AD8090) finite and within budget. This
module owns that admission check; the decoder owns the dungeon context
that decides whether blocks are admitted at all.

===========================================================================
*/
import type { Bundle } from "./resource-contract";

/*
================
validDungeonBlocks

True when the published blocks are well formed: at most 4096 blocks,
each named by its own index, a valid optional ambient colour, and
optional light entries whose name, three colour triplets, range and
(constant, linear) attenuation pair are all finite.
================
*/
export function validDungeonBlocks( blocks: Bundle["dungeonBlocks"] ): boolean {
	if ( !Array.isArray( blocks ) || blocks.length > 4096 ) return false;
	return !blocks.some( ( block, index ) =>
		block.index !== index || !Array.isArray( block.visibleBlocks ) ||
		(block.ambientColor !== undefined &&
			(!Number.isInteger( block.ambientColor ) || block.ambientColor < 0 ||
				block.ambientColor > 0xffffffff)) ||
		(block.lights !== undefined &&
			(!Array.isArray( block.lights ) || block.lights.length > 1024 ||
				block.lights.some( light =>
					typeof light?.name !== "string" ||
					![ light.position, light.diffuse, light.ambient, light.specular ].every(
						v => Array.isArray( v ) && v.length === 3 && v.every( Number.isFinite )
					) ||
					!Number.isFinite( light.range ) ||
					!Array.isArray( light.attenuation ) || light.attenuation.length !== 2 ||
					!Number.isFinite( light.attenuation[0] ) || !Number.isFinite( light.attenuation[1] )
				)))
	);
}
