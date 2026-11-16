/*
===========================================================================

wing-flight.ts - the Experimental "Silent wing flight" row's sound rule

Port-only, not native (#3547). The wing dresses select the avatar_wing
animation set, which authors only state 7 (run: man_avatar_fly.ban, or
chinawoman_kangsi_runforward.ban for women) and no sound binding. The
native client looks the binding up by set (0xADEAB5) and, finding none,
again under "Default" (0xADEADE, the string at 0xBD0014), so a flying
character plays the default run's snd_run1 footsteps. The data holds no
player wing sound, so the row (on by default, the owner's choice
2026-11-15) only drops those cues; off is the native footsteps.

===========================================================================
*/
import type { AnimationMetadata } from "./animation-metadata";

// The avatar set the wing dresses select (BSR animation set name).
export const WING_FLIGHT_SET = "avatar_wing";
const WING_FLIGHT_CLIP_PREFIX = "native:avatar_wing:";

/*
================
wingFlightClip

Whether an installed clip is one of the wing set's motions (the
`native:<set>:<state>` role the actor pass installs).
================
*/
export function wingFlightClip( clip: string ): boolean {
	return clip.startsWith( WING_FLIGHT_CLIP_PREFIX );
}

/*
================
flightSoundDefinition

The definition the sound pass advances for a clip: unchanged unless the
row is on and the clip is a wing motion, then the same definition without
its sound cues. Cursor timing (durationMs and the rest) is kept.
================
*/
export function flightSoundDefinition(
	clip: string,
	definition: AnimationMetadata | undefined,
	silent: boolean
): AnimationMetadata | undefined {
	if ( !silent || !definition || definition.soundEvents.length === 0 || !wingFlightClip( clip ) ) {
		return definition;
	}
	// A small copy per flying actor per frame; the sound pass keeps no
	// reference to it, and only wing flights with cues pay for it.
	return { ...definition, soundEvents: [] };
}
