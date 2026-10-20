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
				key: "compressedTextures",
				id: "experimental-compressed-textures",
				label: "Compressed textures",
				description: "Phones and tablets: less memory. After reload."
			},
			{
				key: "sharedActorRoom",
				id: "experimental-shared-actor-room",
				label: "Effects in crowds",
				description: "In a crowd, near skill effects draw before far characters."
			},
			{
				key: "homingProjectiles",
				id: "experimental-homing-projectiles",
				label: "Homing projectiles",
				description: "Steer straight projectiles toward a moving target."
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
			},
			{
				key: "enchantSparks",
				id: "experimental-enchant-sparks",
				label: "Enchant sparks",
				description: "Sparks on +8 and higher weapons and shields."
			},
			{
				key: "dungeonCharacterPvs",
				id: "experimental-dungeon-character-pvs",
				label: "Dungeon character PVS",
				description: "Hide characters in unseen dungeon rooms."
			}
		]
	},
	{
		title: "Game",
		section: "Chat and movement",
		rows: [ {
			key: "chatTimestamps",
			id: "experimental-chat-timestamps",
			label: "Chat timestamps",
			description: "Show message time on hover."
		}, {
			key: "predictedRunUp",
			id: "experimental-predicted-run-up",
			label: "Predicted run-up",
			description: "Walk to a far target on press; may jump at the cast."
		}, {
			key: "smoothCamera",
			id: "experimental-smooth-camera",
			label: "Smooth camera",
			description: "Ease small camera jumps and the return from a wall."
		}, {
			key: "greyDisabledTabs",
			id: "experimental-grey-disabled-tabs",
			label: "Grey disabled tabs",
			description: "Dim a window tab you cannot use."
		}, {
			key: "vHidesCompanions",
			id: "experimental-v-hides-companions",
			label: "V hides pets and transports",
			description: "Holding V also hides other players' pets and transports."
		} ]
	},
	{
		title: "Notices",
		section: "Shard notices",
		rows: [ {
			key: "jobKillNotices",
			id: "experimental-job-kill-notices",
			label: "Job kill notices",
			description: "Show the shard's job kill announcements."
		}, {
			key: "dropPlusNotices",
			id: "experimental-drop-plus-notices",
			label: "Drop & plus notices",
			description: "Show the shard's seal drop and +8-or-higher notices."
		}, {
			key: "plusSevenNotices",
			id: "experimental-plus-seven-notices",
			label: "Plus +7 notices",
			description: "Show the shard's +7 alchemy announcements."
		}, {
			key: "noticeLinks",
			id: "experimental-notice-links",
			label: "Links in notices",
			description: "Hover an item for details; click a name to whisper."
		}, {
			key: "shortNotices",
			id: "experimental-short-notices",
			label: "Short notices",
			description: "Show notices for up to the original 7 seconds."
		} ]
	},
	{
		title: "Other",
		section: "Windows and diagnostics",
		rows: [ {
			key: "developerDiagnostics",
			id: "experimental-developer-diagnostics",
			label: "Developer diagnostics",
			description: "Show a diagnostics icon beside FPS."
		}, {
			key: "fitWindowLabels",
			id: "experimental-fit-window-labels",
			label: "Fit window labels",
			description: "Fit long labels and captions; open lists upward."
		}, {
			key: "dropOnBagTab",
			id: "experimental-drop-on-bag-tab",
			label: "Drop on bag tabs",
			description: "Drop an item on a bag tab to move it to that page."
		}, {
			key: "confirmGuildBan",
			id: "experimental-confirm-guild-ban",
			label: "Confirm guild ban",
			description: "Ask before Withdraw bans a guild member."
		}, {
			key: "petClickOpensWindow",
			id: "experimental-pet-click-opens-window",
			label: "Pet panel opens window",
			description: "Click a pet panel to open its window."
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
