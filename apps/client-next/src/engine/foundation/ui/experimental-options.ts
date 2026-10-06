/*
===========================================================================

experimental-options.ts - explicitly opted-in browser additions

These settings are deliberately separate from the native SROptionSet.
Missing or invalid preferences never enable an experimental feature.

===========================================================================
*/

/*
================
ExperimentalOptions
================
*/
export interface ExperimentalOptions {
	readonly chatTimestamps: boolean;
}

/*
================
experimentalOptions
================
*/
export function experimentalOptions( value: unknown = null ): ExperimentalOptions {
	return {
		chatTimestamps: typeof value === "object" && value !== null && !Array.isArray( value ) &&
			"chatTimestamps" in value && value.chatTimestamps === true
	};
}
