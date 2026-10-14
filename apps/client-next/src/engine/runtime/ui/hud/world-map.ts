/*
===========================================================================

world-map.ts - world map navigation and presentation

Owns map placement, page, follow mode and the optional hunting guide. The UI
calls its explicit input, frame and shutdown entry points; map details never
become additional state in createUi. Native marker order stays here beside
the map projection. Hunting guidance is port-only, not native.

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import type { Pose } from "@/engine/contracts/gameplay";
import type { UiControl, UiEvent, UiQuad, UiRect, UiView } from "@/engine/contracts/ui";
import type { AuthoredControl, AuthoredLayout } from "@/engine/foundation/ui/authored-layout";
import type { GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import type { MapLabel, MapIcon } from "@/engine/foundation/ui/world-map";
import type { minimapNpcPositions } from "@/engine/foundation/ui/minimap-catalog";
import type { ExperimentalOptions } from "@/engine/foundation/ui/experimental-options";
import { frameParts, frameRing } from "@/engine/foundation/ui/frame-ring";
import { panelCloseId } from "@/engine/foundation/ui/panels";
import { minimapSameFloor } from "@/engine/foundation/ui/minimap-floor";
import { rosterPositions } from "@/engine/foundation/ui/minimap-markers";
import {
	worldMapDemand,
	MAP_LOCAL_MARKER,
	worldMapPageAt,
	worldMapPages,
	worldMapPresentation,
	worldMapPointMarkers,
	REVERSE_MAP_MARKER,
	type MapMarker,
	mapLabelVisible
} from "@/engine/foundation/ui/world-map";
import {
	HUNTING_AREA_PREFIX,
	HUNTING_MONSTER_SIGN,
	HUNTING_PORTRAIT_FRAME
} from "@/engine/foundation/ui/hunting-guide";
import { createHuntingGuideHud } from "./hunting-guide";

const WORLD_MAP_WIDTH = 652, WORLD_MAP_HEIGHT = 424;
const ROOT = "/assets/images/Media_extracted/";
const FRAME = ROOT + "interface/frame/mframe_wnd_", PARTS = frameParts();
const white = [ 1, 1, 1, 1 ] as const;

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
	hover: string | null;
	hudData: { map: AuthoredLayout; mapLabels: readonly MapLabel[]; mapIcons: readonly MapIcon[]; } | null | undefined;
	trackedQuest: number;
	minimapResources: { positions(): ReturnType<typeof minimapNpcPositions> | undefined; };
	mapTeleport: { view( page: number, clip: UiRect, pan: readonly [number, number], center: Pose ): void; };
	reverseScrollHud: { mapping(): boolean; };
	text: MapText;
	resources: MapImages;
	paths: string[];
	quads: UiQuad[];
	controls: UiControl[];
	hudCopy: ( key: string ) => string;
	beginWindow: () => readonly [number, number, number, number];
	endWindow: ( mark: readonly [number, number, number, number] ) => void;
	windowBox: ( title: string, x: number, y: number, width: number, height: number ) => void;
	authoredButton: ( node: AuthoredControl, x: number, y: number, id: string, label: string ) => void;
	authoredLabeledButton: MapPaint["authoredButton"];
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
	let mapSmall = false, mapFollow = true, mapPage = 0, mapX = 100, mapY = 100;
	let mapPan: [number, number] = [ 0, 0 ], mapCenter: Pose | null = null;
	let guideAvailable = false;
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
			mapFollow = true;
			mapSmall = false;
			mapPan = [ 0, 0 ];
			mapCenter = null;
		},
		/*
		================
		follow
		================
		*/
		follow() {
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
			// 57F43D swallows city buttons in AUTO. The port allows the click and
			// selects Manual Movement State, preserving its existing behavior.
			if ( id.startsWith( "map-town:" ) ) {
				const page = Number( id.slice( 9 ) );
				if ( worldMapPages().some( p => p.id === page ) ) {
					mapPage = page;
					mapFollow = false;
					mapPan = [ 0, 0 ];
					mapCenter = pose ? { ...pose } : null;
				}
				return true;
			}
			// 57AEA0 resizes, then 57A570 recentres in either mode.
			if ( id === "map-size" ) {
				mapSmall = !mapSmall;
				mapPan = [ 0, 0 ];
				mapCenter = mapFollow ? null : pose ? { ...pose } : null;
				return true;
			}
			// 575E90 flips AUTO MOVE only; keep the last automatic centre.
			if ( id === "map-follow" ) {
				mapFollow = !mapFollow;
				mapCenter = pose ? { ...pose } : null;
				mapPan = [ 0, 0 ];
				return true;
			}
			// 57F410 selects the world and Manual Movement State.
			if ( id === "map-world" ) {
				mapPage = 0;
				mapFollow = false;
				mapCenter = pose ? { ...pose } : null;
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
			return id === "map-pan" || (guideAvailable && id.startsWith( HUNTING_AREA_PREFIX ));
		},
		/*
		================
		drag
		================
		*/
		drag( event: Extract<UiEvent, { kind: "drag"; }> ) {
			if ( event.id === "map-drag" ) {
				mapX = Math.max( 0, mapX + event.dx );
				mapY = Math.max( 0, mapY + event.dy );
			} else if (
				(event.id === "map-pan" || event.id.startsWith( "map-town:" ) ||
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
			guideAvailable = source !== undefined;
			const page = mapFollow && next.gameplay?.pose ? worldMapPageAt( next.gameplay.pose ) : mapPage;
			return huntingGuide.step(
				source,
				panel === "Map" && page === 0 && experimental.state().saved.monsterGuide
			);
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
				authoredLabeledButton = input.authoredLabeledButton,
				closeButton = input.closeButton,
				image = input.image;

			let hover = input.hover;
			// The current map is a world-lifetime resource, not a popup-lifetime
			// resource. Closing M must not release the image and flash on the next open.
			if ( mapFollow && pose ) {
				mapPage = worldMapPageAt( pose );
				mapPan = [ 0, 0 ];
				mapCenter = null;
			}
			const mapWidth = mapSmall ? 268 : WORLD_MAP_WIDTH, mapHeight = mapSmall ? 296 : WORLD_MAP_HEIGHT;
			const mapLeft = Math.min( mapX, Math.max( 0, w - mapWidth ) ),
				mapTop = Math.min( mapY, Math.max( 0, h - mapHeight ) );
			mapX = mapLeft;
			mapY = mapTop;
			if ( mapPage !== 0 && hover?.startsWith( HUNTING_AREA_PREFIX ) ) hover = null;
			const mapGuideEnabled = guideAvailable && experimental.state().saved.monsterGuide;
			const mapClip: UiRect = [
				mapLeft + 6,
				mapTop + 34,
				mapWidth - 12,
				mapHeight - 40
			];
			const mapHits: UiControl[] = [];
			// 57FE60's marker passes, in its order: quest NPCs (57B1C0), hunting points
			// (57B550), then the apprenticeship and party rosters (57CE80). Each is a
			// 16x16 quad centred on the projected world position; the world map has no
			// 47px ring, so unlike the minimap there is no edge clamp and no arrow
			// sprite swap, and off-page members are simply clipped by the window.
			const mapMarkers: MapMarker[] = [];
			if ( pose ) {
				for ( const id of game?.quests?.find( q => q.refId === trackedQuest )?.targetIds ?? [] ) {
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
				for ( const row of rosterPositions( pose, game ?? null, next.entities ) ) {
					mapMarkers.push( {
						regionId: row.regionId,
						x: row.x,
						z: row.z,
						rotation: 0,
						path: ROOT + "interface/worldmap/wmap_sign_" + row.kind + ".png"
					} );
				}
			}
			// A closed map only demands its images, so opening it is instant: the
			// demand is computed without the projection (worldMapDemand). Its label
			// glyphs (the largest allocation of a HUD step) and click hits are built
			// while it is open. The font atlas is demanded either way.
			const mapOpen = panel === "Map";
			// The teleport's inverse pick must track the painted projection: it
			// is only valid while the map is open and a pose exists, so record the
			// frame under the same condition that builds the projection.
			if ( mapOpen && pose ) {
				mapTeleport.view(
					mapPage,
					mapClip,
					mapPan,
					mapCenter ?? pose
				);
			}
			const mapProjection = pose && mapOpen ?
				worldMapPresentation(
					pose,
					mapPage,
					mapClip,
					mapPan,
					mapCenter ?? pose,
					hudData?.mapLabels,
					hudData?.mapIcons,
					mapMarkers
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
			const mapGuide = mapOpen && mapGuideEnabled && pose ?
				huntingGuide.present( mapPage, mapClip, mapPan, mapCenter ?? pose, hover ) :
				null;
			const fontPath = text.path();
			if ( !mapOpen && fontPath ) paths.push( fontPath );
			const mapImages = mapProjection ?
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
					...mapProjection.markers
				] :
				[];
			for ( const { icon, rect: r } of mapOpen ? mapProjection?.hits ?? [] : [] ) {
				const page = worldMapPages().find( p => p.id === icon.destination );
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
			// Port-only: the reverse return map points while the map holds a scroll.
			if ( mapOpen && pose && reverseScrollHud.mapping() ) {
				const clip: UiRect = [ mapLeft + 6, mapTop + 34, mapWidth - 12, mapHeight - 40 ];
				for (
					const { point, rect } of worldMapPointMarkers( {
						page: mapPage,
						clip,
						pan: mapPan,
						center: mapCenter ?? pose,
						points: game?.reverseMapPoints ?? []
					} )
				) {
					mapImages.push( {
						rect,
						texture: REVERSE_MAP_MARKER,
						uv: [ 0, 0, 1, 1 ],
						color: [ 1, 1, 1, 1 ],
						clip
					} );
					mapHits.push( {
						id: "reverse-map-point:" + point.id,
						label: point.name,
						rect,
						kind: "button"
					} );
				}
			}
			if ( mapOpen ) {
				paths.push(
					...(mapGuide?.paths ?? []),
					...mapImages.map( q => q.texture ).filter( Boolean )
				);
			} else if ( pose ) {
				worldMapDemand(
					mapPage,
					mapClip,
					mapPan,
					mapCenter ?? pose,
					hudData?.mapIcons ?? [],
					paths
				);
				for ( const row of mapMarkers ) paths.push( row.path );
				paths.push( MAP_LOCAL_MARKER );
			}
			if ( panel === "Map" && hudData ) {
				// Map tiles and icons stream in as a pan reveals them (demanded above, and
				// absent textures simply do not draw). Gating admission on them disabled
				// map-pan mid-drag, which cancelled the drag. Chrome stays gated.
				const admission = beginWindow();
				const mw = mapWidth, mh = mapHeight, mx = mapLeft, my = mapTop;
				if ( mapFollow && pose ) {
					mapPage = worldMapPageAt( pose );
					mapPan = [ 0, 0 ];
					mapCenter = null;
				}
				const page = worldMapPages().find( row => row.id === mapPage ),
					caption = hudCopy( page ? "UIIT_STT_" + page.name : "UIIT_PAG_WORLDMAP" ) || "World Map";
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
				if ( guideAvailable && mapPage === 0 ) {
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
				if ( mapPage !== 0 ) {
					authoredButton(
						{ ...nodes.GDR_WM_BTN_TO_WMAP!, rect: [ mw - 62, 10, 16, 16 ] },
						mx,
						my,
						"map-world",
						"World map"
					);
				}
				if ( !mapSmall ) {
					const node = nodes.GDR_WM_BTN_AUTO_MOVE!,
						caption = hudCopy(
							mapFollow ? "UIIT_STT_WORLDMAP_AUTO_MOVE" : "UIIT_STT_WORLDMAP_MANUAL_MOVE"
						);
					authoredLabeledButton( node, mx, my, "map-follow", caption );
				}
				closeButton( mx + mw - 25, my + 10, panelCloseId( "Map" ) );
				endWindow( admission );
			}
			return hover;
		},
		/*
================
details
================
		*/
		details( input: MapDetails ) {
			const worldVisible = input.worldVisible,
				panel = input.panel,
				pressed = input.pressed,
				hover = input.hover,
				full = input.full,
				paths = input.paths,
				quads = input.quads,
				text = input.text,
				resources = input.resources,
				rect = input.rect;

			if (
				guideAvailable && worldVisible && panel === "Map" && mapPage === 0 &&
				experimental.state().saved.monsterGuide && !pressed
			) {
				const detail = huntingGuide.details( hover, full );
				if ( detail ) {
					paths.push( ...detail.paths );
					const [x, y, width, height] = detail.rect;
					rect( detail.rect, [ .06, .075, .065, .97 ] );
					quads.push(
						...frameRing(
							detail.rect,
							FRAME,
							PARTS.map( part => resources.size( FRAME + part + ".png" ) ),
							full
						)
					);
					quads.push(
						...text.quads( detail.title, [ x + 10, y + 11, width - 20, 18 ], full, [ .96, .83, .55, 1 ], {
							hAlign: 1,
							fontStyle: 2
						} )
					);
					for ( const row of detail.rows ) {
						const [left, top] = row.rect;
						rect( [ left, top, 32, 32 ], white, HUNTING_PORTRAIT_FRAME );
						rect(
							[ left + 2, top + 2, 28, 28 ],
							white,
							resources.has( row.image ) ? row.image : HUNTING_MONSTER_SIGN
						);
						quads.push(
							...text.quads(
								row.monster.name,
								[ left + 40, top + 1, row.rect[2] - 46, 16 ],
								full,
								white,
								{ overflow: "clip" }
							),
							...text.quads(
								"Lv. " + row.monster.level,
								[ left + 40, top + 17, row.rect[2] - 46, 14 ],
								full,
								[ .85, .75, .52, 1 ]
							)
						);
					}
					quads.push(
						...text.quads( detail.caption, [ x + 10, y + height - 23, width - 20, 16 ], full, [
							.72,
							.7,
							.63,
							1
						], { hAlign: 1 } )
					);
				}
			}
		},
		/*
================
dispose
================
		*/
		dispose() {
			huntingGuide.dispose();
		}
	};
}
