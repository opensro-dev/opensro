/*
===========================================================================

environment-stages.ts - the character environment map's native stage setups

The asset build publishes one of these per material (sroEnvironment.mode,
scripts/build/shared/characterEnvironment.mjs); the geometry shader draws
it as Material.reflection.x. Both values come from CRTModEnvMap::Apply
(AEE6D0), split on its modifier's base word 5 bit 0.

===========================================================================
*/

/** Bit 0 set: TFACTOR sphere, MODULATEALPHA_ADDCOLOR, MODULATE2X by the lit vertex colour. */
export const ENVIRONMENT_METAL = 1;
/** Bit 0 clear, table entry AEE430 with D3DTOP_ADD: base + sphere, no lighting stage. */
export const ENVIRONMENT_ADD = 2;

export type EnvironmentStages = typeof ENVIRONMENT_METAL | typeof ENVIRONMENT_ADD;
