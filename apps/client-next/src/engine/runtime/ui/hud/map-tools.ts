/*
===========================================================================

map-tools.ts - the world map's own tools (issue #3167)

Port-only presentation the owner asked for, on by default: a monster
level filter and a region and monster drawer in the map window's top
strip and right edge, clickable dungeon entrances, and the later
client's dungeon page chrome (issue #3210): the globe back to the
world and the follow pin at the map's top right corner, the floor
selector down its right edge, in art the build imports from the later
client. One owner for that state; the map HUD keeps the window, the
pages and the cave view itself. Monster levels and anchors come from
the hunting catalogue the session publishes; dungeon pages from the
cave catalogue; region names from the published regioncode table
read through the zone text.

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import type { Pose } from "@/engine/contracts/gameplay";
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import type { GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import {
	caveDungeonFloors,
	caveDungeonName,
	cavePageCenter,
	type CaveMapCatalog,
	type CaveMapPage
} from "@/engine/foundation/ui/cave-map";
import {
	MONSTER_LEVEL_FILTER_SPAN,
	monsterDirectory,
	type HuntingCatalogue,
	type MonsterDirectoryRegion
} from "@/engine/foundation/ui/hunting-guide";
import { worldMapDungeonPose, worldMapPointMarkers, type WorldMapLayout } from "@/engine/foundation/ui/world-map";
import { featuresAt } from "@/engine/foundation/world/plane";

const ROOT = "/assets/images/Media_extracted/";
const CHECK_ON = ROOT + "interface/ifcommon/com_checkbutton_on.png";
const CHECK_OFF = ROOT + "interface/ifcommon/com_checkbutton_off.png";
// The later client's dungeon-page chrome (issue #3210), published by
// import_vsro_worldmap_buttons.py beside the v1.150 worldmap art.
const WMAP = ROOT + "interface/worldmap/";
const GLOBE = WMAP + "wmap_big_button_world.png";
const PIN_AUTO = WMAP + "wmap_button_automatic.png";
const PIN_PASSIVE = WMAP + "wmap_button_passivity.png";
const FLOOR_UP = WMAP + "wmap_up_button.png";
const MAP_SIGN = WMAP + "wmap_sign_huntingpoint.png";
const FLOOR_UP_OFF = WMAP + "wmap_up_button_disable.png";
const FLOOR_DOWN = WMAP + "wmap_down_button.png";
const FLOOR_DOWN_OFF = WMAP + "wmap_down_button_disable.png";
const FLOOR_BOX = WMAP + "wmap_floor_box.png";
const FLOOR_CHOICE_BOX = WMAP + "wmap_floor_choice_box.png";
// The selector's plate is the floor track's own art drawn at the
// compact height the later client's stepper shows (issue #3238): its
// slate frame and dark ground, without the 228px stretch.
const FLOOR_TRACK = WMAP + "wmap_floor_big_box.png";
const REGIONCODE_PATH = "/assets/text/regioncode.json";
const WHITE = [ 1, 1, 1, 1 ] as const;
const GOLD = [ .96, .83, .55, 1 ] as const;
const DIM = [ .72, .7, .63, 1 ] as const;
const DRAWER_WIDTH = 196;
const ROW_HEIGHT = 18;

/*
================
MapToolsText

Only the font operations used here; the UI retains font ownership.
================
*/
interface MapToolsText {
	run( value: string, style: number, font: number ): { width: number; };
	extentHeight( font: number ): number;
	quads( value: string, rect: UiRect, clip: UiRect, color: UiQuad["color"], style?: GlyphStyle ): UiQuad[];
}

