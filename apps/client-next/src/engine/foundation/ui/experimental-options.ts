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
	// Port-only, not native (#2094): screen-space ambient occlusion. A
	// half-resolution depth pass totals how much of each surface's
	// surroundings the previous frame's depth hides, and the next
	// frame's surfaces scale their ambient term by it; the diffuse
	// keeps its direction and the unlit path is untouched. Off by
	// default; off owns nothing and the frame is the native one.
	readonly ambientOcclusion: boolean;
	// Port-only, not native: drifting procedural clouds, the sun's
	// atmospheric glow and a rim light against the sky (pipelines.ts's
	// stages.w). Off by default; off is the native gradient and disc.
	readonly skyDetail: boolean;
	// Port-only, not native: the scene renders through a 4x multisampled
	// colour and depth buffer and resolves into the frame (the original
	// presents single-sampled). Off by default; off is byte-identical to
	// the native pass order.
	readonly msaa: boolean;
	// Port-only, not native: FXAA 3.11 on the scene alone, before the
	// world labels and the HUD draw, so text is never resampled. The
	// scene renders into its own full-size colour and resolves into the
	// frame through the FXAA pass. Off by default; off is the native frame.
	readonly sceneFxaa: boolean;
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
	// Port-only, not native: the Actions window gains Auto Select (action 1005,
	// select-nearest.ts). Native holds the code but no row or key reaches it.
	readonly autoSelect: boolean;
	// Port-only, not native: a device without BC is served re-encoded
	// textures (texture-variants.ts) instead of decoding them to RGBA.
	// Read when the asset worker starts, so it applies from the next load.
	// On unless turned off (the owner's choice, 2026-10-14): only devices
	// without BC are affected, where RGBA ends the tab; off is the native
	// texture content.
	readonly compressedTextures: boolean;
	// Port-only, not native (#2061): the HD texture set, the asset build's
	// x2 AI-upscaled world object textures served from their own /hd
	// subtree behind a separate pack index. Off unless turned on (the
	// owner's choice, 2026-11-07): off, the asset worker fetches none of
	// the set, not even its index; on, the index is fetched and the set
	// downloads with progress, and the retail texture stays the fallback
	// for every path the set does not hold.
	readonly hdTextures: boolean;
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
	// Port-only, not native: the shard's item-break announcements for
	// characters who opted in on the account page (their own 0x3667
	// channel, server wire.BreakNoticeChannel; SRO_ALCHEMY_BREAK_NOTICE,
	// #1738). On unless turned off; off hides them from the banner and
	// the chat log.
	readonly breakNotices: boolean;
	// Port-only, not native (#2727): shard event announcements (the
	// shardEvent notices) print in red in the bottom-right message log.
	// The native box colours only category 6 light blue and every other
	// category the default beige (0x67A600), so the highlight is the
	// client's own. On unless turned off (the owner's ask, 2026-11-06);
	// off is the native beige line.
	readonly highlightEventNotices: boolean;
	// Port-only, not native: when a unique the player damaged dies, the
	// shard's damage ranking (0x3FBC, server SRO_UNIQUE_DAMAGE_RANK) opens
	// in a window (unique-rank-hud.ts). Off unless turned on, so each
	// player opts in.
	readonly uniqueDamageRank: boolean;
	// Port-only, not native: the Unique Status window (unique-status-hud.ts)
	// lists every unique's alive/dead state and last kill from the shard's
	// feed (0x3FC4/0x3FC5, server SRO_UNIQUE_STATUS), and alerts - in game,
	// and in the browser once allowed - when a marked unique spawns or
	// dies. Off unless turned on, so each player opts in.
	readonly uniqueStatus: boolean;
	// Port-only, not native: the Consignment Market (market-hud.ts) sells
	// while the seller is offline (server SRO_CONSIGNMENT). The row adds
	// its System menu entry; the window itself also opens from its button
	// beside FPS, the K key and a storage keeper, all of which the shard's
	// own switch already gates. Off unless turned on, so each player opts in.
	readonly consignmentMarket: boolean;
	// Port-only, not native: holding V (UIIT_STT_USER_MOSTER_BLIND) also
	// hides other players' pets and transports. The original hides only
	// monsters and players (8602C0, 85E550): a COS draws through its owner
	// (CICCos_ResolveDrawOwner 854680), which ignores the key. Off unless
	// turned on (the balance advisor's ask, owner-approved 2026-10-15).
	readonly vHidesCompanions: boolean;
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
	// Port-only, not native: Alt+click on an item while typing a chat line
	// links it, and a linked item's name in the chat log shows its tooltip
	// (foundation/gameplay/chat-item-links.ts; the server's
	// SRO_CHAT_ITEM_LINKS must be on too). On unless turned off (the
	// owner's choice, 2026-11-07); off is plain text and no gesture.
	readonly chatItemLinks: boolean;
	// Port-only, not native: the notice banner shows for 5000 ms with a
	// 1000 ms fade instead of the native 7000 ms and 2000 ms (6B3180, 6B30F0;
	// runtime/ui/hud/unique-banner.ts). On unless turned off (the balance
	// advisor's ask, owner-approved 2026-10-15); off is the original timing.
	readonly shortNotices: boolean;
	// Port-only, not native: straight AT_MOV_1TAR projectiles re-aim at the
	// target's live socket each frame (stepHomingProjectile). The original
	// interpolates between the launch-captured start (+0x58) and end (+0x64)
	// vectors without re-reading the target (CProjectileTrace_Process 8D8580,
	// CProjectile_Bezier_Process 879650). Off unless turned on.
	readonly homingProjectiles: boolean;
	// Port-only, not native (#4527): the auto-reload that empties the
	// quiver takes the smallest matching bag stack of bolts or arrows
	// instead of the native lowest bag slot (ties still go to the lower
	// slot). The choice rides a one-byte control frame to the server
	// (ammunitionpreference.go) and is read per shot; off is the
	// original's lowest-slot reload.
	readonly smallestAmmoStack: boolean;
	// Port-only, not native (#4740): a skill the server queued behind the
	// running cast (0xB2CD arm count 2) starts its wind-up when the client
	// estimates the server ended that cast, one delivery before the 0xB245
	// that natively starts it. Off by default until the owner decides
	// (2026-11-28); off is the native start on 0xB245.
	readonly predictedQueuedCast: boolean;
	// Port-only, not native (owner 2026-10-24, extended #2741): companion
	// and vigor auto-potions (transport-auto-potion.ts, companion-potions.ts).
	// The original watches only the player (70C550). On unless turned off
	// (the owner's choice, 2026-11-07, #2741: an explicit exception to the
	// off-by-default rule); off is the native player-only recovery.
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
	// Port-only, not native (#840): every active quest's NPC target shows on
	// the world map and the minimap, not only the one quest the journal
	// tracks. Off unless turned on; off is the native tracked quest only.
	readonly questTrackerMarkers: boolean;
	// Port-only, not native (#3394): a monster picked in the M map guide's
	// Monster tab also marks its hunting ground on the minimap (the edge
	// arrow while the point is off the ring) until another row is picked.
	// The later client only tints the ground on the M map. Off unless
	// turned on; off, the minimap is unchanged.
	readonly guideWaypoint: boolean;
	// Port-only, not native: the world map (M, never the minimap) offers a
	// "Unique spawns" box at its foot, off until ticked; with both on it
	// draws one unique sign on every nest a unique may spawn from, all
	// candidates of a unique together (foundation/ui/unique-nests.ts). A
	// place only: no hover, no alive or dead state, no timer. The row is on
	// by default (the owner's choice, 2026-11-07), the box off (2026-11-13);
	// row off is the native map.
	readonly uniqueSpawnPoints: boolean;
	// Port-only, not native (#1640): the player's own degree-9-or-higher
	// Seal of Sun drops get a gold name plate and a soft ground ring, so
	// the top seal reads differently the moment it lands. Other players'
	// drops are never marked. Off unless turned on.
	readonly sunDropMarker: boolean;
	// Port-only, not native: the portal-out cue (SND_WARP, gatein.wav) when
	// the world arrives. The native row has no traced caller - the gate at
	// 0x68eafd in 0x68e530 never reaches it on the traced path - so the
	// native arrival is silent. Off unless turned on.
	readonly warpArrivalSound: boolean;
	// Port-only, not native: the four Christmas carols (event_carol_01..04,
	// music mode 1) while the Christmas event group (1) is on. The native
	// carol branch (8F7A43) is reached only through the music manager's
	// SetMusicMode (8FC550, vtable slot 0x58), which no v1.150 code calls,
	// so the native event plays the region music. Off unless turned on.
	readonly christmasCarols: boolean;
	// Port-only, not native (#2496): a rare item's seal line sits under the
	// item name. 558FF0 appends it after the requirement rows (the 56B099
	// call in FormatWeapon, after 568500). Off unless turned on; off is the
	// native place.
	readonly sealUnderHeading: boolean;
	// Port-only, not native: an equipment tooltip lists its set's tiers and how many pieces are worn,
	// on a shard that applies set bonuses (SRO_CAP110_SET_BONUSES). Off unless turned on; the
	// native client has no set rows.
	readonly setBonusTooltip: boolean;
	// Port-only, not native (#2269): the Item Mall's Buying list (native
	// CIFSilkBuyList), listing recorded silk uses by period. The native
	// window is never instantiated, its Inquire handler is an empty stub
	// (0x6CE6F0) and its mall button is born disabled (0x6BC17E). Off
	// unless turned on; off is the original.
	readonly silkHistory: boolean;
	// Port-only, not native (#3257): the later mall's category rail
	// (Expendables, Avatar, Pet, Premium, Alchemy, HOT shop), its
	// sub-category tabs, item search and the N/H/R badges. The server
	// sides it with SRO_MALL_CATEGORIES; a client with the row off keeps
	// the shipped mall exactly. Off unless turned on.
	readonly mallCategories: boolean;
	// Port-only, not native (#3907): the v1.696 SRO Macro (CNIFMacroMenuWnd
	// and its option windows), which v1.150 does not have. The server sides
	// it with SRO_AUTO_MACRO; a client with the row off never opens the
	// windows and never sends 0x7554. On unless turned off (owner,
	// 2026-11-21: the macro ships on in the next release).
	readonly sroMacro: boolean;
	// Port-only, not native (#3532): the Event Calendar window over the
	// public schedule feed (#3520). Off unless turned on; off, the System
	// menu has no calendar entry and the window never opens.
	readonly eventCalendar: boolean;
	// Port-only, not native (#3530): the Job Honor Rank window over the
	// honor feed. Off unless turned on; off, the System menu has no
	// honor entry and the window never opens. The server sides it with
	// SRO_JOB_HONOR; with that off nothing is ever sent.
	readonly jobHonor: boolean;
	// Port-only, not native (#3925): the login and server-list screens
	// show the running maintenance from the public schedule feed (#3520)
	// as one notice line. Off by default; off, the title never reads the
	// feed and shows the native screens exactly.
	readonly maintenanceNotice: boolean;
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
browserDisplayClass

