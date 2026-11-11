/*
===========================================================================

hunting-guide.ts - optional static map atlas and native portrait resources

Port-only. One owner polls the two bounded byte requests and retains the
projection; the ordinary UI texture owner loads only visible still images.
Hidden/disabled maps admit no guide work. No live mobs or gameplay commands.

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import type { Pose } from "@/engine/contracts/gameplay";
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import { HUNTING_GUIDE_BYTES_LIMIT, type HuntingGuideSource } from "@/engine/contracts/hunting-guide";
import { frameParts, frameRing } from "@/engine/foundation/ui/frame-ring";
import type { GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import {
	decodeHuntingGuide,
	decodeHuntingPortraits,
	filterHuntingGuide,
	projectHuntingGuide,
	huntingGuideDetails,
	HUNTING_MONSTER_SIGN,
	HUNTING_PORTRAIT_FRAME,
	HUNTING_PORTRAITS,
	type HuntingCatalogue,
	type HuntingProjection
} from "@/engine/foundation/ui/hunting-guide";
import { worldMapFrame, type WorldMapLayout } from "@/engine/foundation/ui/world-map";
import { featuresAt } from "@/engine/foundation/world/plane";

const EMPTY: HuntingProjection = { quads: [], controls: [], labels: [], paths: [], areas: [] };
const ART_BYTES_LIMIT = 512 * 1024;
const CARD_FRAME = "/assets/images/Media_extracted/interface/frame/mframe_wnd_", CARD_PARTS = frameParts();
const WHITE: [number, number, number, number] = [ 1, 1, 1, 1 ];
/*
================
Load
================
*/
type Load<T> =
	| { kind: "idle"; }
	| { kind: "loading"; id: number; }
	| { kind: "ready"; data: T; }
	| { kind: "failed"; }
	| { kind: "disposed"; };

