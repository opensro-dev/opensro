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
	renderScales,
	uiScaleStop
} from "@/engine/foundation/ui/experimental-options";
import type { SessionState } from "@/engine/contracts/session";

/*
================
ExperimentalRow

A checkbox or numeric selection: its preference key, control id, label
and one-line help. Numeric preferences never pass through toggle().
================
*/
export interface ExperimentalRow {
	readonly key: ExperimentalKey | "renderScale" | "uiScale";
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
stages, Chat and Developer the earlier additions. Pickup holds the item
filter's kinds (#841); it is appended last so the existing tabs keep the
indices a reopening window relies on.
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
				key: "phoneRenderCap",
				id: "experimental-phone-render-cap",
				label: "Phone resolution",
				description: "Phones: lighter scene while Render scale is 100%."
			},
			{
				key: "uiScale",
				id: "experimental-ui-scale",
				label: "UI size",
				description: "Scale windows and text; 100% is native size."
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
		section: "World and map",
		rows: [
			{
				key: "monsterGuide",
				id: "experimental-monster-guide",
				label: "Monster hunting guide",
				description: "Monster portraits and approximate level ranges on M."
			},
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
			},
			{
				key: "sunDropMarker",
				id: "experimental-sun-drop-marker",
				label: "Seal of Sun drop marker",
				description: "Mark your own degree-9+ Sun drops on the ground."
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
				key: "ambientOcclusion",
				id: "experimental-ambient-occlusion",
				label: "Ambient occlusion",
				description: "Corners and contact points shade their ambient light."
			},
			{
				key: "enchantSparks",
				id: "experimental-enchant-sparks",
				label: "Enhanced item glow",
				description: "Richer glow and sparks on reinforced gear."
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
			key: "eventChatTab",
			id: "experimental-event-chat-tab",
			label: "Event chat tab",
			description: "Event notices get their own chat tab."
		}, {
			key: "predictedRunUp",
			id: "experimental-predicted-run-up",
			label: "Predicted run-up",
			description: "Walk to a far target on press; stop at the cast."
		}, {
			key: "smoothCamera",
			id: "experimental-smooth-camera",
			label: "Smooth camera",
			description: "Ease small camera jumps and the return from a wall."
		}, {
			key: "mainThreadOwnWalk",
			id: "experimental-main-thread-own-walk",
			label: "Instant own movement",
			description: "Your character starts walking on the click frame."
		}, {
			key: "touchInspect",
			id: "experimental-touch-inspect",
			label: "Tap shows item details",
			description: "On touch, a tap on an item or skill keeps its tooltip up."
		}, {
			key: "webTransport",
			id: "experimental-web-transport",
			label: "WebTransport connection",
			description: "Connect over QUIC/UDP; falls back to WebSocket."
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
			key: "breakNotices",
			id: "experimental-break-notices",
			label: "Item break notices",
			description: "Show the shard's item break announcements."
		}, {
			key: "uniqueDamageRank",
			id: "experimental-unique-damage-rank",
			label: "Unique damage ranking",
			description: "When a unique you hit dies, show who did the most."
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
			key: "greyDisabledTabs",
			id: "experimental-grey-disabled-tabs",
			label: "Grey disabled tabs",
			description: "Dim a window tab you cannot use."
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
			key: "exchangeClickShortcuts",
			id: "experimental-exchange-click-shortcuts",
			label: "Exchange click shortcuts",
			description: "Ctrl+click, right-click or double-click a bag item to offer it."
		}, {
			key: "confirmGuildBan",
			id: "experimental-confirm-guild-ban",
			label: "Confirm guild ban",
			description: "Ask before Withdraw bans a guild member."
		}, {
			key: "safeLoadingScreen",
			id: "experimental-safe-loading-screen",
			label: "Safe loading screen",
			description: "Hide inappropriate loading images."
		} ]
	},
	{
		title: "Pets",
		section: "Attack pets",
		rows: [ {
			key: "attackPetTraining",
			id: "experimental-attack-pet-training",
			label: "Attack-pet training",
			description: "Pet skills and gear; requires server opt-in."
		}, {
			key: "transportAutoPotion",
			id: "experimental-transport-auto-potion",
			label: "Transport auto-potion",
			description: "Heal your transport at your auto-potion HP %."
		}, {
			key: "petClickOpensWindow",
			id: "experimental-pet-click-opens-window",
			label: "Pet panel opens window",
			description: "Click a pet panel to open its window."
		}, {
			key: "petPickupFilter",
			id: "experimental-pet-pickup-filter",
			label: "Pickup pet item filter",
			description: "Choose what the pickup pet collects: Pickup tab."
		}, {
			key: "expandedWorldMap",
			id: "experimental-expanded-world-map",
			label: "Expanded world map",
			description: "Show extra town pages and wider overview."
		}, {
			key: "vHidesCompanions",
			id: "experimental-v-hides-companions",
			label: "V hides pets and transports",
			description: "Holding V also hides other players' pets and transports."
		}, {
			// Moved from Game (#1976): the window fits seven rows a tab
			// and the Event chat tab row joins Chat and movement; the
			// option, its key and its default are unchanged.
			key: "questTrackerMarkers",
			id: "experimental-quest-tracker-markers",
			label: "Quest target markers",
			description: "Show every active quest's target on M and the minimap."
		} ]
	},
	{
		title: "Pickup",
		section: "Pickup pet filter",
		rows: [ {
			key: "smartPetPickup",
			id: "experimental-smart-pet-pickup",
			label: "Keep pet gold pickup when full",
			description: "Keep picking up gold when a full bag refuses an item."
		}, {
			key: "petFilterGold",
			id: "experimental-pet-filter-gold",
			label: "Gold",
			description: "Collect gold; only while the item filter is on."
		}, {
			key: "petFilterEquipment",
			id: "experimental-pet-filter-equipment",
			label: "Equipment",
			description: "Collect equipment; only while the filter is on."
		}, {
			key: "petFilterAlchemy",
			id: "experimental-pet-filter-alchemy",
			label: "Alchemy",
			description: "Elixirs, stones and tablets; only with the filter."
		}, {
			key: "petFilterPotions",
			id: "experimental-pet-filter-potions",
			label: "Potions",
			description: "Collect potions; only while the filter is on."
		}, {
			key: "petFilterOther",
			id: "experimental-pet-filter-other",
			label: "Other items",
			description: "Scrolls, arrows and the rest; only with the filter."
		} ]
	}
];

const EXPERIMENTAL_TAB_COUNT = EXPERIMENTAL_TABS.length;

/*
================
experimentalTabs

Port-only, not native. The shard's published reference is the capability;
a saved local preference cannot reveal an option the shard has disabled.
================
*/
export function experimentalTabs( session: SessionState | null | undefined ) {
	if ( session?.phase === "world" && session.huntingGuide ) return EXPERIMENTAL_TABS;
	return EXPERIMENTAL_TABS.map( tab => ({ ...tab, rows: tab.rows.filter( row => row.key !== "monsterGuide" ) }) );
}

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
		setUiScale

		Port-only, not native. The slider's stop becomes the draft's UI size;
		a value off the slider leaves the draft intact.
		================
		*/
		setUiScale( value: number ) {
			const uiScale = uiScaleStop( value );
			if ( uiScale === undefined ) return;
			draft = { ...draft, uiScale };
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
