/*
===========================================================================

experimental-hud.ts - saved and draft experimental preferences

Opening starts a fresh draft on the first tab. Only Confirm changes
effective preferences; closing or Escape leaves the saved value intact.
The window's tabs (Video, Chat, Developer) only choose which rows show;
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

The window's tabs, Options style: Video holds the renderer stages that
deviate from the 2005 look, Chat and Developer the earlier additions.
================
*/
export const EXPERIMENTAL_TABS: readonly { readonly title: string; readonly rows: readonly ExperimentalRow[]; }[] = [
	{
		title: "Video",
		rows: [
			{
				key: "postProcessing",
				id: "experimental-post-processing",
				label: "Anti-aliasing and color grade",
				description: "Smooths edges and grades color, UI included."
			},
			{
				key: "anisotropicFiltering",
				id: "experimental-anisotropic-filtering",
				label: "Anisotropic filtering",
				description: "Sharper ground and walls at shallow angles."
			},
			{
				key: "heightFog",
				id: "experimental-height-fog",
				label: "Height fog",
				description: "Distance haze that thins with height."
			},
			{
				key: "waterReflection",
				id: "experimental-water-reflection",
				label: "Water reflections",
				description: "Water reflects the sky by viewing angle."
			},
			{
				key: "garmentSheen",
				id: "experimental-garment-sheen",
				label: "Clothing sheen",
				description: "Gloss highlights on clothing."
			}
		]
	},
	{
		title: "Chat",
		rows: [ {
			key: "chatTimestamps",
			id: "experimental-chat-timestamps",
			label: "Chat timestamps",
			description: "Show message time on hover."
		} ]
	},
	{
		title: "Developer",
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
