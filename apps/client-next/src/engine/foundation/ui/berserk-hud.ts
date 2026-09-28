/*
===========================================================================

berserk-hud.ts - elapsed-time projection of the Berserk HUD animation.

Owns visual fades and atlas frames. Gauge awards and gameplay expiry remain
server-owned; every projected opacity stays within the renderer's [0, 1] range.
The existing retail timing references are 6B41F0/6B6620 for the HUD and
777B60 -> 8CEF20 for the shared entry flash.

===========================================================================
*/

const STAGE_COUNT = 5;
const STAGE_MS = 12000;
const DURATION_MS = 60000;
const ATLAS_FRAME_MS = 50;
const ATLAS_FRAMES = 12;
const ORB_FADE_MS = 1000;
const GLOW_FADE_MS = 3000;
const FLASH_ALPHA_BYTE = 128;
const MAX_ALPHA_BYTE = 255;
const FLASH_IN_MS = 200;
const FLASH_OUT_MS = 500;

/*
================
berserkHud

The fade-out clock starts at expiry. Before expiry its factor must stay at
one; extrapolating it backwards amplifies the fade-in above valid opacity.
================
*/
export function berserkHud( elapsedMs: number ) {
	const elapsed = Math.max( 0, elapsedMs );
	const finished = elapsed >= DURATION_MS;
	const glowIn = Math.min( 1, elapsed / GLOW_FADE_MS );
	const glowOut = Math.max( 0, 1 - Math.max( 0, elapsed - DURATION_MS ) / GLOW_FADE_MS );
	const circles: number[] = [];
	const fire: number[] = [];

	for ( let stage = 0; stage < STAGE_COUNT; stage++ ) {
		const stageEnd = (stage + 1) * STAGE_MS;
		const opacity = finished ? 0 : Math.max( 0, Math.min( 1, 1 - (elapsed - stageEnd) / ORB_FADE_MS ) );
		circles.push( opacity );
		fire.push( Math.min( 1, elapsed / ORB_FADE_MS ) * opacity );
	}

	return {
		frame: Math.floor( Math.min( elapsed, DURATION_MS - 1 ) / ATLAS_FRAME_MS ) % ATLAS_FRAMES,
		glow: glowIn * glowOut,
		circles,
		fire
	};
}

/*
================
berserkEntryFlash

Project the shared white flash independently of the longer HUD glow. A frame
before activation or after the fade-out contributes no overlay opacity.
================
*/
export function berserkEntryFlash( elapsedMs: number ) {
	if ( elapsedMs < 0 ) {
		return 0;
	}

	const peakOpacity = FLASH_ALPHA_BYTE / MAX_ALPHA_BYTE;
	if ( elapsedMs < FLASH_IN_MS ) {
		return peakOpacity * elapsedMs / FLASH_IN_MS;
	}

	const fade = Math.max( 0, 1 - (elapsedMs - FLASH_IN_MS) / FLASH_OUT_MS );
	return peakOpacity * fade;
}
