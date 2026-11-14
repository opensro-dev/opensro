/*
===========================================================================

world-map.ts - world map navigation and presentation

Owns map placement, page, follow mode and the optional hunting guide. The UI
calls its explicit input, frame and shutdown entry points; map details never
become additional state in createUi. Native marker order stays here beside
the map projection. Hunting guidance and the unique nest signs are
port-only, not native.

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import type { Pose } from "@/engine/contracts/gameplay";
import type { UiControl, UiEvent, UiQuad, UiRect, UiView } from "@/engine/contracts/ui";
import type { AuthoredControl, AuthoredLayout } from "@/engine/foundation/ui/authored-layout";
import type { GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import type { MapLabel, MapIcon, WorldMapLayout } from "@/engine/foundation/ui/world-map";
import type { minimapNpcPositions } from "@/engine/foundation/ui/minimap-catalog";
import type { ExperimentalOptions } from "@/engine/foundation/ui/experimental-options";
import { featuresAt } from "@/engine/foundation/world/plane";
import {
	type CaveMapCatalog,
	caveDungeonFloors,
	caveMapImage,
	caveMapPage,
	caveMapView,
	cavePageLocal
} from "@/engine/foundation/ui/cave-map";
import { panelCloseId } from "@/engine/foundation/ui/panels";
import { minimapSameFloor } from "@/engine/foundation/ui/minimap-floor";
import { questMarkerTargets, rosterPositions } from "@/engine/foundation/ui/minimap-markers";
import {
	worldMapDemand,
	expandedWorldMapOn,
	worldMapDungeonOf,
	MAP_LOCAL_MARKER,
	worldMapDungeonPose,
	worldMapFrame,
	worldMapPageAt,
	worldMapPages,
	worldMapPresentation,
	worldMapPointMarkers,
	type MapMarker,
	mapLabelVisible
} from "@/engine/foundation/ui/world-map";
import { guideOverlay, guideRace } from "@/engine/foundation/ui/worldmap-guide";
import { NATIVE_LEVEL_CAP } from "@/engine/foundation/gameplay/level-cap";
import { HUNTING_AREA_PREFIX, HUNTING_MONSTER_SIGN } from "@/engine/foundation/ui/hunting-guide";
import { UNIQUE_NEST_SIGN } from "@/engine/foundation/ui/unique-nests";
import { createHuntingGuideHud } from "./hunting-guide";
import { createUniqueNestsHud } from "./unique-nests";
import { reverseMapButtons } from "./reverse-map-buttons";
import { cavePageHits, cavePageImages, mapSignName } from "./map-cave-page";
import {
	MAP_WINDOW_EXTENT,
	MAP_WINDOW_HEIGHT,
	MAP_WINDOW_WIDTH,
	createMapToolsHud,
	mapLeftReach,
	mapPanel
} from "./map-tools";

// The M window is the v1.188 client's (worldmapguide.2dt, map-chrome.ts):
// 650 x 422 with its foot frame reaching 34 below.
const WORLD_MAP_WIDTH = MAP_WINDOW_WIDTH, WORLD_MAP_HEIGHT = MAP_WINDOW_HEIGHT;
const MAP_FOOT_DEPTH = MAP_WINDOW_EXTENT - MAP_WINDOW_HEIGHT;
// Port-only, not native (screens below the native 800x600 minimum): the map
// window shrinks to the screen, less this margin per side, and keeps the map
// at native scale for panning; the generic window fit would otherwise draw
// the whole window, labels included, at a fraction of their size.
const COMPACT_MAP_MARGIN = 4;
const ROOT = "/assets/images/Media_extracted/";
// 57CE80's roster passes (apprenticeship 57D2FC, party 57D7D8) test the
// pointer the map stores on every move (57FB9E, +0x7C8/+0x7CC) against each
// sign inset by 2.0 (C46208), then draw that member's name above the
// sign's centre (mapSignName, map-cave-page.ts).
const MAP_ROSTER_PREFIX = "map-roster:";
const MAP_ROSTER_HOVER_INSET = 2;

/*
================
mapRosterId
================
*/
function mapRosterId( row: { readonly kind: string; readonly name: string; } ): string {
	return MAP_ROSTER_PREFIX + row.kind + ":" + row.name;
}

/*
================
MapHudOptions
================
*/
interface MapHudOptions {
	assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">;
	base: string;
	experimental: { state(): { saved: ExperimentalOptions; }; restore( value: ExperimentalOptions ): void; };
	saveExperimental?: ( value: ExperimentalOptions ) => void;
}

