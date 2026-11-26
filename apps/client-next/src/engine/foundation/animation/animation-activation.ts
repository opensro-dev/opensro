/** Identity belongs to a motion installation, never a clip name or sampled age. */
export interface AnimationActivation {
	readonly animationActivation: unique symbol;
	readonly started: number;
}
export function animationActivation( started: number ): AnimationActivation {
	if ( !Number.isFinite( started ) ) throw Error( "Invalid animation activation time" );
	// The brand is type-only. Each installation owns a fresh frozen object;
	// shared animation helpers must not allocate a module-level identity token.
	return Object.freeze( { started } ) as AnimationActivation;
}
