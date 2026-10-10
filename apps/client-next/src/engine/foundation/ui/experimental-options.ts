/*
===========================================================================

experimental-options.ts - explicitly opted-in browser additions

These settings are deliberately separate from the native SROptionSet.
Missing or invalid preferences never enable an experimental feature, so
the defaults are the native client: no presentation pass, no anisotropy,
linear fog and full-resolution rendering. Water reflection and equipment shine are native Video
options (Water reflection, Metal detail), not experimental ones.

===========================================================================
*/

/*
================
ExperimentalOptions
================
*/
export interface ExperimentalOptions {
	// Port-only, not native. Reduced scene resolution requires explicit opt-in.
	readonly renderScale: RenderScale;
	readonly chatTimestamps: boolean;
	// Port-only, not native: beginner atlas on M, explicitly enabled.
	readonly monsterGuide: boolean;
	readonly developerDiagnostics: boolean;
	// Video: renderer stages that deviate from the 2005 D3D9 look.
	readonly postProcessing: boolean;
	readonly anisotropicFiltering: boolean;
	readonly heightFog: boolean;
	readonly dynamicSun: boolean;
	readonly terrainRelief: boolean;
	readonly texturedHorizon: boolean;
	readonly floatBloom: boolean;
	readonly hdrToneMap: boolean;
	readonly sunShadow: boolean;
	readonly perPixelLighting: boolean;
}

/*
================
ExperimentalKey

Only boolean preferences may be passed to the window's toggle action.
================
*/
export type ExperimentalKey = {
	[Key in keyof ExperimentalOptions]: ExperimentalOptions[Key] extends boolean ? Key : never;
}[keyof ExperimentalOptions];

/*
================
RenderScale

Port-only, not native. 100 preserves full-resolution scene rendering.
================
*/
export type RenderScale = 100 | 75 | 50;
export const DEFAULT_RENDER_SCALE: RenderScale = 100;

/*
================
renderScales
================
*/
export function renderScales(): readonly RenderScale[] {
	return [ DEFAULT_RENDER_SCALE, 75, 50 ];
}

/*
================
experimentalOptions

Only an explicit true enables a toggle. Missing or invalid render scales
restore full resolution; legacy Video settings cannot enable scaling.
================
*/
export function experimentalOptions( value: unknown = null ): ExperimentalOptions {
	const record = typeof value === "object" && value !== null && !Array.isArray( value ) ?
		value as Record<string, unknown> :
		{};
	const enabled = ( key: ExperimentalKey ) => record[key] === true;
	return {
		renderScale: record.renderScale === 75 || record.renderScale === 50 ? record.renderScale : DEFAULT_RENDER_SCALE,
		chatTimestamps: enabled( "chatTimestamps" ),
		monsterGuide: enabled( "monsterGuide" ),
		developerDiagnostics: enabled( "developerDiagnostics" ),
		postProcessing: enabled( "postProcessing" ),
		anisotropicFiltering: enabled( "anisotropicFiltering" ),
		heightFog: enabled( "heightFog" ),
		dynamicSun: enabled( "dynamicSun" ),
		terrainRelief: enabled( "terrainRelief" ),
		texturedHorizon: enabled( "texturedHorizon" ),
		floatBloom: enabled( "floatBloom" ),
		hdrToneMap: enabled( "hdrToneMap" ),
		sunShadow: enabled( "sunShadow" ),
		perPixelLighting: enabled( "perPixelLighting" )
	};
}

/*
================
ExperimentalVideo

The renderer's slice of the preferences: the stages it switches at run
time. Every flag off and renderScale 100 preserve the native frame.
================
*/
export interface ExperimentalVideo {
	readonly renderScale: RenderScale;
	readonly postProcessing: boolean;
	readonly anisotropicFiltering: boolean;
	readonly heightFog: boolean;
	readonly dynamicSun: boolean;
	readonly terrainRelief: boolean;
	readonly texturedHorizon: boolean;
	readonly floatBloom: boolean;
	readonly hdrToneMap: boolean;
	readonly sunShadow: boolean;
	readonly perPixelLighting: boolean;
}

/*
================
experimentalVideo
================
*/
export function experimentalVideo( options: ExperimentalOptions ): ExperimentalVideo {
	return {
		renderScale: options.renderScale,
		postProcessing: options.postProcessing,
		anisotropicFiltering: options.anisotropicFiltering,
		heightFog: options.heightFog,
		dynamicSun: options.dynamicSun,
		terrainRelief: options.terrainRelief,
		texturedHorizon: options.texturedHorizon,
		floatBloom: options.floatBloom,
		hdrToneMap: options.hdrToneMap,
		sunShadow: options.sunShadow,
		perPixelLighting: options.perPixelLighting
	};
}