/*
================
MapPaint

The existing UI drawing primitives and frame inputs, borrowed for this call.
No callbacks or frame arrays are retained by the map owner.
================
*/
interface MapPaint {
	next: UiView;
	game: UiView["gameplay"];
	pose: Pose | undefined | null;
	panel: string;
	w: number;
	h: number;
	full: UiRect;
	// The compact layout's window area (above the quickslots), when that
	// layout fits windows into it; undefined on the desktop layout.
	bounds?: UiRect;
	hover: string | null;
	hudData:
		| {
			map: AuthoredLayout;
			mapLabels: readonly MapLabel[];
			mapIcons: readonly MapIcon[];
			mapLayout?: WorldMapLayout;
			expandedMapLabels?: readonly MapLabel[];
			expandedMapIcons?: readonly MapIcon[];
			zones?: Readonly<Record<string, string>>;
			// The object and UI text (textdataname over textuisystem).
			strings?: Readonly<Record<string, string>>;
		}
		| null
		| undefined;
	trackedQuest: number;
	minimapResources: { positions(): ReturnType<typeof minimapNpcPositions> | undefined; };
	// The dungeon pages, read by the caller this frame.
	caveMaps?: CaveMapCatalog | undefined;
	mapTeleport: {
		view( page: number, clip: UiRect, pan: readonly [number, number], center: Pose, layout?: WorldMapLayout ): void;
		viewCave( pick: ( x: number, y: number ) => { regionId: number; x: number; z: number; } | null ): void;
		hide(): void;
	};
	reverseScrollHud: { mapping(): boolean; };
	text: MapText;
	resources: MapImages;
	paths: string[];
	quads: UiQuad[];
	controls: UiControl[];
	hudCopy: ( key: string ) => string;
	beginWindow: () => readonly [number, number, number, number];
	endWindow: ( mark: readonly [number, number, number, number], owner?: string, source?: UiRect ) => void;
	windowBox: ( title: string, x: number, y: number, width: number, height: number ) => void;
	authoredButton: ( node: AuthoredControl, x: number, y: number, id: string, label: string ) => void;
	closeButton: ( x: number, y: number, id: ReturnType<typeof panelCloseId> ) => void;
	image: ( rect: UiRect, path: string ) => void;
}

/*
================
MapDetails
================
*/
interface MapDetails {
	worldVisible: boolean;
	panel: string;
	pressed: string | null;
	hover: string | null;
	full: UiRect;
	paths: string[];
	quads: UiQuad[];
	text: MapText;
	resources: MapImages;
	rect: ( rect: UiRect, color: UiQuad["color"], texture?: string ) => void;
}

/*
================
MapText

Only the font operations used by the map; the UI retains font ownership.
================
*/
interface MapText {
	path(): string | null;
	run( value: string, style: number, font: number ): { width: number; };
	extentHeight( font: number ): number;
	quads( value: string, rect: UiRect, clip: UiRect, color: UiQuad["color"], style?: GlyphStyle ): UiQuad[];
}

/*
================
MapImages
================
*/
interface MapImages {
	has( path: string ): boolean;
	size( path: string ): readonly [number, number] | undefined;
}

