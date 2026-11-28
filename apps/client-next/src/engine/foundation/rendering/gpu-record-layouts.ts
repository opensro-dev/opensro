/*
===========================================================================

gpu-record-layouts.ts - word layouts the CPU writes and a GPU pass reads

Port-only (#4692). A record the world or weather owner fills on the CPU
and a device compute pass reads must agree on its words; the layout lives
here so neither side imports the other's module.

===========================================================================
*/

// A terrain cull range (device/index-cull.ts): centre xyz and radius
// (float), source start, index count, the draw's slot and a pad (uint).
export const RANGE_WORDS = 8;

// The weather pool's regions (device/weather-particles.ts): rain (CWRain),
// snow (CWSnow) and rare snow (CWSnow2), and the drops each one holds.
// weather.ts never lets a spawn take a slot whose drop may be alive; these
// hold several times the steady state of the strongest native emission.
export const WEATHER_REGIONS = 3;
export const WEATHER_CAPACITY = [ 8192, 16384, 8192 ] as const;

// One weather drop: its words, the alive flag in word 0, its birth word.
export const PARTICLE_WORDS = 8;
export const PARTICLE_ALIVE = 1;
export const PARTICLE_BIRTH_WORD = 7;