/*
================
createHuntingGuideHud

Replaced sources cannot publish into a new world. Completions are collected
while hidden; only an open, explicitly enabled map starts either request.
================
*/
export function createHuntingGuideHud(
	assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">,
	base: string
) {
	let source: HuntingGuideSource | undefined, load: Load<HuntingCatalogue> = { kind: "idle" };
	let art: Load<ReadonlyMap<number, string>> = { kind: "idle" }, cacheKey = "", projection = EMPTY;
	/*
	================
	invalidate
	================
	*/
	function invalidate() {
		cacheKey = "";
		projection = EMPTY;
	}
	return {
		/*
		================
		step
		================
		*/
		step( next: HuntingGuideSource | undefined, needed: boolean, artNeeded: boolean = needed ) {
			if ( load.kind === "disposed" ) return false;
			let changed = false;
			if ( next?.url !== source?.url || next?.bytes !== source?.bytes ) {
				if ( load.kind === "loading" ) assets.cancel( load.id );
				if ( !next && art.kind === "loading" ) {
					assets.cancel( art.id );
					art = { kind: "idle" };
				}
				source = next;
				load = { kind: "idle" };
				invalidate();
				changed = true;
			}
			if ( load.kind === "idle" && needed && source && assets.available() > 0 ) {
				load = { kind: "loading", id: assets.request( source.url, HUNTING_GUIDE_BYTES_LIMIT ) };
				changed = true;
			}
			if ( load.kind === "loading" ) {
				const result = assets.take( load.id );
				if ( result ) {
					try {
						if ( result.kind !== "bytes" || result.buffer.byteLength !== source?.bytes ) {
							throw Error( "Hunting guide resource mismatch" );
						}
						load = {
							kind: "ready",
							data: decodeHuntingGuide(
								JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) )
							)
						};
					} catch {
						load = { kind: "failed" };
					}
					invalidate();
					changed = true;
				}
			}
			if ( art.kind === "idle" && artNeeded && source && assets.available() > 0 ) {
				art = {
					kind: "loading",
					id: assets.request( new URL( HUNTING_PORTRAITS, base ).href, ART_BYTES_LIMIT )
				};
				changed = true;
			}
			if ( art.kind === "loading" ) {
				const result = assets.take( art.id );
				if ( result ) {
					try {
						if ( result.kind !== "bytes" || result.buffer.byteLength > ART_BYTES_LIMIT ) {
							throw Error( "Hunting portrait resource mismatch" );
						}
						art = {
							kind: "ready",
							data: decodeHuntingPortraits(
								JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) )
							)
						};
					} catch {
						art = { kind: "failed" };
					}
					invalidate();
					changed = true;
				}
			}
			return changed;
		},
		/*
		================
		present
		================
		*/
		present(
			page: number,
			clip: UiRect,
			pan: readonly [number, number],
			center: Pose,
			hover: string | null = null,
			layout?: WorldMapLayout,
			maxLevel?: number
		) {
			if ( page !== 0 || load.kind !== "ready" || !featuresAt( center.regionId ).worldMap ) {
				invalidate();
				return EMPTY;
			}
			const frame = worldMapFrame( page, clip, pan, center, layout ),
				key = [ page, ...clip, frame.ox, frame.oy, hover ?? "", maxLevel ?? "" ].join( "|" );
			if ( key !== cacheKey ) {
				projection = projectHuntingGuide(
					maxLevel === undefined ? load.data : filterHuntingGuide( load.data, maxLevel ),
					frame,
					clip,
					hover
				);
				cacheKey = key;
			}
			return projection;
		},
		/*
		================
		catalogue

		The decoded catalogue once its bytes land, for the map's own
		monster directory (issue #3167); undefined while it loads.
		================
		*/
		catalogue(): HuntingCatalogue | undefined {
			return load.kind === "ready" ? load.data : undefined;
		},
		/*
		================
		details
		================
		*/
		details( hover: string | null, viewport: UiRect ) {
			return huntingGuideDetails( projection, art.kind === "ready" ? art.data : new Map(), hover, viewport );
		},
		/*
		================
		presentDetails

		The hover card itself: this owner frames its own detail rows on the
		map. The map owns the gating (window, page, the guide toggle); the
		card is appended to the caller's quad/path sinks.
		================
		*/
		presentDetails( input: {
			hover: string | null;
			full: UiRect;
			paths: string[];
			quads: UiQuad[];
			text: {
				quads(
					value: string,
					rect: UiRect,
					clip: UiRect,
					color: UiQuad["color"],
					style?: GlyphStyle
				): UiQuad[];
			};
			resources: { has( path: string ): boolean; size( path: string ): readonly [number, number] | undefined; };
			rect( rect: UiRect, color: UiQuad["color"], texture?: string ): void;
		} ) {
			const detail = huntingGuideDetails(
				projection,
				art.kind === "ready" ? art.data : new Map(),
				input.hover,
				input.full
			);
			if ( !detail ) {
				return;
			}
			const paths = input.paths,
				quads = input.quads,
				text = input.text,
				resources = input.resources,
				rect = input.rect,
				full = input.full;
			paths.push( ...detail.paths );
			const [x, y, width, height] = detail.rect;
			rect( detail.rect, [ .06, .075, .065, .97 ] );
			quads.push(
				...frameRing(
					detail.rect,
					CARD_FRAME,
					CARD_PARTS.map( part => resources.size( CARD_FRAME + part + ".png" ) ),
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
				rect( [ left, top, 32, 32 ], WHITE, HUNTING_PORTRAIT_FRAME );
				rect(
					[ left + 2, top + 2, 28, 28 ],
					WHITE,
					resources.has( row.image ) ? row.image : HUNTING_MONSTER_SIGN
				);
				quads.push(
					...text.quads(
						row.monster.name,
						[ left + 40, top + 1, row.rect[2] - 46, 16 ],
						full,
						WHITE,
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
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( load.kind === "loading" ) assets.cancel( load.id );
			if ( art.kind === "loading" ) assets.cancel( art.id );
			load = art = { kind: "disposed" };
			source = undefined;
			invalidate();
		}
	};
}
