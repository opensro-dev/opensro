/*
===========================================================================

experimental-off.mjs - the all-default Experimental options fixture

The one frozen record every Experimental test compares against: each
option at the value a fresh profile computes (defaults on where the
owner chose them, everything else off). Shared by the options-semantics
tests and the window tests, which both deep-compare against it.

===========================================================================
*/
export const OFF = Object.freeze( {
	renderScale: 100,
	// On by default (owner 2026-10-27, #1806): a phone caps the scene's pixels;
	// every other display draws at full resolution.
	phoneRenderCap: true,
	// Native 1:1 interface size.
	uiScale: 100,
	chatTimestamps: false,
	// On by default (owner 2026-10-28, #1976): event notices get their
	// own chat tab; an explicit false restores the native four tabs.
	eventChatTab: true,
	safeLoadingScreen: false,
	monsterGuide: false,
	developerDiagnostics: false,
	postProcessing: false,
	anisotropicFiltering: false,
	heightFog: false,
	dynamicSun: false,
	floatBloom: false,
	hdrToneMap: false,
	sunShadow: false,
	perPixelLighting: false,
	// Off by default: ambient occlusion is a port-only stage (#2094).
	ambientOcclusion: false,
	// Off by default: sky detail is a port-only deviation.
	skyDetail: false,
	// Off by default: water refraction is a port-only deviation.
	waterRefraction: false,
	// On by default (owner 2026-10-25): a press walks at once and stops
	// where the server casts.
	predictedRunUp: true,
	// On by default (owner 2026-10-15): the camera eases its height and release.
	smoothCamera: true,
	// On by default (owner 2026-10-17): +8 and higher weapons and shields wear enchant sparks.
	enchantSparks: true,
	// On by default (owner 2026-10-14); it changes only devices without BC.
	compressedTextures: true,
	// Off by default (owner 2026-11-07, #2061): off fetches none of the
	// HD set, not even its index.
	hdTextures: false,
	// On by default (owner 2026-10-14): near effects outrank far characters.
	sharedActorRoom: true,
	// On by default (owner 2026-10-25): the own walk is stepped on the main thread.
	mainThreadOwnWalk: true,
	// On by default (owner 2026-10-15): a disabled tab is dimmed.
	greyDisabledTabs: true,
	// On by default (owner 2026-10-15): the shard's job-kill notices show.
	jobKillNotices: true,
	// On by default (owner 2026-10-15): the shard's drop and plus notices show.
	dropPlusNotices: true,
	// On by default (owner 2026-10-19): the shard's +7 notices show.
	plusSevenNotices: true,
	// On by default: the shard's item break notices show (#1738).
	breakNotices: true,
	// On by default (owner ask 2026-11-06, #2727): shard event notices print red in the message log.
	highlightEventNotices: true,
	// Off by default: each player opts in to the unique damage ranking.
	uniqueDamageRank: false,
	// Off by default: V hides only monsters and players, as the original does.
	vHidesCompanions: false,
	// On by default (port-only, not native): hides characters in dungeon blocks the active block does not draw.
	dungeonCharacterPvs: true,
	// On by default (owner 2026-10-15): labels and lists stay inside their window.
	fitWindowLabels: true,
	// On by default (owner 2026-10-15): a drop on a bag tab moves the item there.
	dropOnBagTab: true,
	// On by default (owner 2026-10-24): click shortcuts stage into an exchange.
	exchangeClickShortcuts: true,
	// On by default (owner 2026-10-15): a notice's player and item are links.
	noticeLinks: true,
	// On by default (owner 2026-11-07): Alt+click links an item into chat.
	chatItemLinks: true,
	// On by default (owner 2026-10-15): the notice banner shows for 5 s.
	shortNotices: true,
	autoSelect: false,
	// On by default (owner 2026-10-15): Withdraw asks before a guild ban.
	confirmGuildBan: true,
	// Off by default: straight projectiles keep their launch-captured endpoint (8D8580).
	homingProjectiles: false,
	// On by default (owner 2026-11-07, #2741): companion and vigor auto-potions;
	// off, the automatic potion watches only the player (70C550).
	transportAutoPotion: true,
	// On by default (owner 2026-10-18, BUG-086): clicking CIFPetMiniInfo opens CIFCos;
	// off restores the native no-op click (6B3760).
	petClickOpensWindow: true,
	// On by default (owner 2026-10-20): keep bit 0x80 enabled on 0xB4/0xB5 so the
	// pet keeps picking up gold when full; off restores the native 0x0077DD61 disable.
	smartPetPickup: true,
	// Off by default (#841): the pickup pet proposes the native categories
	// until the item filter is turned on.
	petPickupFilter: false,
	// The filter's kinds include everything unless one is explicitly off;
	// they are read only while the filter itself is on.
	petFilterGold: true,
	petFilterEquipment: true,
	petFilterAlchemy: true,
	petFilterPotions: true,
	petFilterOther: true,
	// On by default (owner 2026-10-22, #1243): a touch tap keeps an item or skill tooltip up.
	touchInspect: true,
	// On by default (#1451 step 3): WebTransport is the
	// default connect and the WebSocket the fallback; off is one setting.
	webTransport: true,
	// Off by default: the M window shows only the five native towns and 28 x 8 overview.
	expandedWorldMap: false,
	// Off by default (#840): the maps mark the tracked quest's targets only.
	questTrackerMarkers: false,
	// Off by default (#1640): only the native plate and no ground ring.
	sunDropMarker: false,
	// Off by default: port-only portal-out cue on arrival; the native
	// gate has no traced caller, so silence is the native default.
	warpArrivalSound: false,
	// Off by default (#2496): the seal line under the item name instead of
	// after the requirement rows (558FF0 at 56B099).
	sealUnderHeading: false,
	// Off by default (#2269): the Item Mall's Buying list; the native
	// window is inert and its mall button is born disabled.
	silkHistory: false
} );
