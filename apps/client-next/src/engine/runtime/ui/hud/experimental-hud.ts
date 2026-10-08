/*
===========================================================================

experimental-hud.ts - saved and draft experimental preferences

Opening starts a fresh draft on the first tab. Only Confirm changes
effective preferences; closing or Escape leaves the saved value intact.
The window's tabs (Image, World, Chat, Developer) only choose which rows show;
every tab edits the same draft.

===========================================================================
*/

import {
	experimentalOptions,
	type ExperimentalKey,
	type ExperimentalOptions
} from "@/engine/foundation/ui/experimental-options";

/*
================
ExperimentalRow

One checkbox row: the preference it toggles, its control id, its label
and the one-line help under it.
================
*/
export interface ExperimentalRow {
	readonly key: ExperimentalKey;
	readonly id: string;
	readonly label: string;
	readonly description: string;
}

/*
================
EXPERIMENTAL_TABS

Port-only, not native. The window's tabs, Options style, at most four rows each: Image holds the
frame-wide stages (edges, filtering, glow), World the lighting and
atmosphere stages that deviate from the 2005 look, Chat and Developer the
earlier additions.
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
