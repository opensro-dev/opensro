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
and one-line help. Numeric preferences never pass through toggle(). A row
with a parent only acts while its parent is on (the pickup filter's
kinds): it is drawn indented, and dimmed while the parent is off.
================
*/
export interface ExperimentalRow {
	readonly key: ExperimentalKey | "renderScale" | "uiScale";
	readonly id: string;
	readonly label: string;
	readonly description: string;
	readonly parent?: ExperimentalKey;
}

/*
================
ExperimentalSection

One titled run of rows inside a tab's list.
================
*/
export interface ExperimentalSection {
	readonly title: string;
	readonly rows: readonly ExperimentalRow[];
}

/*
================
ExperimentalTab
================
*/
export interface ExperimentalTab {
	readonly title: string;
	readonly sections: readonly ExperimentalSection[];
}

/*
================
experimentalRows

Every row of a tab, its sections in order.
================
*/
export function experimentalRows( tab: ExperimentalTab ): readonly ExperimentalRow[] {
	return tab.sections.flatMap( section => section.rows );
}

/*
================
EXPERIMENTAL_TABS

Port-only, not native. Six tabs by subject - Graphics, Interface, Game,
Map, Notices, Pets - each one scrolling list of titled sections
(foundation/ui/experimental-list.ts), so a tab holds any number of rows
and every title reads in full. Opening the window returns to Graphics.
================
*/
export const EXPERIMENTAL_TABS: readonly ExperimentalTab[] = [
	{
		title: "Graphics",
		sections: [
			{
				title: "Image",
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
						// Port-only, not native: beside the post-process
						// anti-aliasing, the other edge smoothing. 4x is the
						// one multisample count WebGPU guarantees besides 1.
						key: "msaa",
						id: "experimental-msaa",
						label: "MSAA 4x",
						description: "Scene edges smooth through 4x multisampling."
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
				title: "Light and sky",
				rows: [
					{
						key: "sunShadow",
						id: "experimental-sun-shadow",
						label: "Sun shadows",
						description: "Scene shadows cast by the sunlight."
					},
					{
						key: "dynamicSun",
						id: "experimental-dynamic-sun",
						label: "Moving sunlight",
						description: "Lighting follows the time of day."
					},
					{
						key: "perPixelLighting",
						id: "experimental-per-pixel-lighting",
						label: "Per-pixel character light",
						description: "Smooth lighting on characters and objects."
					},
					{
						key: "heightFog",
						id: "experimental-height-fog",
						label: "Height fog",
						description: "Distance haze that thins with height."
					},
					{
						key: "skyDetail",
						id: "experimental-sky-detail",
						label: "Sky detail",
						description: "Drifting clouds, sun glow and rim light."
					},
					{
						// Port-only, not native (#2094).
						key: "ambientOcclusion",
						id: "experimental-ambient-occlusion",
						label: "Ambient occlusion",
						description: "Shade corners and contact seams from depth."
					}
				]
			},
			{
				title: "Textures and effects",
				rows: [
					{
						key: "enchantSparks",
						id: "experimental-enchant-sparks",
						label: "Enhanced item glow",
						description: "Richer glow and sparks on reinforced gear."
					},
					{
						key: "sharedActorRoom",
						id: "experimental-shared-actor-room",
						label: "Effects in crowds",
						description: "In a crowd, near skill effects draw before far characters."
					},
					{
						key: "compressedTextures",
						id: "experimental-compressed-textures",
						label: "Compressed textures",
						description: "Phones and tablets: less memory. After reload."
					},
					{
						// Port-only, not native (#2061): the row is live, no reload -
						// switching it on fetches the set's own index and starts its
						// download; off stops the download and the originals serve.
						// Listed only while the build carries the set (#2800,
						// experimentalTabs), so it is never a dead toggle.
						key: "hdTextures",
						id: "experimental-hd-textures",
						label: "HD textures",
						description: "Sharper world textures; downloads while on."
					},
					{
						key: "dungeonCharacterPvs",
						id: "experimental-dungeon-character-pvs",
						label: "Dungeon character PVS",
						description: "Hide characters in unseen dungeon rooms."
					}
				]
			}
		]
	},
	{
		title: "Interface",
		sections: [
			{
				title: "Windows",
				rows: [
					{
						key: "fitWindowLabels",
						id: "experimental-fit-window-labels",
						label: "Fit window labels",
						description: "Fit long labels and captions; open lists upward."
					},
					{
						key: "greyDisabledTabs",
						id: "experimental-grey-disabled-tabs",
						label: "Grey disabled tabs",
						description: "Dim a window tab you cannot use."
					},
					{
						key: "dropOnBagTab",
						id: "experimental-drop-on-bag-tab",
						label: "Drop on bag tabs",
						description: "Drop an item on a bag tab to move it to that page."
					},
					{
						key: "exchangeClickShortcuts",
						id: "experimental-exchange-click-shortcuts",
						label: "Exchange click shortcuts",
						description: "Ctrl+click, right-click or double-click a bag item to offer it."
					},
					{
						key: "confirmGuildBan",
						id: "experimental-confirm-guild-ban",
						label: "Confirm guild ban",
						description: "Ask before Withdraw bans a guild member."
					},
					{
						key: "touchInspect",
						id: "experimental-touch-inspect",
						label: "Tap shows item details",
						description: "On touch, a tap on an item or skill keeps its tooltip up."
					}
				]
			},
			{
				title: "Items and mall",
				rows: [
					{
						key: "sealUnderHeading",
						id: "experimental-seal-under-heading",
						label: "Seal line under item name",
						description: "Show a rare item's seal right under its name."
					},
					{
						key: "silkHistory",
						id: "experimental-silk-history",
						label: "Silk charge history",
						description: "The Item Mall's Buying list: silk spending by period."
					}
				]
			},
			{
				title: "Chat",
				rows: [
					{
						key: "chatTimestamps",
						id: "experimental-chat-timestamps",
						label: "Chat timestamps",
						description: "Show message time on hover."
					},
					{
						key: "eventChatTab",
						id: "experimental-event-chat-tab",
						label: "Event chat tab",
						description: "Event notices get their own chat tab."
					},
					{
						key: "chatItemLinks",
						id: "experimental-chat-item-links",
						label: "Item links in chat",
						description: "Alt+click an item while typing to link it in chat."
					}
				]
			},
			{
				title: "Other",
				rows: [
					{
						key: "safeLoadingScreen",
						id: "experimental-safe-loading-screen",
						label: "Safe loading screen",
						description: "Hide inappropriate loading images."
					},
					{
						key: "developerDiagnostics",
						id: "experimental-developer-diagnostics",
						label: "Developer diagnostics",
						description: "Show a diagnostics icon beside FPS."
					}
				]
			}
		]
	},
	{
		title: "Game",
		sections: [
			{
				title: "Movement and camera",
				rows: [
					{
						key: "predictedRunUp",
						id: "experimental-predicted-run-up",
						label: "Predicted run-up",
						description: "Walk to a far target on press; stop at the cast."
					},
					{
						key: "mainThreadOwnWalk",
						id: "experimental-main-thread-own-walk",
						label: "Instant own movement",
						description: "Your character starts walking on the click frame."
					},
					{
						key: "smoothCamera",
						id: "experimental-smooth-camera",
						label: "Smooth camera",
						description: "Ease small camera jumps and the return from a wall."
					}
				]
			},
			{
				title: "Combat",
				rows: [
					{
						key: "autoSelect",
						id: "experimental-auto-select",
						label: "Auto Select",
						description: "Actions window: select the nearest monster."
					},
					{
						key: "homingProjectiles",
						id: "experimental-homing-projectiles",
						label: "Homing projectiles",
						description: "Steer straight projectiles toward a moving target."
					},
					{
						key: "uniqueDamageRank",
						id: "experimental-unique-damage-rank",
						label: "Unique damage ranking",
						description: "When a unique you hit dies, show who did the most."
					},
					{
						key: "vHidesCompanions",
						id: "experimental-v-hides-companions",
						label: "V hides pets and transports",
						description: "Holding V also hides other players' pets and transports."
					}
				]
			},
			{
				title: "Sound and connection",
				rows: [
					{
						key: "warpArrivalSound",
						id: "experimental-warp-arrival-sound",
						label: "Portal arrival sound",
						description: "Play the portal-out cue when you arrive."
					},
					{
						key: "webTransport",
						id: "experimental-web-transport",
						label: "WebTransport connection",
						description: "Connect over QUIC/UDP; falls back to WebSocket."
					}
				]
			}
		]
	},
	{
		title: "Map",
		sections: [
			{
				title: "World map (M)",
				rows: [
					{
						key: "monsterGuide",
						id: "experimental-monster-guide",
						label: "Monster hunting guide",
						description: "Monster portraits and approximate level ranges on M."
					}
				]
			},
			{
				title: "Map and minimap",
				rows: [
					{
						key: "questTrackerMarkers",
						id: "experimental-quest-tracker-markers",
						label: "Quest target markers",
						description: "Show every active quest's target on M and the minimap."
					}
				]
			},
			{
				title: "Ground",
				rows: [
					{
						key: "sunDropMarker",
						id: "experimental-sun-drop-marker",
						label: "Seal of Sun drop marker",
						description: "Mark your own degree-9+ Sun drops on the ground."
					}
				]
			}
		]
	},
	{
		title: "Notices",
		sections: [
			{
				title: "Shard notices",
				rows: [
					{
						key: "jobKillNotices",
						id: "experimental-job-kill-notices",
						label: "Job kill notices",
						description: "Show the shard's job kill announcements."
					},
					{
						key: "dropPlusNotices",
						id: "experimental-drop-plus-notices",
						label: "Drop & plus notices",
						description: "Show the shard's seal drop and +8-or-higher notices."
					},
					{
						key: "plusSevenNotices",
						id: "experimental-plus-seven-notices",
						label: "Plus +7 notices",
						description: "Show the shard's +7 alchemy announcements."
					},
					{
						key: "breakNotices",
						id: "experimental-break-notices",
						label: "Item break notices",
						description: "Show the shard's item break announcements."
					},
					{
						key: "highlightEventNotices",
						id: "experimental-highlight-event-notices",
						label: "Highlight event notices",
						description: "Show the shard's event announcements in red."
					},
					{
						key: "noticeLinks",
						id: "experimental-notice-links",
						label: "Links in notices",
						description: "Hover an item for details; click a name to whisper."
					},
					{
						key: "shortNotices",
						id: "experimental-short-notices",
						label: "Short notices",
						description: "Show notices for up to the original 7 seconds."
					}
				]
			}
		]
	},
	{
		title: "Pets",
		sections: [
			{
				title: "Companions",
				rows: [
					{
						key: "transportAutoPotion",
						id: "experimental-transport-auto-potion",
						label: "Companion auto-potion",
						description: "Pet and transport HP and cure settings in the T dialog."
					},
					{
						key: "petClickOpensWindow",
						id: "experimental-pet-click-opens-window",
						label: "Pet panel opens window",
						description: "Click a pet panel to open its window."
					}
				]
			},
			{
				title: "Pickup pet",
				rows: [
					{
						key: "smartPetPickup",
						id: "experimental-smart-pet-pickup",
						label: "Keep pet gold pickup when full",
						description: "Keep picking up gold when a full bag refuses an item."
					},
					{
						key: "petPickupFilter",
						id: "experimental-pet-pickup-filter",
						label: "Pickup pet item filter",
						description: "Choose what the pickup pet collects."
					},
					{
						key: "petFilterGold",
						id: "experimental-pet-filter-gold",
						label: "Gold",
						description: "Collect gold; only while the item filter is on.",
						parent: "petPickupFilter"
					},
					{
						key: "petFilterEquipment",
						id: "experimental-pet-filter-equipment",
						label: "Equipment",
						description: "Collect equipment; only while the filter is on.",
						parent: "petPickupFilter"
					},
					{
						key: "petFilterAlchemy",
						id: "experimental-pet-filter-alchemy",
						label: "Alchemy",
						description: "Elixirs, stones and tablets; only with the filter.",
						parent: "petPickupFilter"
					},
					{
						key: "petFilterPotions",
						id: "experimental-pet-filter-potions",
						label: "Potions",
						description: "Collect potions; only while the filter is on.",
						parent: "petPickupFilter"
					},
					{
						key: "petFilterOther",
						id: "experimental-pet-filter-other",
						label: "Other items",
						description: "Scrolls, arrows and the rest; only with the filter.",
						parent: "petPickupFilter"
					}
				]
			}
		]
	}
];

