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

import {
	PICKUP_KIND_ALCHEMY,
	PICKUP_KIND_EQUIPMENT,
	PICKUP_KIND_GOLD,
	PICKUP_KIND_OTHER,
	PICKUP_KIND_POTIONS
} from "@/engine/foundation/gameplay/ground-item";

/*
================
ExperimentalOptions
================
*/
export interface ExperimentalOptions {
	// Port-only, not native. Reduced scene resolution requires explicit opt-in.
	readonly renderScale: RenderScale;
	// Port-only, not native: "Phone resolution" (#1806). On a phone, while
	// Render scale is 100%, the scene is drawn at the largest render scale
	// whose backing store fits PHONE_SCENE_PIXEL_BUDGET; the HUD keeps its
	// full resolution. A render scale the player picks below 100 wins. On
	// unless turned off (the owner's choice, 2026-10-27); off, or any
	// display that is not a phone, is full resolution.
	readonly phoneRenderCap: boolean;
	// Port-only, not native: the interface drawn larger or smaller than the
	// native 1:1 size (the platform's display scale). 100 is the native
	// size; a missing or invalid stored value restores it.
	readonly uiScale: number;
	readonly chatTimestamps: boolean;
	// Port-only, not native (#1976): a fifth chat tab, Event, holding the
	// shard's channel-7 event notices (seal drops, plus results, item
	// breaks, job kills) that otherwise land in All. On unless turned off
	// (the owner's choice, 2026-10-28); off is the native four tabs with
	// notices in All.
	readonly eventChatTab: boolean;
	// Port-only, not native: use only the male warrior loading artwork.
	// Off by default so both original loading backgrounds remain available.
	readonly safeLoadingScreen: boolean;
	// Port-only, not native: beginner atlas on M, explicitly enabled.
	readonly monsterGuide: boolean;
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
	// Port-only, not native: the water refracts the frame behind it and
	// whitens at the shore (device/water-refraction.ts), drawing apart
	// from the main pass. Off by default; off is the native blended
	// reflection, byte for byte.
	readonly waterRefraction: boolean;
	// Gameplay: a skill press's run-up walks before the server's answer.
	readonly predictedRunUp: boolean;
	// Camera: ease the follow target's height and the collision release
	// (camera-smoothing.ts); native 68F830 snaps both. On by default (owner
	// 2026-10-15); an explicit false is the native snap.
	readonly smoothCamera: boolean;
	// Port-only, not native: "Enhanced item glow". The later client's glow
	// tiers (+6/8/10 beside +3/5/7/9) and its per-item enchant sparks
	// (equipmentGlowCatalog, equipmentEnchantSparks). On by default (owner
	// 2026-10-17, 2026-10-24); an explicit false restores the native v1.150 look.
	readonly enchantSparks: boolean;
	// Port-only wolf skill tree controls; requires the server's matching opt-in.
	readonly attackPetTraining: boolean;
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
	// Port-only, not native: the local player's ground-click walk is stepped
	// on the main thread from the click frame (runtime/movement/local-mover.ts,
	// Unreal's locally controlled character, epic #1437, with
	// reconciliation #1441b). The original client predicts its own walk the
	// same way (0x6932A0 -> StartGroundMove 0x878600). On unless turned off
	// (the owner's choice, 2026-10-25); off is the worker-driven walk.
	readonly mainThreadOwnWalk: boolean;
	// Port-only, not native: a disabled tab is dimmed with a grey label (the
	// original draws it like an unselected tab, 5376A0). On unless turned
	// off (the owner's choice, 2026-10-15); off is the original look.
	readonly greyDisabledTabs: boolean;
	// Port-only, not native: the shard's job-kill announcements (their own
	// 0x3667 channel, server wire.JobKillNoticeChannel). On unless turned off
	// (the owner's choice, 2026-10-15); off hides them from the banner and
	// the chat log.
	readonly jobKillNotices: boolean;
	// Port-only, not native: the shard's seal-drop and +8-and-above
	// announcements (their own 0x3667 channel, server
	// wire.DropPlusNoticeChannel). On unless turned off (the owner's
	// choice, 2026-10-15); off hides them from the banner and the chat log.
	readonly dropPlusNotices: boolean;
	// Port-only, not native: the shard's +7 announcements, their own bucket
	// and 0x3667 channel apart from the drop/plus channel, so this row never
	// widens that one. On unless turned off (the owner's choice,
	// 2026-10-19); off hides them from the banner and the chat log.
	readonly plusSevenNotices: boolean;
	// Port-only, not native: the shard's item-break announcements for
	// characters who opted in on the account page (their own 0x3667
	// channel, server wire.BreakNoticeChannel; SRO_ALCHEMY_BREAK_NOTICE,
	// #1738). On unless turned off; off hides them from the banner and
	// the chat log.
	readonly breakNotices: boolean;
	// Port-only, not native: when a unique the player damaged dies, the
	// shard's damage ranking (0x3FBC, server SRO_UNIQUE_DAMAGE_RANK) opens
	// in a window (unique-rank-hud.ts). Off unless turned on, so each
	// player opts in.
	readonly uniqueDamageRank: boolean;
	// Port-only, not native: holding V (UIIT_STT_USER_MOSTER_BLIND) also
	// hides other players' pets and transports. The original hides only
	// monsters and players (8602C0, 85E550): a COS draws through its owner
	// (CICCos_ResolveDrawOwner 854680), which ignores the key. Off unless
	// turned on (the balance advisor's ask, owner-approved 2026-10-15).
	readonly vHidesCompanions: boolean;
	// Port-only, not native: a character the navmesh puts in a dungeon room
	// the camera's room does not draw is hidden and cannot be clicked, as
	// that room's scenery is not drawn, so characters and rooms appear and
	// vanish together. The original draws every entity in its frustum, so
	// mobs show floating in the void behind a culled room (World_PickEntity-
	// AtScreenPoint 692680 likewise raycasts entities only). On unless
	// turned off (the owner's choice, 2026-10-19); off is the original,
	// frustum-only draw and pick.
	readonly dungeonCharacterPvs: boolean;
	// Port-only, not native: an authored label wider than its box wraps onto
	// two lines inside it, and an open list that would leave its window opens
	// upward (the original runs the text past the window's edge and draws the
	// list below the box). On unless turned off (the owner's choice,
	// 2026-10-15); off is the original look.
	readonly fitWindowLabels: boolean;
	// Port-only, not native: an item dropped on the other bag page's tab
	// moves to that page's first free slot (bag-tab-drop.ts). On unless
	// turned off (the owner's choice, 2026-10-15); off is the original,
	// where a drop on a tab does nothing.
	readonly dropOnBagTab: boolean;
	// Port-only, not native: Ctrl+click, right-click or double-click a bag
	// item stages it into the open exchange, as a drop on the table does;
	// the original stages by drag and drop only. On unless turned off (the
	// owner's choice, 2026-10-24); off is the original, drag-only staging.
	readonly exchangeClickShortcuts: boolean;
	// Port-only, not native: in the chat log, a drop or plus notice's player
	// name links to a whisper and its item name shows the item's tooltip
	// (foundation/gameplay/notice-links.ts). On unless turned off (the
	// owner's choice, 2026-10-15); off is plain text.
	readonly noticeLinks: boolean;
	// Port-only, not native: the notice banner shows for 5000 ms with a
	// 1000 ms fade instead of the native 7000 ms and 2000 ms (6B3180, 6B30F0;
	// runtime/ui/hud/unique-banner.ts). On unless turned off (the balance
	// advisor's ask, owner-approved 2026-10-15); off is the original timing.
	readonly shortNotices: boolean;
	// Port-only, not native: the guild window's Withdraw asks "Ban [name]?"
	// (UIIT_MSG_GUILD_QUESTION_EXPULSION_ANYONE) before it bans; 5E4C30 sends
	// the ban on the click. On unless turned off (the owner's choice,
	// 2026-10-15); off is the original.
	readonly confirmGuildBan: boolean;
	// Port-only, not native: straight AT_MOV_1TAR projectiles re-aim at the
	// target's live socket each frame (stepHomingProjectile). The original
	// interpolates between the launch-captured start (+0x58) and end (+0x64)
	// vectors without re-reading the target (CProjectileTrace_Process 8D8580,
	// CProjectile_Bezier_Process 879650). Off unless turned on.
	readonly homingProjectiles: boolean;
	// Port-only, not native (owner 2026-10-24): the summoned trade transport
	// drinks a transport HP potion at the auto-potion HP percentage
	// (transport-auto-potion.ts). The original watches only the player
	// (70C550). Off unless turned on.
	readonly transportAutoPotion: boolean;
	// Port-only, not native (#1448): connect over WebTransport (QUIC/UDP)
	// first and fall back to the WebSocket when the path fails. On unless
	// turned off (#1451 step 3): WebTransport is the
	// default connect and the one bounded attempt remembers a failed QUIC
	// path for the session, so off stays one setting away for anyone whose
	// path cannot carry it. Applies from the next connect.
	readonly webTransport: boolean;
	// Port-only, not native: left-clicking the attack pet's mini panel
	// (CIFPetMiniInfo 6B3460 / 6B3760) or a companion's status portrait
	// (CIFCOSStatus 6A9BC0) selects the companion and toggles its window
	// (CIFCos 69D920). Natively CIFPetMiniInfo::vftable (BFBDAC) inherits
	// CIFWnd_GetMessageMap (53CC30 -> BD2CB8, initialized by BB20D0) and leaves
	// vtable slot 29 at CIFWnd_DefaultLeftButtonDown (4B0740, no-op), and
	// CIFCOSStatus maps no left-click message (6A9BC0). On unless turned off
	// (the owner's choice, 2026-10-18, BUG-086); off is native.
	readonly petClickOpensWindow: boolean;
	// Port-only, not native (0x0077DD61 clears commandMode bit 0x80 via 0x705B
	// selector 2 on 0xB69E pickup error 0xB4/0xB5, stopping gold pickup too):
	// when on, keep bit 0x80 enabled on full-bag errors and pause non-gold item
	// pickup until bag space changes so the pet keeps picking up gold. Default on.
	readonly smartPetPickup: boolean;
	// Port-only, not native (#841): an item filter for the pickup pet. Off
	// unless turned on; off, the pet proposes exactly the native categories
	// and the kind switches below are never read. The filter only ever
	// narrows the client's own proposals above the server-owned category
	// byte, so the pet window, its saved setting and other clients see
	// native behaviour; manual pickup commands are never filtered.
	readonly petPickupFilter: boolean;
	// The filter's kinds (ground-item's pickupItemKind): each is collected
	// unless explicitly turned off, and only while petPickupFilter is on.
	readonly petFilterGold: boolean;
	readonly petFilterEquipment: boolean;
	readonly petFilterAlchemy: boolean;
	readonly petFilterPotions: boolean;
	readonly petFilterOther: boolean;
	// Port-only, not native: a touch tap on an item or skill slot also keeps
	// its tooltip up until the next touch elsewhere or a scroll (#1243); the
	// tap's own action is unchanged. Touch only. On by default (the owner's
	// choice, 2026-10-22); off is the brief hover a tap gives today.
	readonly touchInspect: boolean;
	// Port-only, not native: when the build publishes extra worldmap_mapinfo
	// town pages (Alexandria) and a wider overview box, the M window uses them
	// instead of the five native town pages and 28 x 8 overview (5758D0). Off
	// unless turned on.
	readonly expandedWorldMap: boolean;
	// Port-only, not native (#840): every active quest's NPC target shows on
	// the world map and the minimap, not only the one quest the journal
	// tracks. Off unless turned on; off is the native tracked quest only.
	readonly questTrackerMarkers: boolean;
	// Port-only, not native (#1640): the player's own degree-9-or-higher
	// Seal of Sun drops get a gold name plate and a soft ground ring, so
	// the top seal reads differently the moment it lands. Other players'
	// drops are never marked. Off unless turned on.
	readonly sunDropMarker: boolean;
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

// #1806: the scene's backing-store budget on a phone. An iPhone at device
// pixel ratio 3 drew 2250 x 1206 (2.7 MP) and spiked past 100 ms about 670
// times in 93 s; 1 MP puts that screen at render scale 50.
export const PHONE_SCENE_PIXEL_BUDGET = 1_000_000;
// The longest short screen side, in CSS pixels, a phone has. Tablets start
// at 600 (a small Android tablet) and keep full resolution.
export const PHONE_SHORT_SIDE_MAX = 500;

/*
================
DisplayClass

What the platform reports about the display: whether the primary pointer is
coarse (a finger) and the screen's short side in CSS pixels.
================
*/
export interface DisplayClass {
	readonly coarsePointer: boolean;
	readonly shortSide: number;
}

/*
================
phoneDisplay

A phone is a coarse pointer on a small screen. A touch laptop has a fine
primary pointer and a tablet a longer short side, so both keep the
desktop rule.
================
*/
export function phoneDisplay( display: DisplayClass ): boolean {
	return display.coarsePointer && display.shortSide > 0 && display.shortSide <= PHONE_SHORT_SIDE_MAX;
}

/*
================
sceneRenderScale

The render scale the scene is drawn at this frame. Only the phone cap
changes the saved scale, and only while that scale is the default 100:
the largest scale whose backing store fits the budget, else the smallest.
================
*/
export function sceneRenderScale(
	video: Pick<ExperimentalVideo, "renderScale" | "phoneRenderCap">,
	viewport: { readonly width: number; readonly height: number; },
	phone: boolean
): RenderScale {
	if ( !phone || !video.phoneRenderCap || video.renderScale !== DEFAULT_RENDER_SCALE ) return video.renderScale;
	const scales = renderScales(), pixels = viewport.width * viewport.height;
	for ( const scale of scales ) {
		if ( pixels * scale * scale / 10000 <= PHONE_SCENE_PIXEL_BUDGET ) return scale;
	}
	return scales[scales.length - 1]!;
}

/*
================
UiScale

Port-only, not native. The UI size slider's stops: 100 is the native 1:1
interface, 50 and 200 its ends, in steps of 5.
================
*/
export const UI_SCALE_DEFAULT = 100;
export const UI_SCALE_MIN = 50;
export const UI_SCALE_MAX = 200;
export const UI_SCALE_STEP = 5;

/*
================
uiScaleStop

A UI size as a slider stop: a finite number inside the slider's range
snapped to a step, else undefined, so each caller decides what an invalid
value means (a stored preference restores the native 100; a UI selection
leaves the draft intact).
================
*/
export function uiScaleStop( value: unknown ): number | undefined {
	if ( typeof value !== "number" || !Number.isFinite( value ) ) return undefined;
	if ( value < UI_SCALE_MIN || value > UI_SCALE_MAX ) return undefined;
	return Math.round( value / UI_SCALE_STEP ) * UI_SCALE_STEP;
}

/*
================
uiScaleFactor

The multiple of the native UI size the preference asks the platform's
display scale for. Exactly 1 at the native 100.
================
*/
export function uiScaleFactor( options: ExperimentalOptions ): number {
	return options.uiScale / UI_SCALE_DEFAULT;
}

/*
================
experimentalOptions

Only an explicit true enables a toggle, except smoothCamera, enchantSparks,
compressedTextures, sharedActorRoom, mainThreadOwnWalk, greyDisabledTabs, jobKillNotices, dropPlusNotices,
plusSevenNotices, breakNotices, fitWindowLabels, dropOnBagTab, exchangeClickShortcuts, noticeLinks, shortNotices, confirmGuildBan,
petClickOpensWindow, smartPetPickup, touchInspect, phoneRenderCap, webTransport, eventChatTab and dungeonCharacterPvs, which only an explicit false turns off.
The pickup filter's kinds (petFilterGold and its siblings) also only turn off
explicitly, and are read only while petPickupFilter is on.
Missing or invalid render scales restore full resolution and missing or
invalid UI sizes the native 100; legacy Video
settings cannot enable scaling. Keys of retired rows (terrainRelief and texturedHorizon, removed at the owner's
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
		phoneRenderCap: record.phoneRenderCap !== false,
		uiScale: uiScaleStop( record.uiScale ) ?? UI_SCALE_DEFAULT,
		chatTimestamps: enabled( "chatTimestamps" ),
		eventChatTab: record.eventChatTab !== false,
		safeLoadingScreen: enabled( "safeLoadingScreen" ),
		monsterGuide: enabled( "monsterGuide" ),
		developerDiagnostics: enabled( "developerDiagnostics" ),
		postProcessing: enabled( "postProcessing" ),
		anisotropicFiltering: enabled( "anisotropicFiltering" ),
		heightFog: enabled( "heightFog" ),
		dynamicSun: enabled( "dynamicSun" ),
		floatBloom: enabled( "floatBloom" ),
		hdrToneMap: enabled( "hdrToneMap" ),
		sunShadow: enabled( "sunShadow" ),
		perPixelLighting: enabled( "perPixelLighting" ),
		waterRefraction: enabled( "waterRefraction" ),
		predictedRunUp: record.predictedRunUp !== false,
		smoothCamera: record.smoothCamera !== false,
		enchantSparks: record.enchantSparks !== false,
		compressedTextures: record.compressedTextures !== false,
		sharedActorRoom: record.sharedActorRoom !== false,
		mainThreadOwnWalk: record.mainThreadOwnWalk !== false,
		greyDisabledTabs: record.greyDisabledTabs !== false,
		jobKillNotices: record.jobKillNotices !== false,
		dropPlusNotices: record.dropPlusNotices !== false,
		plusSevenNotices: record.plusSevenNotices !== false,
		breakNotices: record.breakNotices !== false,
		uniqueDamageRank: enabled( "uniqueDamageRank" ),
		vHidesCompanions: enabled( "vHidesCompanions" ),
		dungeonCharacterPvs: record.dungeonCharacterPvs !== false,
		fitWindowLabels: record.fitWindowLabels !== false,
		dropOnBagTab: record.dropOnBagTab !== false,
		exchangeClickShortcuts: record.exchangeClickShortcuts !== false,
		noticeLinks: record.noticeLinks !== false,
		shortNotices: record.shortNotices !== false,
		attackPetTraining: enabled( "attackPetTraining" ),
		confirmGuildBan: record.confirmGuildBan !== false,
		homingProjectiles: enabled( "homingProjectiles" ),
		transportAutoPotion: enabled( "transportAutoPotion" ),
		webTransport: record.webTransport !== false,
		petClickOpensWindow: record.petClickOpensWindow !== false,
		smartPetPickup: record.smartPetPickup !== false,
		petPickupFilter: enabled( "petPickupFilter" ),
		petFilterGold: record.petFilterGold !== false,
		petFilterEquipment: record.petFilterEquipment !== false,
		petFilterAlchemy: record.petFilterAlchemy !== false,
		petFilterPotions: record.petFilterPotions !== false,
		petFilterOther: record.petFilterOther !== false,
		touchInspect: record.touchInspect !== false,
		expandedWorldMap: enabled( "expandedWorldMap" ),
		questTrackerMarkers: enabled( "questTrackerMarkers" ),
		sunDropMarker: enabled( "sunDropMarker" )
	};
}

// Where the preferences are kept (the platform saves them). The key's
// version bumped with the WebTransport default flip (#1451 step 3): a
// record saved under the previous key carries the old explicit default
// for that row, so it migrates on read without its webTransport value
// and the new default reaches every saved profile. A choice stored
// under the current key is kept as written.
export const EXPERIMENTAL_STORAGE_KEY = "sro:v1150:experimental-options:2";
export const PREVIOUS_EXPERIMENTAL_STORAGE_KEY = "sro:v1150:experimental-options:1";

/*
================
storedExperimentalOptions

The saved preferences from storage, or the defaults when there are none or
they cannot be read. A record found only under the previous key migrates
without its webTransport value (see the key above); the platform's next
save writes the current key and retires the old one. The platform owns
saving them; an owner that must act before the platform starts (the asset
worker's texture variant) reads them here. Throws only for a stored value
that is not JSON, which the platform reports.
================
*/
export function storedExperimentalOptions( storage: Pick<Storage, "getItem"> | undefined ): {
	readonly options: ExperimentalOptions;
	readonly raw: unknown;
} {
	const stored = storage?.getItem( EXPERIMENTAL_STORAGE_KEY ) ?? null;
	if ( stored !== null ) {
		const raw: unknown = JSON.parse( stored );
		return { options: experimentalOptions( raw ), raw };
	}
	const previous = storage?.getItem( PREVIOUS_EXPERIMENTAL_STORAGE_KEY ) ?? null;
	if ( previous === null ) {
		return { options: experimentalOptions(), raw: null };
	}
	const parsed: unknown = JSON.parse( previous );
	if ( typeof parsed !== "object" || parsed === null || Array.isArray( parsed ) ) {
		return { options: experimentalOptions( parsed ), raw: parsed };
	}
	const raw: Record<string, unknown> = { ...(parsed as Record<string, unknown>) };
	delete raw.webTransport;
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
	readonly phoneRenderCap: boolean;
	readonly postProcessing: boolean;
	readonly anisotropicFiltering: boolean;
	readonly heightFog: boolean;
	readonly dynamicSun: boolean;
	readonly floatBloom: boolean;
	readonly hdrToneMap: boolean;
	readonly sunShadow: boolean;
	readonly perPixelLighting: boolean;
	readonly waterRefraction: boolean;
	readonly smoothCamera: boolean;
	readonly enchantSparks: boolean;
	readonly sharedActorRoom: boolean;
	readonly vHidesCompanions: boolean;
	readonly dungeonCharacterPvs: boolean;
}

/*
================
experimentalVideo
================
*/
export function experimentalVideo( options: ExperimentalOptions ): ExperimentalVideo {
	return {
		renderScale: options.renderScale,
		phoneRenderCap: options.phoneRenderCap,
		postProcessing: options.postProcessing,
		anisotropicFiltering: options.anisotropicFiltering,
		heightFog: options.heightFog,
		dynamicSun: options.dynamicSun,
		floatBloom: options.floatBloom,
		hdrToneMap: options.hdrToneMap,
		sunShadow: options.sunShadow,
		perPixelLighting: options.perPixelLighting,
		waterRefraction: options.waterRefraction,
		smoothCamera: options.smoothCamera,
		enchantSparks: options.enchantSparks,
		sharedActorRoom: options.sharedActorRoom,
		vHidesCompanions: options.vHidesCompanions,
		dungeonCharacterPvs: options.dungeonCharacterPvs
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
	readonly plusSevenNotices: boolean;
	readonly breakNotices: boolean;
	readonly webTransport: boolean;
	readonly homingProjectiles?: boolean;
	readonly smartPetPickup?: boolean;
	readonly sunDropMarker?: boolean;
	// The pickup pet filter's kind mask (ground-item's PICKUP_KIND bits);
	// undefined while the filter row is off, which is the native loop.
	readonly petPickupKinds?: number;
}

/*
================
experimentalGameplay
================
*/
export function experimentalGameplay( options: ExperimentalOptions ): ExperimentalGameplay {
	return {
		predictedRunUp: options.predictedRunUp,
		webTransport: options.webTransport,
		jobKillNotices: options.jobKillNotices,
		dropPlusNotices: options.dropPlusNotices,
		plusSevenNotices: options.plusSevenNotices,
		breakNotices: options.breakNotices,
		homingProjectiles: options.homingProjectiles,
		smartPetPickup: options.smartPetPickup,
		sunDropMarker: options.sunDropMarker,
		petPickupKinds: options.petPickupFilter ?
			(options.petFilterGold ? PICKUP_KIND_GOLD : 0) |
			(options.petFilterEquipment ? PICKUP_KIND_EQUIPMENT : 0) |
			(options.petFilterAlchemy ? PICKUP_KIND_ALCHEMY : 0) |
			(options.petFilterPotions ? PICKUP_KIND_POTIONS : 0) |
			(options.petFilterOther ? PICKUP_KIND_OTHER : 0) :
			undefined
	};
}