What this window reports about its display, read once per call: the primary
pointer's coarseness and the screen's short side in CSS pixels. This is the
one reader of the pointer class - the renderer and the UI runtime both call
it. A host without matchMedia reads as a fine pointer, so layout keeps the
desktop rule; a test that wants the touch layout stubs a coarse matchMedia
before the UI is created, instead of relying on a production default.
================
*/
export function browserDisplayClass(): DisplayClass {
	const query = globalThis.matchMedia?.( "(pointer: coarse)" ),
		screen = globalThis.screen,
		width = screen?.width ?? 0,
		height = screen?.height ?? 0;
	return { coarsePointer: query ? query.matches : false, shortSide: Math.min( width, height ) };
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
compressedTextures, jobKillNotices, dropPlusNotices,
breakNotices, highlightEventNotices, exchangeClickShortcuts, noticeLinks, chatItemLinks, shortNotices,
petClickOpensWindow, touchInspect, uniqueSpawnPoints, phoneRenderCap, webTransport, eventChatTab, transportAutoPotion,
christmasCarols and sroMacro (owners, 2026-11-15 and 2026-11-21: on by default, off by the player's choice), which only an explicit false turns off.
The pickup filter's kinds (petFilterGold and its siblings) also only turn off
explicitly, and are read only while petPickupFilter is on.
Missing or invalid render scales restore full resolution and missing or
invalid UI sizes the native 100; legacy Video
settings cannot enable scaling. Keys of retired rows (terrainRelief and texturedHorizon, removed at the owner's
request on 2026-10-11; plusSevenNotices, retired with the +7 notices in #3681)
are never read, so a saved preference holding them loads cleanly and every
other row keeps its saved choice.
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
		ambientOcclusion: enabled( "ambientOcclusion" ),
		skyDetail: enabled( "skyDetail" ),
		msaa: enabled( "msaa" ),
		sceneFxaa: enabled( "sceneFxaa" ),
		predictedRunUp: record.predictedRunUp !== false,
		smoothCamera: record.smoothCamera !== false,
		enchantSparks: record.enchantSparks !== false,
		compressedTextures: record.compressedTextures !== false,
		hdTextures: enabled( "hdTextures" ),
		jobKillNotices: record.jobKillNotices !== false,
		dropPlusNotices: record.dropPlusNotices !== false,
		breakNotices: record.breakNotices !== false,
		highlightEventNotices: record.highlightEventNotices !== false,
		uniqueDamageRank: enabled( "uniqueDamageRank" ),
		uniqueStatus: enabled( "uniqueStatus" ),
		consignmentMarket: enabled( "consignmentMarket" ),
		vHidesCompanions: enabled( "vHidesCompanions" ),
		exchangeClickShortcuts: record.exchangeClickShortcuts !== false,
		noticeLinks: record.noticeLinks !== false,
		chatItemLinks: record.chatItemLinks !== false,
		shortNotices: record.shortNotices !== false,
		autoSelect: enabled( "autoSelect" ),
		homingProjectiles: enabled( "homingProjectiles" ),
		smallestAmmoStack: enabled( "smallestAmmoStack" ),
		predictedQueuedCast: enabled( "predictedQueuedCast" ),
		transportAutoPotion: record.transportAutoPotion !== false,
		webTransport: record.webTransport !== false,
		petClickOpensWindow: record.petClickOpensWindow !== false,
		petPickupFilter: enabled( "petPickupFilter" ),
		petFilterGold: record.petFilterGold !== false,
		petFilterEquipment: record.petFilterEquipment !== false,
		petFilterAlchemy: record.petFilterAlchemy !== false,
		petFilterPotions: record.petFilterPotions !== false,
		petFilterOther: record.petFilterOther !== false,
		touchInspect: record.touchInspect !== false,
		questTrackerMarkers: enabled( "questTrackerMarkers" ),
		guideWaypoint: enabled( "guideWaypoint" ),
		uniqueSpawnPoints: record.uniqueSpawnPoints !== false,
		sunDropMarker: enabled( "sunDropMarker" ),
		warpArrivalSound: enabled( "warpArrivalSound" ),
		christmasCarols: record.christmasCarols !== false,
		sealUnderHeading: enabled( "sealUnderHeading" ),
		setBonusTooltip: enabled( "setBonusTooltip" ),
		silkHistory: enabled( "silkHistory" ),
		mallCategories: enabled( "mallCategories" ),
		sroMacro: record.sroMacro !== false,
		eventCalendar: enabled( "eventCalendar" ),
		jobHonor: enabled( "jobHonor" ),
		maintenanceNotice: enabled( "maintenanceNotice" )
	};
}

// Where the preferences are kept (the platform saves them). The key's
// version bumped with the WebTransport default flip (#1451 step 3): a
// record saved under the previous key carries the old explicit default
// for that row, so it migrates on read without its webTransport value
// and the new default reaches every saved profile. It bumped again with
// the SRO Macro default flip (owner, 2026-11-21): a record saved under
// the :2 key migrates without its sroMacro value, and one under the
// oldest (:1) key without its webTransport and sroMacro values. A
// choice stored under the current key is kept as written.
export const EXPERIMENTAL_STORAGE_KEY = "sro:v1150:experimental-options:3";
export const PREVIOUS_EXPERIMENTAL_STORAGE_KEY = "sro:v1150:experimental-options:2";
export const FIRST_EXPERIMENTAL_STORAGE_KEY = "sro:v1150:experimental-options:1";

/*
================
migratedExperimentalOptions

A record saved under an older key, read with the flipped rows' stored
values dropped (see the keys above), or undefined when that key holds
nothing. A stored value that is not a JSON object loads as itself, so
every row falls back to its rule for an invalid record.
================
*/
function migratedExperimentalOptions(
	storage: Pick<Storage, "getItem"> | undefined,
	key: string,
	flippedRows: readonly string[]
): { readonly options: ExperimentalOptions; readonly raw: unknown; } | undefined {
	const stored = storage?.getItem( key ) ?? null;
	if ( stored === null ) {
		return undefined;
	}
	const parsed: unknown = JSON.parse( stored );
	if ( typeof parsed !== "object" || parsed === null || Array.isArray( parsed ) ) {
		return { options: experimentalOptions( parsed ), raw: parsed };
	}
	const raw: Record<string, unknown> = { ...(parsed as Record<string, unknown>) };
	for ( const row of flippedRows ) {
		delete raw[row];
	}
	return { options: experimentalOptions( raw ), raw };
}

/*
================
storedExperimentalOptions

The saved preferences from storage, or the defaults when there are none or
they cannot be read. A record found only under an older key migrates
without the flipped rows' values (see the keys above); the platform's next
save writes the current key and retires the old ones. The platform owns
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
	const previous = migratedExperimentalOptions( storage, PREVIOUS_EXPERIMENTAL_STORAGE_KEY, [ "sroMacro" ] );
	if ( previous !== undefined ) {
		return previous;
	}
	const first = migratedExperimentalOptions(
		storage,
		FIRST_EXPERIMENTAL_STORAGE_KEY,
		[ "webTransport", "sroMacro" ]
	);
	if ( first !== undefined ) {
		return first;
	}
	return { options: experimentalOptions(), raw: null };
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
	readonly ambientOcclusion: boolean;
	readonly skyDetail: boolean;
	readonly msaa: boolean;
	readonly sceneFxaa: boolean;
	readonly smoothCamera: boolean;
	readonly enchantSparks: boolean;
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
		phoneRenderCap: options.phoneRenderCap,
		postProcessing: options.postProcessing,
		anisotropicFiltering: options.anisotropicFiltering,
		heightFog: options.heightFog,
		dynamicSun: options.dynamicSun,
		floatBloom: options.floatBloom,
		hdrToneMap: options.hdrToneMap,
		sunShadow: options.sunShadow,
		perPixelLighting: options.perPixelLighting,
		ambientOcclusion: options.ambientOcclusion,
		skyDetail: options.skyDetail,
		msaa: options.msaa,
		sceneFxaa: options.sceneFxaa,
		smoothCamera: options.smoothCamera,
		enchantSparks: options.enchantSparks,
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
	readonly breakNotices: boolean;
	readonly highlightEventNotices?: boolean;
	readonly webTransport: boolean;
	readonly homingProjectiles?: boolean;
	// Port-only (#4527): the server reloads the quiver from the
	// smallest matching bag stack while on; off (or unset) is native.
	readonly smallestAmmoStack?: boolean;
	// Port-only (#4740): predict a server-queued cast; off (or unset) is native.
	readonly predictedQueuedCast?: boolean;
	readonly sunDropMarker?: boolean;
	readonly warpArrivalSound?: boolean;
	readonly christmasCarols?: boolean;
	// Port-only (#3907): the SRO Macro's automation runs only while on.
	readonly sroMacro?: boolean;
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
		breakNotices: options.breakNotices,
		highlightEventNotices: options.highlightEventNotices,
		homingProjectiles: options.homingProjectiles,
		smallestAmmoStack: options.smallestAmmoStack,
		predictedQueuedCast: options.predictedQueuedCast,
		sunDropMarker: options.sunDropMarker,
		warpArrivalSound: options.warpArrivalSound,
		christmasCarols: options.christmasCarols,
		sroMacro: options.sroMacro,
		petPickupKinds: options.petPickupFilter ?
			(options.petFilterGold ? PICKUP_KIND_GOLD : 0) |
			(options.petFilterEquipment ? PICKUP_KIND_EQUIPMENT : 0) |
			(options.petFilterAlchemy ? PICKUP_KIND_ALCHEMY : 0) |
			(options.petFilterPotions ? PICKUP_KIND_POTIONS : 0) |
			(options.petFilterOther ? PICKUP_KIND_OTHER : 0) :
			undefined
	};
}
