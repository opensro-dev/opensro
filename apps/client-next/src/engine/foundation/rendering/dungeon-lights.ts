/*
===========================================================================

dungeon-lights.ts - an actor's Light1 point light from its dungeon block

In a dungeon the native render pass (SWorld_RenderDungeonPass 0x8B17E0,
entity loop at 0x8B1CF4..0x8B1D77) gives every visible entity the point
light of its block nearest the entity: CRTDunBlock_FindNearestPointLight
(0x00AD3C30) over the block's DunBlockLightEntry vector, applied through
RenderLight_ApplyLight1 (0x00A5A7F0). This module is that selection for
the port's character point-light channel: the scene publishes each
block's authored entries (contracts/scene.ts DungeonBlockLight), and the
renderer binds the nearest one to each actor standing in the block.

===========================================================================
*/
import type { CharacterPointLight } from "@/engine/contracts/character";
import type { DungeonBlockLight } from "@/engine/contracts/scene";

// RenderLight_ApplyLight1 uploads the descriptor's Ambient triplet
// divided by 1.67 into shader constant 0xD (0x00A5A7F0: each Ambient
// component / 1.6699999570846558). The diffuse triplet and the
// attenuation terms pass through unscaled.
const DUNGEON_LIGHT_AMBIENT_DIVISOR = 1.67;

/*
================
dungeonPointLight

The point light of the authored entry nearest (x, y, z) in the actor's
block, or undefined when the block authors none - the native pass then
falls back to the dungeon's default descriptor, which carries no light
an actor could borrow. Squared distance, first entry wins ties, as
0x00AD3C30 measures. The actor keeps its own effect light when it has
one; callers apply this only in its absence.
================
*/
export function dungeonPointLight(
	blocks: readonly (readonly DungeonBlockLight[])[] | undefined,
	block: number | undefined,
	regionId: number,
	x: number,
	y: number,
	z: number
): CharacterPointLight | undefined {
	if ( !blocks || block === undefined ) return undefined;
	const entries = blocks[block];
	if ( !entries?.length ) return undefined;
	let best: DungeonBlockLight | undefined, bestDelta = Infinity;
	for ( const entry of entries ) {
		const dx = entry.position[0]! - x,
			dy = entry.position[1]! - y,
			dz = entry.position[2]! - z,
			delta = dx * dx + dy * dy + dz * dz;
		if ( delta < bestDelta ) {
			bestDelta = delta;
			best = entry;
		}
	}
	if ( !best ) return undefined;
	return {
		pose: { regionId, x: best.position[0]!, y: best.position[1]!, z: best.position[2]! },
		ambient: [
			best.ambient[0]! / DUNGEON_LIGHT_AMBIENT_DIVISOR,
			best.ambient[1]! / DUNGEON_LIGHT_AMBIENT_DIVISOR,
			best.ambient[2]! / DUNGEON_LIGHT_AMBIENT_DIVISOR
		],
		diffuse: [ best.diffuse[0]!, best.diffuse[1]!, best.diffuse[2]! ],
		attenuation: best.attenuation[1]!,
		attenuationConstant: best.attenuation[0]!,
		range: best.range
	};
}
