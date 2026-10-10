/*
===========================================================================

experimental-hud.ts - saved and draft experimental preferences

Opening starts a fresh draft on the first tab. Only Confirm changes
effective preferences; closing or Escape leaves the saved value intact.
The window's tabs only choose which rows show;
every tab edits the same draft.

===========================================================================
*/

import {
	experimentalOptions,
	type ExperimentalKey,
	type ExperimentalOptions,
	renderScales
} from "@/engine/foundation/ui/experimental-options";

/*
================
ExperimentalRow

A checkbox or numeric selection: its preference key, control id, label
and one-line help. Numeric preferences never pass through toggle().
================
*/
export interface ExperimentalRow {
	readonly key: ExperimentalKey | "renderScale";
	readonly id: string;
	readonly label: string;
	readonly description: string;
}

/*
================
EXPERIMENTAL_TABS

Port-only, not native. The window's tabs, Options style: Image holds the
frame-wide stages and scene resolution, World the
atmosphere stages that deviate from the 2005 look, Lighting the direct-light
stages, Chat and Developer the earlier additions.
================
*/
export const EXPERIMENTAL_TABS: readonly {
	readonly title: string;
	readonly section: string;
	readonly rows: readonly ExperimentalRow[];
}[] = [
	{
		title: "Image",
		section: "Image quality",
		rows: [
			{
				key: "renderScale",
				id: "experimental-render-scale",
				label: "Render scale",
				description: "100% is native; lower values soften the scene."
			},
			{
				key: "postProcessing",
				id: "experimental-post-processing",
				label: "Anti-aliasing and color grade",
				description: "Smooths edges and adjusts color, including UI."
			},
			{
				key: "anisotropicFiltering",
				id: "experimental-anisotropic-filtering",
				label: "Anisotropic filtering",
				description: "Sharper textures viewed at shallow angles."
			},
			{
				key: "floatBloom",
				id: "experimental-float-bloom",
				label: "Smooth bloom",
				description: "Requires Bloom effect in Video options."
			},
			{
				key: "hdrToneMap",
				id: "experimental-hdr-tone-map",
				label: "HDR tone map",
				description: "Float frame with filmic highlight roll-off."
			}
		]
	},
	{
		title: "World",
		section: "Lighting and terrain",
		rows: [
			{
				key: "heightFog",
				id: "experimental-height-fog",
				label: "Height fog",
				description: "Distance haze that thins with height."
			},
			{
				key: "dynamicSun",
				id: "experimental-dynamic-sun",
				label: "Moving sunlight",
				description: "Lighting follows the time of day."
			},
			{
				key: "terrainRelief",
				id: "experimental-terrain-relief",
				label: "Terrain relief",
				description: "Shades slopes. Reloads the current area."
			},
			{
				key: "texturedHorizon",
				id: "experimental-textured-horizon",
				label: "Textured horizon",
				description: "Shows distant ground textures; may shimmer."
			}
		]
	},
	{
		title: "Lighting",
		section: "Light and shadows",
		rows: [
			{
				key: "sunShadow",
				id: "experimental-sun-shadow",
				label: "Sun shadows",
				description: "Scene shadows cast by the sunlight."
			},
			{
				key: "perPixelLighting",
				id: "experimental-per-pixel-lighting",
				label: "Per-pixel character light",
				description: "Smooth lighting on characters and objects."
			}
		]
	},
	{
		title: "Chat",
		section: "Chat display",
		rows: [ {
			key: "chatTimestamps",
			id: "experimental-chat-timestamps",
			label: "Chat timestamps",
			description: "Show message time on hover."
		} ]
	},
	{
		title: "Developer",
		section: "Diagnostics",
		rows: [ {
			key: "developerDiagnostics",
			id: "experimental-developer-diagnostics",
			label: "Developer diagnostics",
			description: "Show a diagnostics icon beside FPS."
		} ]
	}
];

const EXPERIMENTAL_TAB_COUNT = EXPERIMENTAL_TABS.length;

/*
================
createExperimentalHud
================
*/
export function createExperimentalHud() {
	let saved = experimentalOptions(), draft = saved, tab = 0;
	return {
		/*
		================
		restore
		================
		*/
		restore( value: ExperimentalOptions ) {
			saved = experimentalOptions( value );
			draft = saved;
		},
		/*
		================
		open
		================
		*/
		open() {
			draft = saved;
			tab = 0;
		},
		/*
		================
		toggle
		================
		*/
		toggle( key: ExperimentalKey ) {
			draft = { ...draft, [key]: !draft[key] };
		},
		/*
		================
		selectRenderScale

		Port-only, not native. Invalid UI selections leave the draft intact.
		================
		*/
		selectRenderScale( value: number ) {
			const renderScale = renderScales().find( scale => scale === value );
			if ( renderScale === undefined ) return;
			draft = { ...draft, renderScale };
		},
		/*
		================
		selectTab
		================
		*/
		selectTab( index: number ) {
			if ( !Number.isInteger( index ) || index < 0 || index >= EXPERIMENTAL_TAB_COUNT ) {
				throw Error( "Invalid experimental tab" );
			}
			tab = index;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			draft = experimentalOptions();
		},
		/*
		================
		confirm
		================
		*/
		confirm() {
			saved = draft;
			return saved;
		},
		/*
		================
		state
		================
		*/
		state() {
			return { saved, draft, tab };
		}
	};
}
