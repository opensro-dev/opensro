/*
===========================================================================

environment-block.ts - the environment uniform block's byte layout

The native block is 336 bytes; the port-only tail the experimental stages
share follows it, so the WGSL Environment struct (pipelines.ts) and the
device's writes stay in one table. Which Experimental row sits in which
stage word is part of that table, so the packing lives here too.

===========================================================================
*/

// Native 336 plus the sun-direction vec4 (Moving sunlight).
export const ENVIRONMENT_BLOCK_BYTES = 352;
// The first experimental stages vec4: height fog, sun direction, terrain
// relief, textured horizon.
export const ENVIRONMENT_STAGES_OFFSET = 352;
// The second stages vec4: HDR tone map, sun shadow, per-pixel lighting.
export const ENVIRONMENT_STAGES2_OFFSET = 368;
// The sun shadow cascade matrix (sun-shadow-math.ts).
export const ENVIRONMENT_SHADOW_MATRIX_OFFSET = 384;
export const ENVIRONMENT_UNIFORM_BYTES = 448;

// The stage words the frame overrides (worldView): the sun direction,
// which the viewer's studio light forces on, and the sun shadow, which a
// preview scene without a cascade forces off.
const STAGE_SUN_DIRECTION = 1;
const STAGE_SUN_SHADOW = 5;

/*
================
StageRows

The Experimental rows that own a stage word, as experimentalVideo hands
them over. All off is the native frame: both stages vec4s zero.
================
*/
export interface StageRows {
	readonly heightFog: boolean;
	readonly dynamicSun: boolean;
	readonly waterRefraction: boolean;
	readonly skyDetail: boolean;
	readonly hdrToneMap: boolean;
	readonly sunShadow: boolean;
	readonly perPixelLighting: boolean;
	readonly ambientOcclusion: boolean;
}

/*
================
packStages

The saved stage words, in the order the WGSL struct reads them (stages,
then stages2). stages.z was terrain relief, removed at the owner's
request (2026-10-11), and now gates the port-only water refraction;
stages.w was the textured horizon, and now gates the port-only sky
detail (clouds, glow, rim).
================
*/
export function packStages( saved: Float32Array, rows: StageRows ) {
	saved.set( [
		rows.heightFog ? 1 : 0,
		rows.dynamicSun ? 1 : 0,
		rows.waterRefraction ? 1 : 0,
		rows.skyDetail ? 1 : 0,
		rows.hdrToneMap ? 1 : 0,
		rows.sunShadow ? 1 : 0,
		rows.perPixelLighting ? 1 : 0,
		rows.ambientOcclusion ? 1 : 0
	] );
}

/*
================
frameStages

One frame's stage words: the saved ones, with the sun shadow off for a
scene without a cascade (previews; the saved choice is kept for world
re-entry) and the sun direction on for the viewer's studio light.
================
*/
export function frameStages( saved: Float32Array, out: Float32Array, shadowEnabled: boolean, directional: boolean ) {
	out.set( saved );
	if ( !shadowEnabled ) out[STAGE_SUN_SHADOW] = 0;
	if ( directional ) out[STAGE_SUN_DIRECTION] = 1;
	return out;
}
