/*
===========================================================================

experimental-options.ts - explicitly opted-in browser additions

These settings are deliberately separate from the native SROptionSet.
Missing or invalid preferences never enable an experimental feature, so
the defaults are the native client: no presentation pass, no anisotropy,
linear fog. Water reflection and equipment shine are native Video
options (Water reflection, Metal detail), not experimental ones.

===========================================================================
*/

/*
================
ExperimentalOptions
================
*/
export interface ExperimentalOptions {
	readonly chatTimestamps: boolean;
	readonly developerDiagnostics: boolean;
	// Video: renderer stages that deviate from the 2005 D3D9 look.
	readonly postProcessing: boolean;
	readonly anisotropicFiltering: boolean;
	readonly heightFog: boolean;
	readonly dynamicSun: boolean;
	readonly terrainRelief: boolean;
	readonly texturedHorizon: boolean;
	readonly floatBloom: boolean;
}

/*
================
ExperimentalKey

Every preference is one boolean, so the window toggles them by name.
================
*/
export type ExperimentalKey = keyof ExperimentalOptions;

/*
================
experimentalOptions

Only an explicit true enables a preference; anything else is off.
================
*/
export function experimentalOptions( value: unknown = null ): ExperimentalOptions {
	const record = typeof value === "object" && value !== null && !Array.isArray( value ) ?
		value as Record<string, unknown> :
		{};
	const enabled = ( key: ExperimentalKey ) => record[key] === true;
	return {
		chatTimestamps: enabled( "chatTimestamps" ),
		developerDiagnostics: enabled( "developerDiagnostics" ),
		postProcessing: enabled( "postProcessing" ),
		anisotropicFiltering: enabled( "anisotropicFiltering" ),
		heightFog: enabled( "heightFog" ),
		dynamicSun: enabled( "dynamicSun" ),
		terrainRelief: enabled( "terrainRelief" ),
		texturedHorizon: enabled( "texturedHorizon" ),
		floatBloom: enabled( "floatBloom" )
	};
}

/*
================
ExperimentalVideo

The renderer's slice of the preferences: the stages it switches at run
time. Every flag off is the native frame.
================
*/
export interface ExperimentalVideo {
	readonly postProcessing: boolean;
	readonly anisotropicFiltering: boolean;
	readonly heightFog: boolean;
	readonly dynamicSun: boolean;
	readonly terrainRelief: boolean;
	readonly texturedHorizon: boolean;
	readonly floatBloom: boolean;
}

/*
================
experimentalVideo
================
*/
export function experimentalVideo( options: ExperimentalOptions ): ExperimentalVideo {
	return {
		postProcessing: options.postProcessing,
		anisotropicFiltering: options.anisotropicFiltering,
		heightFog: options.heightFog,
		dynamicSun: options.dynamicSun,
		terrainRelief: options.terrainRelief,
		texturedHorizon: options.texturedHorizon,
		floatBloom: options.floatBloom
	};
}
