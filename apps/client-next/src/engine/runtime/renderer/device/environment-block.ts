/*
===========================================================================

environment-block.ts - the environment uniform block's byte layout

The native block is 336 bytes; the port-only tail the experimental stages
share follows it, so the WGSL Environment struct (pipelines.ts) and the
device's writes stay in one table.

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