/*
================
MapToolsPaint

The frame inputs the tools draw with, borrowed for this call.
================
*/
interface MapToolsPaint {
	open: boolean;
	cave: boolean;
	followed: boolean;
	floors: readonly CaveMapPage[];
	floor: CaveMapPage | undefined;
	window: UiRect;
	clip: UiRect;
	full: UiRect;
	catalogue: HuntingCatalogue | undefined;
	characterLevel: number | undefined;
	zones: Readonly<Record<string, string>> | undefined;
	text: MapToolsText;
	page: number;
	pan: readonly [number, number];
	center: Pose | null;
	layout?: WorldMapLayout;
	huntingVisible: boolean;
}

/*
================
followPin

The follow pin at the map's top right corner, on the world and on a
dungeon page alike (issue #3210). The later client keeps the captioned
AUTO_MOVE button of the window layout and draws this one big button
over the map itself: the art names its two faces for the captioned
modes, wmap_button_automatic while the map follows the character and
wmap_button_passivity while it stands where it was put, and the
screenshots show the passive face in town and in the tomb alike. The
press lands on the map HUD's own follow state, the same state the
title bar's captioned button flips; the pin only shows it at the map.
================
*/
function followPin( input: MapToolsPaint, quads: UiQuad[], controls: UiControl[] ) {
	const rect: UiRect = [ input.clip[0] + input.clip[2] - 40, input.clip[1] + 4, 36, 36 ];
	quads.push( {
		texture: input.followed ? PIN_AUTO : PIN_PASSIVE,
		rect,
		uv: [ 0, 0, 1, 1 ],
		color: WHITE,
		clip: input.clip
	} );
	controls.push( {
		id: "map-pin",
		kind: "button",
		label: input.followed ? "Auto move" : "Manual move",
		helpText: input.followed ?
			"The map follows you; press for manual move" :
			"The map stands where you put it; press for auto move",
		rect,
		selected: input.followed
	} );
}

/*
================
RegionNamesLoad
================
*/
type RegionNamesLoad =
	| { kind: "idle"; }
	| { kind: "loading"; id: number; }
	| { kind: "ready"; names: ReadonlyMap<number, string>; }
	| { kind: "failed"; }
	| { kind: "disposed"; };

/*
================
dungeonKey

The dungeon a region belongs to, as the floor selector counts them:
1 is Donwhang's cave, 2 is Ch'in Tomb (regions 2..7 of the mask).
================
*/
function dungeonKey( regionId: number ): number {
	const dungeon = regionId & 0x7fff;
	if ( dungeon === 1 ) return 1;
	if ( dungeon >= 2 && dungeon <= 7 ) return 2;
	return regionId;
}

