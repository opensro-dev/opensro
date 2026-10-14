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
	readonly developerDiagnostics: boolean;
	// Video: renderer stages that deviate from the 2005 D3D9 look.
	readonly postProcessing: boolean;
	readonly anisotropicFiltering: boolean;
	readonly heightFog: boolean;
	readonly dynamicSun: boolean;
	readonly floatBloom: boolean;
	readonly hdrToneMap: boolean;
	readonly sunShadow: boolean;
	readonly perPixelLighting: boolean;
	// Gameplay: a skill press's run-up walks before the server's answer.
	readonly predictedRunUp: boolean;
	// Camera: ease the follow target's height and the collision release
	// (camera-smoothing.ts); native 68F830 snaps both.
	readonly smoothCamera: boolean;
	// Port-only, not native: the unused enchant sparks on +8 and higher
	// weapons and shields (equipmentEnchantSparks).
	readonly enchantSparks: boolean;
	// Port-only, not native: a device without BC is served re-encoded
	// textures (texture-variants.ts) instead of decoding them to RGBA.
	// Read when the asset worker starts, so it applies from the next load.
	// On unless turned off (the owner's choice, 2026-10-14): only devices
	// without BC are affected, where RGBA ends the tab; off is the native
	// texture content.
	readonly compressedTextures: boolean;
	// Port-only, not native: characters and effects share one nearest-first
	// order under the renderer's CHARACTER_ACTORS (the original keeps every
	// decoration, 85C020), so a far character gives way to a near effect.
	// On unless turned off (the owner's choice, 2026-10-14); off is the
	// previous rule, characters first.
	readonly sharedActorRoom: boolean;
	// Port-only, not native: a disabled tab is dimmed with a grey label (the
	// original draws it like an unselected tab, 5376A0). On unless turned
	// off (the owner's choice, 2026-10-15); off is the original look.
	readonly greyDisabledTabs: boolean;
	// Port-only, not native: the shard's job-kill announcements (their own
	// 0x3667 channel, server wire.JobKillNoticeChannel). On unless turned off
	// (the owner's choice, 2026-10-15); off hides them from the banner and
	// the chat log.
	readonly jobKillNotices: boolean;
	// Port-only, not native: the shard's seal-drop and +8..+11 announcements
	// (their own 0x3667 channel, server wire.DropPlusNoticeChannel). On unless
	// turned off (the owner's choice, 2026-10-15); off hides them from the
	// banner and the chat log.
	readonly dropPlusNotices: boolean;
	// Port-only, not native: holding V (UIIT_STT_USER_MOSTER_BLIND) also
	// hides other players' pets and transports. The original hides only
	// monsters and players (8602C0, 85E550): a COS draws through its owner
	// (CICCos_ResolveDrawOwner 854680), which ignores the key. Off unless
	// turned on (the balance advisor's ask, owner-approved 2026-10-15).
	readonly vHidesCompanions: boolean;
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

Only an explicit true enables a toggle, except compressedTextures,
sharedActorRoom, greyDisabledTabs, jobKillNotices and dropPlusNotices, which
only an explicit false turns off. Missing or invalid render scales
restore full resolution; legacy Video settings cannot enable scaling. Keys
of retired rows (terrainRelief and texturedHorizon, removed at the owner's
request on 2026-10-11) are never read, so a saved preference holding them
loads cleanly.
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
		developerDiagnostics: enabled( "developerDiagnostics" ),
		postProcessing: enabled( "postProcessing" ),
		anisotropicFiltering: enabled( "anisotropicFiltering" ),
		heightFog: enabled( "heightFog" ),
		dynamicSun: enabled( "dynamicSun" ),
		floatBloom: enabled( "floatBloom" ),
		hdrToneMap: enabled( "hdrToneMap" ),
		sunShadow: enabled( "sunShadow" ),
		perPixelLighting: enabled( "perPixelLighting" ),
		predictedRunUp: enabled( "predictedRunUp" ),
		smoothCamera: enabled( "smoothCamera" ),
		enchantSparks: enabled( "enchantSparks" ),
		compressedTextures: record.compressedTextures !== false,
		sharedActorRoom: record.sharedActorRoom !== false,
		greyDisabledTabs: record.greyDisabledTabs !== false,
		jobKillNotices: record.jobKillNotices !== false,
		dropPlusNotices: record.dropPlusNotices !== false,
		vHidesCompanions: enabled( "vHidesCompanions" )
	};
}

// Where the preferences are kept (the platform saves them).
export const EXPERIMENTAL_STORAGE_KEY = "sro:v1150:experimental-options:1";

/*
================
storedExperimentalOptions

The saved preferences from storage, or the defaults when there are none or
they cannot be read. The platform owns saving them; an owner that must act
before the platform starts (the asset worker's texture variant) reads them
here. Throws only for a stored value that is not JSON, which the platform
reports.
================
*/
export function storedExperimentalOptions( storage: Pick<Storage, "getItem"> | undefined ): {
	readonly options: ExperimentalOptions;
	readonly raw: unknown;
} {
	const stored = storage?.getItem( EXPERIMENTAL_STORAGE_KEY ) ?? null;
	const raw = stored === null ? null : JSON.parse( stored );
	return { options: experimentalOptions( raw ), raw };
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
	readonly floatBloom: boolean;
	readonly hdrToneMap: boolean;
	readonly sunShadow: boolean;
	readonly perPixelLighting: boolean;
	readonly smoothCamera: boolean;
	readonly enchantSparks: boolean;
	readonly sharedActorRoom: boolean;
	readonly vHidesCompanions: boolean;
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
		floatBloom: options.floatBloom,
		hdrToneMap: options.hdrToneMap,
		sunShadow: options.sunShadow,
		perPixelLighting: options.perPixelLighting,
		smoothCamera: options.smoothCamera,
		enchantSparks: options.enchantSparks,
		sharedActorRoom: options.sharedActorRoom,
		vHidesCompanions: options.vHidesCompanions
	};
}

/*
================
ExperimentalGameplay

The simulation worker's slice of the preferences. Every flag off is the
native client.
================
*/
export interface ExperimentalGameplay {
	readonly predictedRunUp: boolean;
	readonly jobKillNotices: boolean;
	readonly dropPlusNotices: boolean;
}

/*
================
experimentalGameplay
================
*/
export function experimentalGameplay( options: ExperimentalOptions ): ExperimentalGameplay {
	return {
		predictedRunUp: options.predictedRunUp,
		jobKillNotices: options.jobKillNotices,
		dropPlusNotices: options.dropPlusNotices
	};
}