const EXPERIMENTAL_TAB_COUNT = EXPERIMENTAL_TABS.length;

/*
================
experimentalTabs

Port-only, not native. The shard's published reference is the capability;
a saved local preference cannot reveal an option the shard has disabled.
The same rule hides the HD row (#2800): it is listed only while the
published build carries the set's index (the main pack index certifies
it; hdAvailable). A section left with no rows goes with it, then a tab
left with no sections.
================
*/
export function experimentalTabs(
	session: SessionState | null | undefined,
	hdAvailable: boolean
): readonly ExperimentalTab[] {
	const guide = session?.phase === "world" && !!session.huntingGuide;
	const listed = ( row: ExperimentalRow ) =>
		(guide || row.key !== "monsterGuide") && (hdAvailable || row.key !== "hdTextures");
	return EXPERIMENTAL_TABS
		.map( tab => ({
			...tab,
			sections: tab.sections
				.map( section => ({ ...section, rows: section.rows.filter( listed ) }) )
				.filter( section => section.rows.length > 0 )
		}) )
		.filter( tab => tab.sections.length > 0 );
}

// A press that moved less than this (in pixels, as the platform's touch
// hold slop) and stepped no row is still a click on the row.
const EXPERIMENTAL_CLICK_SLOP = 8;

/*
================
createExperimentalHud
================
*/
export function createExperimentalHud() {
	// scroll is the list's row step. A drag carries its pixels until they
	// make whole rows, and remembers how far it travelled for the click test.
	let saved = experimentalOptions(), draft = saved, tab = 0, scroll = 0;
	let dragCarry = 0, dragTravel = 0, dragStepped = false;
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
			scroll = 0;
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
			scroll = 0;
		},
		/*
		================
		scroll

		Moves the list by delta items, held inside [0, range].
		================
		*/
		scroll( delta: number, range: number ) {
			scroll = Math.max( 0, Math.min( Math.max( 0, range ), scroll + delta ) );
		},
		/*
		================
		drag

		A thumb or touch drag: pixels toward the list's end, stepPixels per
		row. The list moves a whole row once the carry passes half a row
		either way, so a pixel of jitter never moves it.
		================
		*/
		drag( pixels: number, stepPixels: number, range: number ) {
			dragCarry += pixels;
			dragTravel += Math.abs( pixels );
			const steps = Math.sign( dragCarry ) * Math.floor( Math.abs( dragCarry ) / stepPixels + 0.5 );
			if ( steps === 0 ) return;
			dragCarry -= steps * stepPixels;
			dragStepped = true;
			scroll = Math.max( 0, Math.min( Math.max( 0, range ), scroll + steps ) );
		},
		/*
		================
		endDrag

		Ends the drag; true when it was a click: it stayed inside the slop
		and stepped no row.
		================
		*/
		endDrag() {
			const click = !dragStepped && dragTravel < EXPERIMENTAL_CLICK_SLOP;
			dragCarry = 0;
			dragTravel = 0;
			dragStepped = false;
			return click;
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
			return { saved, draft, tab, scroll };
		}
	};
}