/*
================
createMapToolsHud
================
*/
export function createMapToolsHud( options: {
	assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">;
	base: string;
} ) {
	const assets = options.assets;
	let levelFilter = true;
	let drawerOpen = false;
	let drawerRegion: number | null = null;
	let drawerPage = 0;
	let mark: { regionId: number; x: number; z: number; refObjId: number; } | null = null;
	let dungeonView: number | null = null;
	let floorChoice: { regionId: number; floorIndex: number; } | null = null;
	// Whether the floor pick was made while viewing a dungeon from
	// outside it. A pick belongs to the side it was made on: walking
	// into the dungeon starts on the character's own floor, not on a
	// floor picked at the entrance (issue #3167).
	let choiceFromView = false;
	let choiceDungeon = 0;
	// The floors of the dungeon page last presented, and the page it
	// showed: the floor selector's arrows step from what is on screen.
	let shownFloors: readonly CaveMapPage[] = [];
	let shownFloor: CaveMapPage | undefined;
	let regionLoad: RegionNamesLoad = { kind: "idle" };
	let directoryFor: {
		catalogue: HuntingCatalogue;
		cap: number | undefined;
		regions: readonly MonsterDirectoryRegion[];
	} | null = null;

	/*
	================
	filterCap

	The level filter's cap for this character: their level plus the
	span, or undefined when the filter is off or the level unknown
	(the directory then shows every species).
	================
	*/
	function filterCap( characterLevel: number | undefined ): number | undefined {
		if ( !levelFilter || characterLevel === undefined ) return undefined;
		return characterLevel + MONSTER_LEVEL_FILTER_SPAN;
	}

	/*
	================
	directory

	The drawer rows for this catalogue and filter cap, rebuilt only
	when either changes.
	================
	*/
	function directory(
		catalogue: HuntingCatalogue | undefined,
		cap: number | undefined
	): readonly MonsterDirectoryRegion[] {
		if ( !catalogue ) return [];
		if ( directoryFor?.catalogue !== catalogue || directoryFor?.cap !== cap ) {
			directoryFor = { catalogue, cap, regions: monsterDirectory( catalogue, cap ) };
		}
		return directoryFor.regions;
	}

	/*
	================
	stepFloor

	Shows the floor one step from the one on screen (issue #3210):
	the selector's up arrow is the next floor down, its down arrow the
	one before, and the ends hold. The pick belongs to the side it was
	made on, as a named floor pick does.
	================
	*/
	function stepFloor( step: number, indoors: boolean ) {
		const current = shownFloors.findIndex(
			floor => floor.regionId === shownFloor?.regionId && floor.floorIndex === shownFloor?.floorIndex
		);
		const next = current < 0 ? undefined : shownFloors[current + step];
		if ( !next ) return;
		floorChoice = { regionId: next.regionId, floorIndex: next.floorIndex };
		choiceFromView = !indoors;
	}

	/*
	================
	regionName

	A region's shown name: the zone text by sector key, else the
	regioncode's zone text (or the key itself when no zone text
	names it), else a plain "Region <id>".
	================
	*/
	function regionName( region: number, zones: MapToolsPaint["zones"] ): string {
		const byKey = zones?.[String( region )];
		if ( byKey ) return byKey;
		const code = regionLoad.kind === "ready" ? regionLoad.names.get( region ) : undefined;
		if ( code ) return zones?.[code + "_01"] ?? zones?.[code] ?? code;
		return "Region " + region;
	}

	return {
		/*
		================
		step

		Drives the regioncode load while the map is open; true when a
		load landed and the frame should redraw.
		================
		*/
		step( panel: string ): boolean {
			if ( regionLoad.kind === "disposed" ) return false;
			let changed = false;
			if ( panel === "Map" && regionLoad.kind === "idle" && assets.available() > 0 ) {
				regionLoad = {
					kind: "loading",
					id: assets.request( new URL( REGIONCODE_PATH, options.base ).href )
				};
				changed = true;
			}
			if ( regionLoad.kind === "loading" ) {
				const result = assets.take( regionLoad.id );
				if ( result ) {
					try {
						if ( result.kind !== "bytes" ) throw Error( "Regioncode resource mismatch" );
						const data = JSON.parse(
							new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer )
						) as { format?: unknown; entries?: Record<string, unknown>; };
						if ( data?.format !== "sro-regioncode" || !data.entries ) {
							throw Error( "Invalid regioncode" );
						}
						const names = new Map<number, string>();
						for ( const [key, value] of Object.entries( data.entries ) ) {
							const region = Number( key );
							if ( Number.isInteger( region ) && typeof value === "string" ) names.set( region, value );
						}
						regionLoad = { kind: "ready", names };
					} catch {
						regionLoad = { kind: "failed" };
					}
					changed = true;
				}
			}
			return changed;
		},
		/*
		================
		filterCap
		================
		*/
		filterCap,
		/*
		================
		drawerOpen

		Whether the region and monster list is open: the catalogue
		bytes are only worth fetching once it (or the guide opt-in)
		asks for them.
		================
		*/
		drawerOpen(): boolean {
			return drawerOpen;
		},
		/*
		================
		mark

		The picked species' anchor, drawn as a map marker by the map
		HUD; null until a species is picked from the drawer.
		================
		*/
		mark(): { regionId: number; x: number; z: number; refObjId: number; } | null {
			return mark;
		},
		/*
		================
		viewingDungeon

		Whether a dungeon page is being viewed from outside it.
		================
		*/
		viewingDungeon(): boolean {
			return dungeonView !== null;
		},
		/*
		================
		clearView

		Back to the world: the viewed dungeon and the chosen floor
		are done (the map HUD's own navigation calls this).
		================
		*/
		clearView() {
			dungeonView = null;
			floorChoice = null;
		},
		/*
		================
		cavePage

		The cave page the tools put on screen, when they overrule the
		map's indoor default: a chosen floor of the dungeon the player
		stands in, or the viewed dungeon's chosen (else first) floor
		with the page's centre to look at. Undefined keeps the default.
		================
		*/
		cavePage(
			catalog: CaveMapCatalog | undefined,
			pose: Pose | null | undefined
		): { page: CaveMapPage; center?: Pose; } | undefined {
			if ( !catalog ) return undefined;
			if ( pose && !featuresAt( pose.regionId ).worldMap ) {
				const key = dungeonKey( pose.regionId );
				if ( choiceDungeon !== key ) {
					choiceDungeon = key;
					floorChoice = null;
				}
				// A floor picked at the entrance is done once inside:
				// the character's own floor stands until they pick here.
				if ( floorChoice && choiceFromView ) floorChoice = null;
				if ( floorChoice ) {
					const chosen = caveDungeonFloors( catalog, pose.regionId )
						.find( f => f.regionId === floorChoice!.regionId && f.floorIndex === floorChoice!.floorIndex );
					if ( chosen ) return { page: chosen };
				}
				return undefined;
			}
			if ( dungeonView === null ) return undefined;
			const anchor = dungeonView === 1 ? 0x8001 : 0x8002;
			const floors = caveDungeonFloors( catalog, anchor );
			if ( floors.length === 0 ) return undefined;
			if ( choiceDungeon !== dungeonView ) {
				choiceDungeon = dungeonView;
				floorChoice = null;
			}
			if ( floorChoice && !choiceFromView ) floorChoice = null;
			const chosen = floorChoice ?
				floors.find( f => f.regionId === floorChoice!.regionId && f.floorIndex === floorChoice!.floorIndex ) :
				undefined;
			const page = chosen ?? floors[0]!;
			return { page, center: cavePageCenter( page ) };
		},
		/*
		================
		activate

		Handles the tools' control ids. Returns a pose when a picked
		species wants the map centred on it, null when the id was
		handled without one, and undefined when the id is not ours.
		================
		*/
		activate(
			id: string,
			catalogue: HuntingCatalogue | undefined,
			characterLevel: number | undefined,
			indoors: boolean = false
		): Pose | null | undefined {
			if ( id === "map-level-filter" ) {
				levelFilter = !levelFilter;
				drawerPage = 0;
				return null;
			}
			if ( id === "map-list" ) {
				drawerOpen = !drawerOpen;
				drawerRegion = null;
				drawerPage = 0;
				return null;
			}
			if ( id === "map-drawer-back" ) {
				drawerRegion = null;
				drawerPage = 0;
				return null;
			}
			if ( id === "map-drawer-prev" || id === "map-drawer-next" ) {
				drawerPage += id === "map-drawer-next" ? 1 : -1;
				if ( drawerPage < 0 ) drawerPage = 0;
				return null;
			}
			if ( id.startsWith( "map-region:" ) ) {
				const region = Number( id.slice( "map-region:".length ) );
				if ( Number.isInteger( region ) ) {
					drawerRegion = region;
					drawerPage = 0;
				}
				return null;
			}
			if ( id.startsWith( "map-monster:" ) ) {
				// The id names the region whose list the row stood in: a
				// species can live in several, and the pick centres on
				// the anchor of the one the player was reading.
				const [regionId, refObjId] = id.slice( "map-monster:".length ).split( ":" ).map( Number );
				if (
					regionId === undefined || refObjId === undefined ||
					!Number.isInteger( regionId ) || !Number.isInteger( refObjId )
				) return null;
				const regions = directory( catalogue, filterCap( characterLevel ) );
				for ( const region of regions ) {
					if ( region.region !== regionId ) continue;
					const entry = region.monsters.find( row => row.monster.refObjId === refObjId );
					if ( entry ) {
						// The catalogue's anchors are world units; a pose
						// is its sector plus the local position x10.
						const pose: Pose = {
							regionId: entry.region,
							x: (entry.x - (entry.region & 255) * 192) * 10,
							y: 0,
							z: (entry.z - (entry.region >>> 8) * 192) * 10,
							angle: 0
						};
						mark = { regionId: pose.regionId, x: pose.x, z: pose.z, refObjId };
						return pose;
					}
				}
				return null;
			}
			if ( id.startsWith( "map-dungeon:" ) || id.startsWith( "map-dungeon-icon:" ) ) {
				// The label and the entrance's own icon carry the id in
				// two shapes (map-dungeon:N, map-dungeon-icon:N); the
				// dungeon is the number after the last colon either way.
				const dungeon = Number( id.slice( id.lastIndexOf( ":" ) + 1 ) );
				if ( dungeon === 1 || dungeon === 2 ) {
					dungeonView = dungeon;
					floorChoice = null;
					choiceDungeon = dungeon;
				}
				return null;
			}
			if ( id === "map-floor-up" || id === "map-floor-down" ) {
				stepFloor( id === "map-floor-up" ? 1 : -1, indoors );
				return null;
			}
			if ( id.startsWith( "map-floor:" ) ) {
				const [regionId, floorIndex] = id.slice( "map-floor:".length ).split( ":" ).map( Number );
				if ( Number.isInteger( regionId ) && Number.isInteger( floorIndex ) ) {
					floorChoice = { regionId: regionId!, floorIndex: floorIndex! };
					choiceFromView = !indoors;
				}
				return null;
			}
			return undefined;
		},
		/*
		================
		present

		The strip (level filter and list toggles on the world), the
		dungeon entrances and the follow pin on the world, the dungeon
		page's globe, pin and floor selector in a cave, and the drawer
		when it is open.
		================
		*/
		present( input: MapToolsPaint ): { quads: UiQuad[]; controls: UiControl[]; paths: string[]; } {
			const quads: UiQuad[] = [], controls: UiControl[] = [], paths: string[] = [];
			if ( !input.open ) return { quads, controls, paths };
			const [mx, my, mw] = input.window;
			const text = input.text;
			let stripX = mx + (input.huntingVisible && !input.cave ? 88 : 8);
			if ( input.cave ) {
				// The later client's dungeon chrome (issue #3210): the
				// big globe back to the world beside the follow pin at
				// the map's top right, and the one floor selector at its
				// right edge - one compact stepper on a black plate: up
				// chevron, the floor box, down chevron (issue #3238).
				// The up arrow takes the next floor down: on the first
				// floor the later client greys the down arrow, not the
				// up one, and the ends hold. The page foot carries the
				// monster level-filter note and its checkbox.
				shownFloors = input.floors;
				shownFloor = input.floor;
				paths.push(
					GLOBE,
					PIN_AUTO,
					PIN_PASSIVE,
					FLOOR_UP,
					FLOOR_UP_OFF,
					FLOOR_DOWN,
					FLOOR_DOWN_OFF,
					FLOOR_BOX,
					FLOOR_CHOICE_BOX,
					FLOOR_TRACK,
					CHECK_ON,
					CHECK_OFF
				);
				const clip = input.clip, right = clip[0] + clip[2];
				quads.push( {
					texture: GLOBE,
					rect: [ right - 84, clip[1] + 4, 36, 36 ],
					uv: [ 0, 0, 1, 1 ],
					color: WHITE,
					clip
				} );
				controls.push( {
					id: "map-globe",
					kind: "button",
					label: "World map",
					helpText: "Back to the world map",
					rect: [ right - 84, clip[1] + 4, 36, 36 ]
				} );
				followPin( input, quads, controls );
				const index = input.floors.findIndex( floor => floor === input.floor );
				if ( input.floor && index >= 0 && clip[3] >= 140 ) {
					const sx = right - 32, sy = clip[1] + 48;
					const upOff = index >= input.floors.length - 1, downOff = index <= 0;
					quads.push( {
						texture: FLOOR_TRACK,
						rect: [ sx, sy, 28, 56 ],
						uv: [ 0, 0, 1, 1 ],
						color: WHITE,
						clip
					} );
					quads.push( {
						texture: upOff ? FLOOR_UP_OFF : FLOOR_UP,
						rect: [ sx, sy + 6, 28, 12 ],
						uv: [ 0, 0, 1, 1 ],
						color: WHITE,
						clip
					} );
					controls.push( {
						id: "map-floor-up",
						kind: "button",
						label: "Next floor",
						helpText: "Show the next floor down",
						rect: [ sx, sy + 6, 28, 12 ],
						disabled: upOff
					} );
					quads.push( {
						texture: floorChoice ? FLOOR_CHOICE_BOX : FLOOR_BOX,
						rect: [ sx + 2, sy + 20, 24, 20 ],
						uv: [ 0, 0, 1, 1 ],
						color: WHITE,
						clip
					} );
					quads.push(
						...text.quads(
							"B" + input.floor.layer,
							[ sx + 2, sy + 20, 24, 20 ],
							clip,
							WHITE,
							{ hAlign: 1, vAlign: 1 }
						)
					);
					quads.push( {
						texture: downOff ? FLOOR_DOWN_OFF : FLOOR_DOWN,
						rect: [ sx, sy + 42, 28, 12 ],
						uv: [ 0, 0, 1, 1 ],
						color: WHITE,
						clip
					} );
					controls.push( {
						id: "map-floor-down",
						kind: "button",
						label: "Previous floor",
						helpText: "Show the floor before",
						rect: [ sx, sy + 42, 28, 12 ],
						disabled: downOff
					} );
				}
				// The page foot (issue #3238): the level-filter note in
				// the later client's own wording - its "6 level" is the
				// later client's line, painted as authored; the port's
				// own filter span (#3167) still drives the directory
				// and the tooltip. The filter's checkbox sits at the
				// strip's right end. One switch drives both pages:
				// this box and the world strip's toggle are the same state.
				const footY = input.window[1] + input.window[3] - 24;
				quads.push( {
					texture: "",
					rect: [ mx + 4, footY, mw - 8, 20 ],
					uv: [ 0, 0, 1, 1 ],
					color: [ 0, 0, 0, .92 ],
					clip: input.full
				} );
				quads.push(
					...text.quads(
						"Will show monsters, which level is up to 6 level higher from current characters level",
						[ mx + 12, footY + 2, mw - 52, 16 ],
						input.full,
						WHITE,
						{ hAlign: 1, vAlign: 1 }
					)
				);
				quads.push( {
					texture: levelFilter ? CHECK_ON : CHECK_OFF,
					rect: [ mx + mw - 28, footY + 2, 16, 16 ],
					uv: [ 0, 0, 1, 1 ],
					color: WHITE,
					clip: input.full
				} );
				return { quads, controls, paths };
			}
			// The level filter toggle: the toolkit checkbutton, as the
			// hunting toggle beside it draws.
			paths.push( CHECK_ON, CHECK_OFF );
			quads.push( {
				texture: levelFilter ? CHECK_ON : CHECK_OFF,
				rect: [ stripX, my + 10, 16, 16 ],
				uv: [ 0, 0, 1, 1 ],
				color: WHITE,
				clip: input.full
			} );
			quads.push(
				...text.quads( "Lv filter", [ stripX + 20, my + 10, 56, 17 ], input.full, GOLD, { vAlign: 1 } )
			);
			controls.push( {
				id: "map-level-filter",
				kind: "button",
				label: "Monster level filter",
				helpText: (levelFilter ? "Hide" : "Show") + " only monsters up to " +
					MONSTER_LEVEL_FILTER_SPAN + " levels above you",
				rect: [ stripX - 2, my + 7, 80, 20 ],
				selected: levelFilter
			} );
			stripX += 84;
			// The drawer toggle.
			quads.push(
				...text.quads( "Monsters", [ stripX + 2, my + 10, 64, 17 ], input.full, GOLD, { vAlign: 1 } )
			);
			controls.push( {
				id: "map-list",
				kind: "button",
				label: "Monster list",
				helpText: (drawerOpen ? "Close" : "Open") + " the region and monster list",
				rect: [ stripX, my + 7, 68, 20 ],
				selected: drawerOpen
			} );
			// The follow pin at the map's own corner, as the later
			// client draws it on the world too (issue #3210).
			paths.push( PIN_AUTO, PIN_PASSIVE );
			followPin( input, quads, controls );
			// The dungeon entrances: their authored map points, named.
			const anchors = !input.center ? [] : [ 1, 2 ]
				.map( dungeon => {
					const cave = dungeon === 1 ? 0x8001 : 0x8002;
					return { dungeon, cave, pose: worldMapDungeonPose( cave ) };
				} )
				.filter( ( row ): row is { dungeon: number; cave: number; pose: Pose; } => row.pose !== null );
			for (
				const { point, rect } of !input.center ? [] : worldMapPointMarkers( {
					page: input.page,
					clip: input.clip,
					pan: input.pan,
					center: input.center,
					points: anchors.map( row => row.pose ),
					layout: input.layout
				} )
			) {
				const anchor = anchors.find( row => row.pose === point )!;
				// The door's pose is its overworld point, not a cave
				// region: the name comes from the cave it opens.
				const label = caveDungeonName( anchor.cave )!;
				const width = Math.max( 84, text.run( label, 0, 0 ).width + 16 ),
					left = Math.trunc( rect[0] + rect[2] / 2 - width / 2 ),
					top = rect[1] - 22;
				if (
					left + width < input.clip[0] || left > input.clip[0] + input.clip[2] ||
					top + 20 < input.clip[1] || top > input.clip[1] + input.clip[3]
				) continue;
				paths.push( MAP_SIGN );
				quads.push( {
					texture: MAP_SIGN,
					rect,
					uv: [ 0, 0, 1, 1 ],
					color: WHITE,
					clip: input.clip
				} );
				quads.push(
					...text.quads( label, [ left, top, width, 20 ], input.clip, GOLD, { hAlign: 1, vAlign: 1 } )
				);
				controls.push( {
					id: "map-dungeon:" + anchor.dungeon,
					kind: "button",
					label: label,
					helpText: "Open the " + label + " map",
					rect: [ left, top, width, 20 ]
				} );
				// The entrance's own marker opens it too, and hovering
				// the marker shows the same "Open the ..." popup as the
				// label (issue #3238): the hunt sign is painted at this
				// rect, so the visible marker and its control coincide.
				controls.push( {
					id: "map-dungeon-icon:" + anchor.dungeon,
					kind: "button",
					label: label,
					helpText: "Open the " + label + " map",
					rect
				} );
			}
			if ( drawerOpen ) {
				const dw = DRAWER_WIDTH,
					dx = mx + mw - dw - 6,
					dy = input.clip[1],
					dh = input.clip[3];
				quads.push( {
					texture: "",
					rect: [ dx, dy, dw, dh ],
					uv: [ 0, 0, 1, 1 ],
					color: [ .05, .06, .055, .97 ],
					clip: input.full
				} );
				const cap = filterCap( input.characterLevel );
				const regions = directory( input.catalogue, cap );
				const region = drawerRegion === null ?
					undefined :
					regions.find( row => row.region === drawerRegion );
				const title = region ? regionName( region.region, input.zones ) : "Regions";
				quads.push(
					...text.quads( title, [ dx + 10, dy + 6, dw - 20, 18 ], input.full, GOLD, {
						fontStyle: 2,
						overflow: "clip"
					} )
				);
				if ( region ) {
					controls.push( {
						id: "map-drawer-back",
						kind: "button",
						label: "All regions",
						rect: [ dx + dw - 58, dy + 4, 52, 18 ]
					} );
					quads.push(
						...text.quads( "< Back", [ dx + dw - 58, dy + 6, 52, 18 ], input.full, WHITE, {
							hAlign: 2,
							vAlign: 1
						} )
					);
				}
				const rows = region ?
					region.monsters.map( entry => ({
						id: "map-monster:" + entry.region + ":" + entry.monster.refObjId,
						label: entry.monster.name + "  Lv. " + entry.monster.level,
						selected: mark?.refObjId === entry.monster.refObjId && mark?.regionId === entry.region
					}) ) :
					regions.map( row => ({
						id: "map-region:" + row.region,
						label: regionName( row.region, input.zones ) + "  Lv. " +
							(row.minLevel === row.maxLevel ? row.minLevel : row.minLevel + "-" + row.maxLevel),
						selected: false
					}) );
				const visible = Math.max( 1, Math.floor( (dh - 56) / ROW_HEIGHT ) );
				const pages = Math.max( 1, Math.ceil( rows.length / visible ) );
				if ( drawerPage >= pages ) drawerPage = pages - 1;
				if ( rows.length === 0 ) {
					quads.push(
						...text.quads(
							input.catalogue ? "No monsters" : "Loading...",
							[ dx + 10, dy + 30, dw - 20, 18 ],
							input.full,
							DIM,
							{ overflow: "clip" }
						)
					);
				}
				rows.slice( drawerPage * visible, drawerPage * visible + visible ).forEach( ( row, index ) => {
					const top = dy + 28 + index * ROW_HEIGHT;
					controls.push( {
						id: row.id,
						kind: "button",
						label: row.label,
						rect: [ dx + 4, top, dw - 8, ROW_HEIGHT ],
						selected: row.selected
					} );
					quads.push(
						...text.quads(
							row.label,
							[ dx + 10, top, dw - 16, ROW_HEIGHT ],
							input.full,
							row.selected ? GOLD : WHITE,
							{ vAlign: 1, overflow: "clip" }
						)
					);
				} );
				if ( pages > 1 ) {
					controls.push( {
						id: "map-drawer-prev",
						kind: "button",
						label: "Previous page",
						rect: [ dx + 4, dy + dh - 22, 52, 18 ]
					} );
					controls.push( {
						id: "map-drawer-next",
						kind: "button",
						label: "Next page",
						rect: [ dx + dw - 56, dy + dh - 22, 52, 18 ]
					} );
					quads.push(
						...text.quads( "< Prev", [ dx + 10, dy + dh - 22, 52, 18 ], input.full, WHITE, { vAlign: 1 } ),
						...text.quads( "Next >", [ dx + dw - 62, dy + dh - 22, 52, 18 ], input.full, WHITE, {
							hAlign: 2,
							vAlign: 1
						} ),
						...text.quads(
							(drawerPage + 1) + "/" + pages,
							[ dx + 56, dy + dh - 22, dw - 112, 18 ],
							input.full,
							DIM,
							{ hAlign: 1, vAlign: 1 }
						)
					);
				}
			}
			return { quads, controls, paths };
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( regionLoad.kind === "loading" ) assets.cancel( regionLoad.id );
			regionLoad = { kind: "disposed" };
		}
	};
}
