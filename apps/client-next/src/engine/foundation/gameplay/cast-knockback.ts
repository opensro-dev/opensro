/*
===========================================================================

cast-knockback.ts - which result impacts move their target

Shared by the result consumer (damage-feedback.ts), which holds a struck
character until its hit lands, and the hit presenter (presentation-events.ts),
which starts the recoil there.

===========================================================================
*/
import type { CastImpact } from "@/engine/contracts/gameplay";

// 8D56E8 switches the struck character's travel on the impact type.
export const KNOCKDOWN_IMPACT = 4;
export const KNOCKBACK_IMPACT = 5;

/*
================
isKnockback

A result impact that moves its target: a knockdown or a knockback that
carries a destination.
================
*/
export function isKnockback( hit: CastImpact ): boolean {
	return hit.displacement !== undefined && (hit.type === KNOCKDOWN_IMPACT || hit.type === KNOCKBACK_IMPACT);
}
