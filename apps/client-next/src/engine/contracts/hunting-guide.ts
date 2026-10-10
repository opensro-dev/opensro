/*
===========================================================================

hunting-guide.ts - the public atlas identity admitted with a world session

Port-only, not native. The HUD loads this immutable resource only on demand.
The identity contains no live creature, account or character information.

===========================================================================
*/

/*
================
HuntingGuideSource
================
*/
export interface HuntingGuideSource {
	readonly url: string;
	readonly bytes: number;
}

// Two MiB; shared contracts expose literal scalar constants.
export const HUNTING_GUIDE_BYTES_LIMIT = 2097152;