/*
================
createWorldMapHud
================
*/
export function createWorldMapHud( options: MapHudOptions ) {
	const experimental = options.experimental;
	const huntingGuide = createHuntingGuideHud( options.assets, options.base );
	const mapTools = createMapToolsHud( options );
	const uniqueNests = createUniqueNestsHud( options.assets, options.base );
	let mapSmall = false, mapFollow = true, mapPage = 0, mapX = 100, mapY = 100;
	let mapPan: [number, number] = [ 0, 0 ], mapCenter: Pose | null = null;
	// The last pose the world map could place. 5758D0 finds no page for a
	// dungeon point, so 57FE60 keeps the loaded page; a dungeon without a map
	// point (worldMapDungeonPose) holds the view here instead.
	let mapPose: Pose | null = null;
	// The window drew a cave floor last frame, so a manual view holds cave
	// coordinates rather than the dungeon's point on the world map.
	let mapCaveShown = false;
	// The dungeon the player stood in when the globe chose the world page
	// (issue #3429): while they stay in it the world page holds, instead of
	// their floor's page, until the pin, a dungeon entrance or a finished
	// load brings the cave page back. The v1.188 globe returns to the world
	// from any dungeon page; inferred, as the port's own floor default is
	// otherwise re-derived from the pose every frame and overruled it.
	let mapWorldDungeon: number | null = null;
	// The character level the last painted frame saw; clicks arrive after
	// a paint, and the tools' level filter reads it (issue #3167).
	let lastLevel: number | undefined;
	/*
	================
	mapViewPose

	The pose the view centres on. 57A570 centres a dungeon on its map point,
	never on the cave's own coordinates, which lie off every page.
	================
	*/
	function mapViewPose<T extends Pose | null | undefined>( pose: T ): T | Pose {
		if ( !pose || featuresAt( pose.regionId ).worldMap ) return pose;
		return worldMapDungeonPose( pose.regionId ) ?? mapPose ?? pose;
	}
	/*
	================
	heldCenter

	A manual view's centre: its own copy of the pose the view shows now.
	================
	*/
	function heldCenter( pose: Pose | null | undefined ): Pose | null {
		const view = mapCaveShown ? pose : mapViewPose( pose );
		return view ? { ...view } : null;
	}
	// guideAvailable: the shard offers the port-only portrait overlay;
	// catalogueAvailable: it publishes the atlas the lists read (#3489).
	let guideAvailable = false, catalogueAvailable = false;
	// The expanded world map's layout (port-only, Experimental), as the last
	// presented frame read it; undefined is the native layout.
	let mapLayout: WorldMapLayout | undefined;
	return {
		/*
		================
		position
		================
		*/
		position(): readonly [number, number] {
			return [ mapX, mapY ];
		},
		/*
		================
		restorePosition
		================
		*/
		restorePosition( position: readonly [number, number] | undefined, next: UiView ) {
			[mapX, mapY] = position ??
				[
					Math.trunc( next.width / 2 ) - WORLD_MAP_WIDTH / 2,
					Math.trunc( next.height / 2 ) - WORLD_MAP_HEIGHT / 2
				];
		},
		/*
		================
		reset
		================
		*/
		reset() {
			mapTools.clearView();
			mapFollow = true;
			mapSmall = false;
			mapPan = [ 0, 0 ];
			mapCenter = null;
			mapPose = null;
			mapWorldDungeon = null;
		},
		/*
		================
		follow
		================
		*/
		follow() {
			mapWorldDungeon = null;
			mapFollow = true;
			mapPan = [ 0, 0 ];
			mapCenter = null;
		},
		/*
		================
		shortcut
		================
		*/
		shortcut( id: string ) {
			if ( guideAvailable && mapPage === 0 && id === "map-hunting-toggle" ) {
				const saved = experimental.state().saved;
				experimental.restore( { ...saved, monsterGuide: !saved.monsterGuide } );
				options.saveExperimental?.( experimental.state().saved );
				return true;
			}
			return false;
		},
		/*
		================
		activate
		================
		*/
		activate( id: string, pose: Pose | null | undefined ) {
			// The map tools (issue #3167) take their ids first; a picked
			// species hands back the pose to centre on.
			const picked = mapTools.activate(
				id,
				huntingGuide.catalogue(),
				lastLevel,
				!!pose && !featuresAt( pose.regionId ).worldMap
			);
			if ( picked !== undefined ) {
				if ( id.startsWith( "map-dungeon" ) ) mapWorldDungeon = null;
				if ( picked ) {
					mapFollow = false;
					mapPan = [ 0, 0 ];
					mapCenter = { ...picked };
					mapPage = worldMapPageAt( picked, mapLayout );
					mapTools.clearView();
				}
				return true;
			}
			// 57F43D swallows city buttons in AUTO. The port allows the click and
			// selects Manual Movement State, preserving its existing behavior.
			if ( id.startsWith( "map-town:" ) ) {
				mapTools.clearView();
				const page = Number( id.slice( 9 ) );
				if ( worldMapPages( mapLayout ).some( p => p.id === page ) ) {
					mapPage = page;
					mapFollow = false;
					mapPan = [ 0, 0 ];
					mapCenter = heldCenter( pose );
				}
				return true;
			}
			// 57AEA0 resizes, then 57A570 recentres in either mode.
			if ( id === "map-size" ) {
				mapSmall = !mapSmall;
				mapPan = [ 0, 0 ];
				mapCenter = mapFollow ? null : heldCenter( pose );
				return true;
			}
			// 575E90 flips AUTO MOVE only; keep the last automatic centre.
			// The v1.188 window has no captioned AUTO_MOVE button: its
			// follow pin (issue #3210) is the one switch.
			if ( id === "map-pin" ) {
				mapTools.clearView();
				mapFollow = !mapFollow;
				if ( mapFollow ) mapWorldDungeon = null;
				mapCenter = heldCenter( pose );
				mapPan = [ 0, 0 ];
				return true;
			}
			// 57F410 selects the world and Manual Movement State.
			// The dungeon page's big globe (issue #3210) lands here too.
			if ( id === "map-world" || id === "map-globe" ) {
				mapTools.clearView();
				mapWorldDungeon = pose && !featuresAt( pose.regionId ).worldMap ?
					worldMapDungeonOf( pose.regionId ) ?? pose.regionId :
					null;
				mapPage = 0;
				mapFollow = false;
				// The world page centres on the dungeon's map point, never
				// on the cave coordinates the floor page held.
				const view = mapViewPose( pose );
				mapCenter = view ? { ...view } : null;
				mapPan = [ 0, 0 ];
				return true;
			}
			return false;
		},
		/*
		================
		surface
		================
		*/
		surface( id: string ) {
			return id === "map-pan" || id.startsWith( MAP_ROSTER_PREFIX ) ||
				(guideAvailable && id.startsWith( HUNTING_AREA_PREFIX ));
		},
		/*
		================
		drag
		================
		*/
		drag( event: Extract<UiEvent, { kind: "drag"; }> ) {
			if ( mapTools.drag( event.id, event.dy ) ) return;
			if ( event.id === "map-drag" ) {
				mapX = Math.max( 0, mapX + event.dx );
				mapY = Math.max( 0, mapY + event.dy );
			} else if (
				(event.id === "map-pan" || event.id.startsWith( MAP_ROSTER_PREFIX ) ||
					event.id.startsWith( "map-town:" ) ||
					event.id.startsWith( HUNTING_AREA_PREFIX )) && !mapFollow
			) {
				mapPan = [ mapPan[0] + event.dx, mapPan[1] + event.dy ];
			}
		},
		/*
		================
		step
		================
		*/
		step( next: UiView, panel: string ) {
			const source = next.session?.phase === "world" ? next.session.huntingGuide : undefined;
			catalogueAvailable = source !== undefined;
			guideAvailable = !!source?.overlay;
			const stepPose = next.gameplay?.pose;
			const page = mapFollow && stepPose && featuresAt( stepPose.regionId ).worldMap ?
				worldMapPageAt( stepPose, mapLayout ) :
				mapPage;
			// The catalogue bytes load when something asks for them:
			// the guide opt-in, or the monster drawer (issue #3167)
			// opening. The portrait art still waits for the opt-in.
			const guideChanged = huntingGuide.step(
				source,
				panel === "Map" && (guideAvailable && experimental.state().saved.monsterGuide || mapTools.drawerOpen()),
				panel === "Map" && page === 0 && guideAvailable && experimental.state().saved.monsterGuide
			);
			// Two switches, both needed: the Experimental row offers the
			// M window's unique spawns box, and the box (off by default)
			// draws the signs. Nothing loads before both are on.
			const nestsChanged = uniqueNests.step(
				panel === "Map" && experimental.state().saved.uniqueSpawnPoints && mapTools.uniqueSpawns()
			);
			return mapTools.step( panel ) || guideChanged || nestsChanged;
		},
		/*
		================
		present
		================
		*/
		present( input: MapPaint ) {
			const next = input.next,
				game = input.game,
				pose = input.pose,
				panel = input.panel,
				w = input.w,
				h = input.h,
				full = input.full,
				hudData = input.hudData,
				trackedQuest = input.trackedQuest,
				minimapResources = input.minimapResources,
				mapTeleport = input.mapTeleport,
				reverseScrollHud = input.reverseScrollHud,
				text = input.text,
				resources = input.resources,
				paths = input.paths,
				quads = input.quads,
				controls = input.controls,
				hudCopy = input.hudCopy,
				beginWindow = input.beginWindow,
				endWindow = input.endWindow,
				windowBox = input.windowBox,
				authoredButton = input.authoredButton,
				closeButton = input.closeButton,
				image = input.image;

			let hover = input.hover;
			lastLevel = game?.progression?.level;
			// The current map is a world-lifetime resource, not a popup-lifetime
			// resource. Closing M must not release the image and flash on the next open.
			const expandedMap = expandedWorldMapOn( game );
			mapLayout = expandedMap ? hudData?.mapLayout : undefined;
			const mapIcons = expandedMap ? hudData?.expandedMapIcons : hudData?.mapIcons;
			const mapLabels = expandedMap ? hudData?.expandedMapLabels : hudData?.mapLabels;
			// 5758D0 finds no page for a dungeon point and 57FE60 then keeps the
			// loaded page and its scroll: only a pose on the map may derive a
			// page or hold the view, so an off-map pose leaves both untouched.
			const poseOnMap = pose && featuresAt( pose.regionId ).worldMap ? pose : null;
			if ( poseOnMap ) mapPose = poseOnMap;
			if (
				(mapFollow && poseOnMap) || (mapPage !== 0 && !worldMapPages( mapLayout ).some( p => p.id === mapPage ))
			) {
				mapPage = poseOnMap ? worldMapPageAt( poseOnMap, mapLayout ) : 0;
				mapPan = [ 0, 0 ];
				mapCenter = null;
			}
			const area: UiRect = input.bounds ?? [ 0, 0, w, h ];
			const mapWidth = Math.min(
					mapSmall ? 268 : WORLD_MAP_WIDTH,
					Math.max( 0, area[2] - COMPACT_MAP_MARGIN * 2 )
				),
				mapHeight = Math.min(
					mapSmall ? 296 : WORLD_MAP_HEIGHT,
					Math.max( 0, area[3] - COMPACT_MAP_MARGIN * 2 - MAP_FOOT_DEPTH )
				);
			// The side tab, and the guide while it is open, stand left of
			// the window: keep them on the screen where the area allows.
			const mapRight = area[0] + area[2] - mapWidth,
				mapLeftMost = Math.min(
					area[0] + mapLeftReach( mapTools.drawerOpen() ),
					Math.max( area[0], mapRight )
				);
			const mapLeft = Math.max( mapLeftMost, Math.min( mapX, mapRight ) ),
				mapTop = Math.max( area[1], Math.min( mapY, area[1] + area[3] - mapHeight - MAP_FOOT_DEPTH ) );
			mapX = mapLeft;
			mapY = mapTop;
			if ( mapPage !== 0 && hover?.startsWith( HUNTING_AREA_PREFIX ) ) hover = null;
			const mapGuideEnabled = guideAvailable && experimental.state().saved.monsterGuide;
			const mapClip: UiRect = mapPanel( [ mapLeft, mapTop, mapWidth, mapHeight ] );
			const mapHits: UiControl[] = [];
			// 57FE60's marker passes, in its order: quest NPCs (57B1C0), hunting points
			// (57B550), then the apprenticeship and party rosters (57CE80). Each is a
			// 16x16 quad centred on the projected world position; the world map has no
			// 47px ring, so unlike the minimap there is no edge clamp and no arrow
			// sprite swap, and off-page members are simply clipped by the window.
			// The quest pass draws the tracked quest's targets; port-only, the
			// quest tracker row widens it to every active quest (#840).
			const mapMarkers: MapMarker[] = [];
			const roster = pose ? rosterPositions( pose, game ?? null, next.entities ) : [];
			let rosterStart = 0;
			// Port-only, not native: the unique nest signs, under every native
			// pass. Only an open map lists them; a closed one demands the one
			// sign image below, never a path per nest.
			const nestSigns = experimental.state().saved.uniqueSpawnPoints && mapTools.uniqueSpawns();
			if ( pose && nestSigns && panel === "Map" ) mapMarkers.push( ...uniqueNests.nests() );
			if ( pose ) {
				for (
					const id of questMarkerTargets(
						game?.quests,
						trackedQuest,
						experimental.state().saved.questTrackerMarkers
					)
				) {
					const target = minimapResources.positions()?.get( id );
					if ( target && minimapSameFloor( pose, target, game ?? null ) ) {
						mapMarkers.push( {
							regionId: target.regionId,
							x: target.x,
							z: target.z,
							rotation: 0,
							path: ROOT + "interface/worldmap/wmap_sign_questnpc.png"
						} );
					}
				}
				for ( const point of game?.huntingPoints ?? [] ) {
					const live = next.entities.find( e => e.gid === point.gid ) ?? point;
					mapMarkers.push( {
						regionId: live.regionId,
						x: live.x,
						z: live.z,
						rotation: -point.angle * Math.PI * 2 / 65536,
						path: ROOT + "interface/worldmap/wmap_sign_huntingpoint.png"
					} );
				}
				// Beta operator roster (port-only, beta-map.ts): every other online
				// player, drawn before the party pass so party signs stay on top.
				for ( const player of game?.betaPlayers ?? [] ) {
					if ( player.gid === game?.localGid ) continue;
					mapMarkers.push( {
						regionId: player.regionId,
						x: player.x,
						z: player.z,
						rotation: 0,
						path: ROOT + "interface/minimap/mm_sign_otherplayer.png"
					} );
				}
				rosterStart = mapMarkers.length;
				for ( const row of roster ) {
					mapMarkers.push( {
						regionId: row.regionId,
						x: row.x,
						z: row.z,
						rotation: 0,
						path: ROOT + "interface/worldmap/wmap_sign_" + row.kind + ".png"
					} );
				}
				// The species picked from the monster drawer (issue
				// #3167), marked where it lives, above the roster signs.
				const picked = mapTools.mark();
				if ( picked ) {
					mapMarkers.push( {
						regionId: picked.regionId,
						x: picked.x,
						z: picked.z,
						rotation: 0,
						path: ROOT + "interface/worldmap/wmap_sign_huntingpoint.png"
					} );
				}
			}
			// A closed map only demands its images, so opening it is instant: the
			// demand is computed without the projection (worldMapDemand). Its label
			// glyphs (the largest allocation of a HUD step) and click hits are built
			// while it is open. The font atlas is demanded either way.
			const mapOpen = panel === "Map";
			// A dungeon floor with a page shows it; the rest keep the world map.
			// The tools (issue #3167) may overrule the page: a chosen floor,
			// or a dungeon opened from its entrance while the player is out.
			const poseDungeon = pose && !poseOnMap ? worldMapDungeonOf( pose.regionId ) ?? pose.regionId : null;
			if ( poseDungeon !== mapWorldDungeon ) mapWorldDungeon = null;
			const indoorPage = pose && !poseOnMap && mapWorldDungeon === null ?
				caveMapPage( input.caveMaps, pose.regionId, game?.navigationFloor ) :
				undefined;
			const toolsPage = mapTools.cavePage( input.caveMaps, pose );
			const cavePage = toolsPage?.page ?? indoorPage;
			const caveMark = mapTools.mark();
			const caveMap = mapOpen && pose && cavePage && input.caveMaps ?
				caveMapView( {
					pose,
					center: toolsPage?.center ??
						(mapCenter?.regionId === pose.regionId ? mapCenter : pose),
					page: cavePage,
					catalog: input.caveMaps,
					clip: mapClip,
					pan: mapFollow ? [ 0, 0 ] : mapPan,
					// The "you are here" sign stands only on the floor
					// the player is on (issue #3239).
					local: cavePageLocal( input.caveMaps, cavePage, pose, game?.navigationFloor ),
					mark: caveMark && caveMark.regionId === cavePage.regionId ? caveMark : null
				} ) :
				null;
			mapCaveShown = caveMap !== null;
			if ( caveMap ) mapPan = caveMap.pan;
			// The teleport's inverse pick must track the painted projection: it
			// is only valid while the map is open and a pose exists, so record the
			// frame under the same condition that builds the projection.
			if ( mapOpen && pose && caveMap ) {
				mapTeleport.viewCave( caveMap.point );
			} else if ( mapOpen && pose ) {
				mapTeleport.view(
					mapPage,
					mapClip,
					mapPan,
					mapCenter ?? mapViewPose( pose ),
					mapLayout
				);
			} else {
				mapTeleport.hide();
			}
			const mapProjection = pose && mapOpen && !caveMap ?
				worldMapPresentation(
					pose,
					mapPage,
					mapClip,
					mapPan,
					mapCenter ?? mapViewPose( pose ),
					mapLabels,
					mapIcons,
					mapMarkers,
					mapLayout
				) :
				null;
			// MANUAL drags (AUTO swallows them, 57F43D) move the stored position by the
			// clamped delta (579920); keep the displayed pan so reversing never has to
			// unwind an overshoot first.
			if ( mapProjection && !mapFollow ) mapPan = mapProjection.pan;
			// 57E9A0 builds the 6200 overlay in ascending record id, so the SN_ labels
			// (11xxx-14xxx) enter before the kind-2 icons (92xxx) and the icons paint
			// over them; 57FE60 then runs every marker pass after that whole overlay.
			// Paint background, label glyphs, icons, markers - the arrow is last and
			// can never be hidden by a shop icon or a zone label.
			// 57BBB0 traverses icons first, then labels. 57ED01 centres each label by
			// its own font extent, subtracting the integer half-width from the anchor.
			const mapGuide = mapOpen && mapGuideEnabled && pose && !caveMap ?
				huntingGuide.present(
					mapPage,
					mapClip,
					mapPan,
					mapCenter ?? mapViewPose( pose ),
					hover,
					mapLayout,
					mapTools.filterCap( game?.progression?.level )
				) :
				null;
			// The monster picked in the guide (sub_e75d50, issue #3390):
			// its hunting ground tinted on the world page, under the labels.
			const guidePick = mapProjection && mapPage === 0 && pose ? mapTools.guidePick() : null;
			const guideGround = guidePick && pose ?
				guideOverlay(
					guidePick.catalog,
					guidePick.index,
					worldMapFrame( mapPage, mapClip, mapPan, mapCenter ?? mapViewPose( pose ), mapLayout ),
					mapClip
				) :
				null;
			const fontPath = text.path();
			if ( !mapOpen && fontPath ) paths.push( fontPath );
			const rosterSigns = mapOpen && pose && !caveMap ?
				worldMapPointMarkers( {
					page: mapPage,
					clip: mapClip,
					pan: mapPan,
					center: mapCenter ?? mapViewPose( pose ),
					points: roster,
					layout: mapLayout
				} ) :
				[];
			const mapSigns = mapProjection ? [ ...mapProjection.markers ] : [];
			const hovered = rosterSigns.find( sign => mapRosterId( sign.point ) === hover );
			const signAt = mapProjection && hovered ?
				mapProjection.markerRows.indexOf( rosterStart + roster.indexOf( hovered.point ) ) :
				-1;
			if ( hovered && signAt >= 0 ) {
				// Painted right after the member's own sign, so later signs cover it.
				// Native names at most one member per pass (flag reset at 57D4DF);
				// one hover names one member in all. Inference: the name is clipped
				// to the map area like the signs it belongs to.
				const rect = hovered.rect;
				mapSigns.splice(
					signAt + 1,
					0,
					...mapSignName( text, hovered.point.name, rect[0] + rect[2] / 2, rect[1] + rect[3] / 2, mapClip )
				);
			}
			const mapImages = caveMap ?
				cavePageImages( caveMap, text, mapClip, hover ) :
				mapProjection ?
				[
					...mapProjection.background,
					...(mapGuide?.quads ?? []).map( q =>
						q.texture.startsWith( "/assets/npc/hunting-portraits/" ) && !resources.has( q.texture ) ?
							{ ...q, texture: HUNTING_MONSTER_SIGN } :
							q
					),
					...(mapGuide?.labels ?? []).flatMap( row =>
						text.quads( row.value, row.rect, mapClip, row.color, {
							hAlign: 1,
							vAlign: 1,
							overflow: "clip"
						} )
					),
					// A mask drawn before its image loads would tint the
					// whole tile square: each waits for its own texture.
					...(guideGround?.quads ?? []).filter( q => resources.has( q.texture ) ),
					...mapProjection.overlay,
					...(mapOpen ? mapProjection.labels : []).flatMap( ( { label: entry, x, y, clip } ) => {
						const width = text.run( entry.text, 0, entry.font ).width,
							height = text.extentHeight( entry.font ),
							left = x - Math.floor( width / 2 );
						if ( !mapLabelVisible( [ left, y, width, height ], clip ) ) return [];
						return text.quads(
							entry.text,
							[ left, y, width, height ],
							clip,
							entry.color,
							{ fontIndex: entry.font, hAlign: 0, vAlign: 0 }
						);
					} ),
					...mapSigns
				] :
				[];
			if ( caveMap ) mapHits.push( ...cavePageHits( caveMap ) );
			for ( const { icon, rect: r } of mapOpen ? mapProjection?.hits ?? [] : [] ) {
				const page = worldMapPages( mapLayout ).find( p => p.id === icon.destination );
				if ( page ) {
					mapHits.push( {
						id: "map-town:" + page.id,
						label: hudCopy( "UIIT_STT_" + page.name ),
						rect: r,
						kind: "button",
						draggable: true
					} );
				}
			}
			for ( const { point, rect } of rosterSigns ) {
				const left = Math.max( rect[0] + MAP_ROSTER_HOVER_INSET, mapClip[0] ),
					top = Math.max( rect[1] + MAP_ROSTER_HOVER_INSET, mapClip[1] );
				const right = Math.min( rect[0] + rect[2] - MAP_ROSTER_HOVER_INSET, mapClip[0] + mapClip[2] );
				const bottom = Math.min( rect[1] + rect[3] - MAP_ROSTER_HOVER_INSET, mapClip[1] + mapClip[3] );
				if ( right <= left || bottom <= top ) continue;
				// The native test reads the pointer, not a control: the sign never
				// takes the press, so a drag that starts on it still pans the map.
				mapHits.push( {
					id: mapRosterId( point ),
					label: point.name,
					rect: [ left, top, right - left, bottom - top ],
					kind: "region",
					draggable: true
				} );
			}
			// Port-only: the reverse return map points while the map holds a scroll.
			if ( mapOpen && pose && !caveMap && reverseScrollHud.mapping() ) {
				const buttons = reverseMapButtons( {
					points: worldMapPointMarkers( {
						page: mapPage,
						clip: mapClip,
						pan: mapPan,
						center: mapCenter ?? mapViewPose( pose ),
						points: game?.reverseMapPoints ?? [],
						layout: mapLayout
					} ),
					clip: mapClip,
					hover,
					has: path => resources.has( path )
				} );
				paths.push( ...buttons.paths );
				mapImages.push( ...buttons.images );
				mapHits.push( ...buttons.hits );
			}
			if ( mapOpen ) {
				paths.push(
					...(mapGuide?.paths ?? []),
					...(guideGround?.paths ?? []),
					...mapImages.map( q => q.texture ).filter( Boolean )
				);
			} else if ( cavePage && input.caveMaps ) {
				// Demanded on the floor, so M opens on a loaded page.
				paths.push( caveMapImage( input.caveMaps, cavePage ) );
			} else if ( pose ) {
				worldMapDemand(
					mapPage,
					mapClip,
					mapPan,
					mapCenter ?? mapViewPose( pose ),
					mapIcons ?? [],
					paths,
					mapLayout
				);
				for ( const row of mapMarkers ) paths.push( row.path );
				if ( nestSigns ) paths.push( UNIQUE_NEST_SIGN );
				paths.push( MAP_LOCAL_MARKER );
			}
			if ( panel === "Map" && hudData ) {
				// Map tiles and icons stream in as a pan reveals them (demanded above, and
				// absent textures simply do not draw). Gating admission on them disabled
				// map-pan mid-drag, which cancelled the drag. Chrome stays gated.
				const admission = beginWindow();
				const mw = mapWidth, mh = mapHeight, mx = mapLeft, my = mapTop;
				if ( mapFollow && poseOnMap ) {
					mapPage = worldMapPageAt( poseOnMap, mapLayout );
					mapPan = [ 0, 0 ];
					mapCenter = null;
				}
				const page = worldMapPages( mapLayout ).find( row => row.id === mapPage ),
					// 576949: a town page captions as UIIT_PAG_LOCALMAP, the three
					// wide chars at 0xBD959C (space dash space), then the town's
					// own string; mode 0 keeps UIIT_PAG_WORLDMAP (5768EC). A
					// dungeon page heads with the later client's zone name of
					// its floor's region (issue #3238), which the catalogue
					// carries: "Underground Level 1 of Tomb of Qin-Shi".
					caption = (cavePage && caveMap ? cavePage.caption : undefined) ||
						(page ?
							hudCopy( "UIIT_PAG_LOCALMAP" ) + " - " + hudCopy( "UIIT_STT_" + page.name ) :
							hudCopy( "UIIT_PAG_WORLDMAP" )) ||
						"World Map";
				windowBox( caption, mx, my, mw, mh );
				const inner = mapClip;
				// The map surface is CIFWorldMap itself, not a CIFButton: pressing or
				// dragging it never activates or plays SND_BUTTON_CLICK.
				controls.push( { id: "map-pan", label: "Pan map", kind: "region", draggable: true, rect: inner }, {
					id: "map-drag",
					label: "Move map",
					kind: "button",
					draggable: true,
					rect: [ mx + 20, my + 4, mw - 96, 24 ]
				} );
				quads.push( ...mapImages );
				controls.push( ...(mapGuide?.controls ?? []), ...mapHits );
				if ( guideAvailable && mapPage === 0 && !caveMap ) {
					paths.push(
						ROOT + "interface/ifcommon/com_checkbutton_on.png",
						ROOT + "interface/ifcommon/com_checkbutton_off.png"
					);
					image(
						[ mx + 10, my + 10, 16, 16 ],
						ROOT + "interface/ifcommon/com_checkbutton_" + (mapGuideEnabled ? "on" : "off") + ".png"
					);
					quads.push(
						...text.quads( "Hunting", [ mx + 30, my + 10, 48, 17 ], full, [ .96, .88, .66, 1 ], {
							vAlign: 1
						} )
					);
					controls.push( {
						id: "map-hunting-toggle",
						kind: "button",
						label: "Hunting areas",
						helpText: (mapGuideEnabled ? "Hide" : "Show") + " hunting areas",
						rect: [ mx + 8, my + 7, 73, 20 ],
						selected: mapGuideEnabled
					} );
				}
				const nodes = hudData.map;
				authoredButton(
					{ ...nodes.GDR_WM_BTN_WNDSIZE!, rect: [ mw - 44, 10, 16, 16 ] },
					mx,
					my,
					"map-size",
					"Change map size"
				);
				closeButton( mx + mw - 26, my + 10, panelCloseId( "Map" ) );
				// The tools strip and drawer (issue #3167) draw inside
				// the window, above the map and its hits.
				const tools = mapTools.present( {
					open: true,
					cave: caveMap !== null,
					followed: mapFollow,
					floors: caveMap && input.caveMaps && cavePage ?
						caveDungeonFloors( input.caveMaps, cavePage.regionId ) :
						[],
					floor: caveMap ? cavePage : undefined,
					window: [ mx, my, mw, mh ],
					clip: mapClip,
					full,
					catalogue: huntingGuide.catalogue(),
					guidePublished: catalogueAvailable,
					characterLevel: game?.progression?.level,
					race: guideRace( next.entities.find( e => e.gid === game?.localGid )?.countryByte9c ),
					hover,
					zones: hudData.zones,
					strings: hudData.strings,
					levelNote: input.caveMaps?.monsterLevelNote,
					offerUniqueSpawns: experimental.state().saved.uniqueSpawnPoints,
					icons: (mapProjection?.hits ?? []).map( hit => hit.rect ),
					text,
					page: mapPage,
					pan: mapPan,
					center: pose ? mapCenter ?? mapViewPose( pose ) : null,
					layout: mapLayout,
					size: path => input.resources.size( path )
				} );
				paths.push( ...tools.paths );
				quads.push( ...tools.quads );
				controls.push( ...tools.controls );
				// The page tiles reach past the window through mapClip; the
				// window rect is what the compact fit must measure.
				endWindow( admission, "primary", [ mx, my, mw, mh + MAP_FOOT_DEPTH ] );
			}
			return hover;
		},
		/*
================
details
================
		*/
		details( input: MapDetails ) {
			if (
				guideAvailable && input.worldVisible && input.panel === "Map" && mapPage === 0 &&
				experimental.state().saved.monsterGuide && !input.pressed
			) {
				huntingGuide.presentDetails( input );
			}
		},
		/*
================
dispose
================
		*/
		dispose() {
			mapTools.dispose();
			huntingGuide.dispose();
			uniqueNests.dispose();
		}
	};
}
