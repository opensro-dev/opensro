/*
===========================================================================

spawn-fade.ts - the alpha ramp a character enters the world with

CICharactor_OnSpawnInitialize (85FDB0) adds a CIDecoAppear when the
character's spawn-fade flag (+0x74C) is set: CIDecoAppear_Initialize
(8D4B60) starts alpha at 0 with a rate of 255 / 2.0 s (BCBEEC), and
CIDecoAppear_SetAlpha (8D4C20) adds rate * frame delta each frame, hands the
render object the truncated byte, and at 255 writes 255 and retires. The
constructor (8610A4) sets the flag, so players (local and peer), monsters and
COS fade in; CICNpc_InitializeFromRecord (86EE85) clears it, so NPCs appear
at once. A monster's linked ride gets its own 2 s CIDecoAppear (861EE2).

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";

// BCBEEC: the CIDecoAppear duration every spawn site passes.
export const SPAWN_FADE_SECONDS = 2;
// 8D4B60: 255 / SPAWN_FADE_SECONDS alpha units per second.
const ALPHA_RATE = 127.5;

/*
================
spawnFadeKind

Whether a freshly spawned entity of this kind enters with CIDecoAppear.
================
*/
export function spawnFadeKind( kind: EntityState["kind"] ): boolean {
	return kind === "player" || kind === "local-player" || kind === "monster" || kind === "cos";
}

/*
================
spawnFadeAlpha

The opacity elapsed seconds into the ramp: the truncated alpha byte over
255, and exactly 1 once the ramp has reached 255.
================
*/
export function spawnFadeAlpha( elapsed: number ): number {
	if ( !(elapsed > 0) ) return 0;
	const alpha = Math.fround( ALPHA_RATE * elapsed );
	return alpha >= 255 ? 1 : Math.trunc( alpha ) / 255;
}
