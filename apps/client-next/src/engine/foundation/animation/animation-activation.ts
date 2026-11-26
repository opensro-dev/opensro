/*
===========================================================================

animation-activation.ts - the animation activation identity

An activation names one motion installation: the brand is a unique
symbol, so a clip name or a sampled age can never stand in for it.
Each installation owns a fresh frozen token (the factory below);
shared animation helpers must not allocate a module-level identity.

===========================================================================
*/
export interface AnimationActivation {
	readonly animationActivation: unique symbol;
	readonly started: number;
}
/*
================
animationActivation

Mint one installation's identity at its start time (presentation
milliseconds). A non-finite start is refused: an activation that
cannot be ordered against the clock is a programming error, not data.
================
*/
export function animationActivation( started: number ): AnimationActivation {
	if ( !Number.isFinite( started ) ) throw Error( "Invalid animation activation time" );
	// The brand is type-only. Each installation owns a fresh frozen object;
	// shared animation helpers must not allocate a module-level identity token.
	return Object.freeze( { started } ) as AnimationActivation;
}
