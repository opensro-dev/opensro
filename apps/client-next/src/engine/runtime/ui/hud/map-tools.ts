/*
===========================================================================

map-tools.ts - the world map's tools and the state its chrome shows

The M window is the v1.188 client's (res_ui/worldmapguide.2dt, drawn by
map-chrome.ts): the follow pin and the globe at the map's top right,
the floor spinner on a dungeon page, the foot with the monster level
note and its checkbox, and the side tab that opens the guide panel at
the window's left, whose Monster tab lists the regions and their
species (issue #3167's list, the shown floor's on a dungeon page,
issue #3239). Port-only, on by default: the named, clickable dungeon
entrances on the world page. One owner for that state; the map HUD
keeps the window, the pages and the cave view itself. Monster levels
and anchors come from the hunting catalogue the session publishes;
dungeon pages from the cave catalogue; region names from the published
regioncode table read through the zone text.

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import type { Pose } from "@/engine/contracts/gameplay";
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import type { GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import {
	caveDungeonFloors,
	cavePageCenter,
	type CaveMapCatalog,
	type CaveMapPage
} from "@/engine/foundation/ui/cave-map";
import {
	MONSTER_LEVEL_FILTER_SPAN,
	dungeonDirectory,
	monsterDirectory,
	type HuntingCatalogue,
	type MonsterDirectoryRegion
} from "@/engine/foundation/ui/hunting-guide";
import {
	DONWHANG_CAVE_DUNGEON,
	TOMB_FIRST_DUNGEON,
	worldMapDungeonOf,
	worldMapDungeonPose,
	worldMapPointMarkers,
	type WorldMapLayout
} from "@/engine/foundation/ui/world-map";
import { featuresAt } from "@/engine/foundation/world/plane";
import { dungeonRegionId } from "@/engine/foundation/world/world-space";
import { GUIDE_SCROLL_ID, paintGuideTree, type GuideTreeRow } from "./map-guide-tree";
import {
	GUIDE_WIDTH,
	floorSpinner,
	guideLeft,
	followPin,
	globeButton,
	guidePanel,
	mapFoot,
	sideTab,
	windowCorners,
	type MapChromePaint
} from "./map-chrome";
// The window's size and panel for the map HUD, which places the window.
export { MAP_WINDOW_EXTENT, MAP_WINDOW_HEIGHT, MAP_WINDOW_WIDTH, mapLeftReach, mapPanel } from "./map-chrome";

// The level filter note's UI text key: the later client's words for
// the foot checkbox (the dungeon catalogue already carries them as
// its monsterLevelNote, issue #3238).
const LEVEL_FILTER_NOTE_KEY = "UIIT_STT_WORLDMAP_LEVEL_MONSTER_LIST";
const REGIONCODE_PATH = "/assets/text/regioncode.json";
const WHITE = [ 1, 1, 1, 1 ] as const;
const GOLD = [ .96, .83, .55, 1 ] as const;
// The world map's dungeon doors: the dungeon each opens and the v1.150
// object text that names it. The doors themselves are the client's own
// xy_dungeon icons (worldmap_localinfo rows 92033 and 92032).
const DUNGEON_DOORS = [
	{ dungeon: DONWHANG_CAVE_DUNGEON, name: "SN_ZONE_23002" },
	{ dungeon: TOMB_FIRST_DUNGEON, name: "SN_ZONE_23001" }
] as const;

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
	// Whether the shard publishes the hunting catalogue at all
	// (SRO_HUNTING_GUIDE): without it the guide waits on nothing.
	guidePublished: boolean;
	characterLevel: number | undefined;
	zones: Readonly<Record<string, string>> | undefined;
	// The client's object and UI text (textdataname, textuisystem).
	strings: Readonly<Record<string, string>> | undefined;
	// The cave catalogue's foot note, undefined without the catalogue.
	levelNote: string | undefined;
	// The screen rects of the icons the world page drew (its click hits).
	icons: readonly UiRect[];
	text: MapToolsText;
	page: number;
	pan: readonly [number, number];
	center: Pose | null;
	layout?: WorldMapLayout;
	// A published image's size, for the guide panel's frames and tiles.
	size( path: string ): readonly [number, number] | undefined;
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
the world map's dungeon (every Ch'in Tomb floor is one), else the
region itself.
================
*/
function dungeonKey( regionId: number ): number {
	return worldMapDungeonOf( regionId ) ?? regionId;
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
	// The side tab's guide panel, and the tab it shows.
	let drawerOpen = false;
	let guideTab: "area" | "monster" = "monster";
	// The guide tree's open group (one at a time) and its first shown row.
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
	caveDrawer

	The guide's monster list on a dungeon page (issue #3239): the
	shown floor's species from the catalogue's dungeon anchors. The
	level filter caps it as it caps the world list.
	================
	*/
	function caveDrawer( input: MapToolsPaint, paint: MapChromePaint ) {
		const floor = input.floor;
		if ( !floor ) return;
		const region = input.catalogue ?
			dungeonDirectory( input.catalogue, floor.regionId, filterCap( input.characterLevel ) ) :
			undefined;
		guideTree(
			input,
			paint,
			(region?.monsters ?? []).map( entry =>
				speciesRow( "map-cave-monster:" + entry.region + ":", entry, input.characterLevel )
			)
		);
	}

	/*
	================
	speciesRow

	A species row under its group: "Lv21 Megalopa", its lamp lit while
	the map marks it, red when it stands above the character's level
	(map1 shows Lv25 in red under a Lv24 character's Lv21-25 list; the
	exact rule is not read from the binary, so above-own-level is an
	inference).
	================
	*/
	function speciesRow(
		prefix: string,
		entry: { region: number; monster: { refObjId: number; name: string; level: number; }; },
		characterLevel: number | undefined
	): GuideTreeRow {
		return {
			id: prefix + entry.monster.refObjId,
			label: "Lv" + entry.monster.level + " " + entry.monster.name,
			group: false,
			lamp: mark?.refObjId === entry.monster.refObjId && mark?.regionId === entry.region,
			color: characterLevel !== undefined && entry.monster.level > characterLevel ? "high" : "leaf"
		};
	}

	/*
	================
	guideTree

	Paints the rows on the open guide panel and keeps the clamped scroll.
	================
	*/
	function guideTree( input: MapToolsPaint, paint: MapChromePaint, rows: GuideTreeRow[], empty?: string ) {
		drawerPage = paintGuideTree(
			{
				panel: [ guideLeft( input ), input.window[1], GUIDE_WIDTH, 0 ],
				full: input.full,
				rows,
				scroll: drawerPage,
				empty: empty ?? (
					input.catalogue ?
						"No monsters" :
						input.guidePublished ?
						"Loading..." :
						"No monster guide on this server"
				),
				text: input.text,
				size: input.size
			},
			paint
		);
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
			const anchor = dungeonRegionId( dungeonView );
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
			if ( id === "map-side-tab" ) {
				drawerOpen = !drawerOpen;
				drawerRegion = null;
				drawerPage = 0;
				return null;
			}
			if ( id === "map-guide-tab:area" || id === "map-guide-tab:monster" ) {
				guideTab = id === "map-guide-tab:area" ? "area" : "monster";
				drawerRegion = null;
				drawerPage = 0;
				return null;
			}
			// The guide panel's and the foot's surfaces only keep clicks
			// off the world.
			if ( id === "map-guide" || id === "map-foot" ) return null;
			if ( id === GUIDE_SCROLL_ID + "-up" || id === GUIDE_SCROLL_ID + "-down" ) {
				// The paint clamps the far end against the rows.
				drawerPage = Math.max( 0, drawerPage + (id === GUIDE_SCROLL_ID + "-down" ? 1 : -1) );
				return null;
			}
			if ( id.startsWith( "map-region:" ) ) {
				// A group's toggle opens it in place and shuts the one
				// open before (map1 shows a single open group).
				const region = Number( id.slice( "map-region:".length ) );
				if ( Number.isInteger( region ) ) drawerRegion = drawerRegion === region ? null : region;
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
			if ( id.startsWith( "map-cave-monster:" ) ) {
				// A species picked on a dungeon page marks where it
				// lives on that page; the view stays on the floor.
				const [regionId, refObjId] = id.slice( "map-cave-monster:".length ).split( ":" ).map( Number );
				if (
					regionId === undefined || refObjId === undefined ||
					!Number.isInteger( regionId ) || !Number.isInteger( refObjId ) || !catalogue
				) return null;
				const entry = dungeonDirectory( catalogue, regionId, filterCap( characterLevel ) )
					?.monsters.find( row => row.monster.refObjId === refObjId );
				if ( entry ) mark = { regionId, x: entry.x, z: entry.z, refObjId };
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

		The v1.188 window's chrome (map-chrome.ts) with this frame's
		state: the corner and panel edges, the globe off the world page,
		the follow pin, the floor spinner on a dungeon page, the named
		dungeon entrances on the world page, the foot with the level
		filter, and the side tab with the guide panel when it is open.
		================
		*/
		present( input: MapToolsPaint ): { quads: UiQuad[]; controls: UiControl[]; paths: string[]; } {
			const quads: UiQuad[] = [], controls: UiControl[] = [], paths: string[] = [];
			if ( !input.open ) return { quads, controls, paths };
			const text = input.text;
			const paint: MapChromePaint = { window: input.window, full: input.full, quads, controls, paths };
			windowCorners( paint );
			if ( input.cave || input.page !== 0 ) globeButton( paint );
			followPin( paint, input.followed );
			if ( input.cave ) {
				shownFloors = input.floors;
				shownFloor = input.floor;
				const index = input.floors.findIndex( floor => floor === input.floor );
				if ( input.floor && index >= 0 ) {
					floorSpinner( paint, text, {
						label: "B" + input.floor.layer,
						upOff: index >= input.floors.length - 1,
						downOff: index <= 0
					} );
				}
			}
			// The dungeon entrances: their authored map points, named by
			// the client's own text. A door whose name the text lacks
			// stays the plain icon.
			const anchors: { dungeon: number; label: string; pose: Pose; }[] = [];
			for ( const door of input.center && !input.cave ? DUNGEON_DOORS : [] ) {
				const label = input.strings?.[door.name];
				const pose = worldMapDungeonPose( dungeonRegionId( door.dungeon ) );
				if ( label && pose ) anchors.push( { dungeon: door.dungeon, label, pose } );
			}
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
				const label = anchor.label;
				const width = Math.max( 84, text.run( label, 0, 0 ).width + 16 ),
					left = Math.trunc( rect[0] + rect[2] / 2 - width / 2 ),
					top = rect[1] - 22;
				if (
					left + width < input.clip[0] || left > input.clip[0] + input.clip[2] ||
					top + 20 < input.clip[1] || top > input.clip[1] + input.clip[3]
				) continue;
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
				// The entrance's own icon opens it too, with the label's
				// popup (issue #3238). The icon is the one the world page
				// drew at the door's point (the client's xy_dungeon row),
				// so the control covers what the player sees; the door's
				// point rect stands in when no icon is drawn there.
				const cx = rect[0] + rect[2] / 2, cy = rect[1] + rect[3] / 2;
				const icon = input.icons.find( r => cx >= r[0] && cx < r[0] + r[2] && cy >= r[1] && cy < r[1] + r[3] );
				controls.push( {
					id: "map-dungeon-icon:" + anchor.dungeon,
					kind: "button",
					label: label,
					helpText: "Open the " + label + " map",
					rect: icon ?? rect
				} );
			}
			mapFoot( paint, text, {
				note: input.levelNote ?? input.strings?.[LEVEL_FILTER_NOTE_KEY],
				checked: levelFilter
			} );
			sideTab( paint, drawerOpen );
			if ( drawerOpen ) {
				guidePanel( paint, { tab: guideTab, size: input.size } );
				if ( guideTab === "area" ) {
					// The Field tree (area.txt) is the guide import's
					// next step; the tab stands with an empty list.
					guideTree( input, paint, [], "" );
				} else if ( input.cave ) {
					caveDrawer( input, paint );
				} else {
					const rows: GuideTreeRow[] = [];
					for ( const region of directory( input.catalogue, filterCap( input.characterLevel ) ) ) {
						const open = region.region === drawerRegion;
						rows.push( {
							id: "map-region:" + region.region,
							label: regionName( region.region, input.zones ) + "  Lv." + region.minLevel + "~" +
								region.maxLevel,
							group: true,
							open,
							color: "group"
						} );
						if ( !open ) continue;
						for ( const entry of region.monsters ) {
							rows.push( speciesRow( "map-monster:" + entry.region + ":", entry, input.characterLevel ) );
						}
					}
					guideTree( input, paint, rows );
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
