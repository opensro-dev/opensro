/*
===========================================================================

hunting-guide.ts - optional M-map atlas resource, filters and retained projection

Port-only, not native. Owns one polled asset request, immutable decoded data
and local search/level drafts. It never sends gameplay commands or polls mobs.

===========================================================================
*/

import type { AssetOwner } from "@/engine/contracts/assets";
import type { Pose } from "@/engine/contracts/gameplay";
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import { HUNTING_GUIDE_BYTES_LIMIT, type HuntingGuideSource } from "@/engine/contracts/hunting-guide";
import {
	decodeHuntingGuide,
	projectHuntingGuide,
	huntingLevelColors,
	type HuntingCatalogue,
	type HuntingProjection
} from "@/engine/foundation/ui/hunting-guide";
import { worldMapFrame } from "@/engine/foundation/ui/world-map";

const SEARCH_LIMIT = 64;
const MAX_LEVEL = 255;
const EMPTY: HuntingProjection = { quads: [], controls: [], matches: 0 };

/*
================
GuideLoad
================
*/
type GuideLoad = { kind: "idle"; } | { kind: "loading"; id: number; } | { kind: "ready"; data: HuntingCatalogue; } | {
	kind: "failed";
} | { kind: "disposed"; };

/*
================
createHuntingGuideHud

Cancelled/replaced sources cannot publish into a new world. Completions are
collected while hidden; only an open, explicitly enabled map admits work.
================
*/
export function createHuntingGuideHud( assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel"> ) {
	let source: HuntingGuideSource | undefined, load: GuideLoad = { kind: "idle" };
	let search = "", minimum = "", maximum = "", cacheKey = "", projection = EMPTY;
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
		step( next: HuntingGuideSource | undefined, needed: boolean ) {
			if ( load.kind === "disposed" ) return false;
			let changed = false;
			if ( next?.url !== source?.url || next?.bytes !== source?.bytes ) {
				if ( load.kind === "loading" ) assets.cancel( load.id );
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
			return changed;
		},
		/*
		================
		type
		================
		*/
		type( id: string, value: string ) {
			if ( id === "map-hunting-search" ) search = value.slice( 0, SEARCH_LIMIT );
			else if ( id === "map-hunting-min" ) minimum = value.replace( /[^0-9]/g, "" ).slice( 0, 3 );
			else if ( id === "map-hunting-max" ) maximum = value.replace( /[^0-9]/g, "" ).slice( 0, 3 );
			invalidate();
		},
		/*
		================
		reset
		================
		*/
		reset() {
			search = minimum = maximum = "";
			invalidate();
		},
		/*
		================
		present

		Key the retained result by actual clamped map geometry and filter draft.
		================
		*/
		present( page: number, clip: UiRect, pan: readonly [number, number], center: Pose ) {
			if ( load.kind !== "ready" || center.regionId & 0x8000 ) return EMPTY;
			const frame = worldMapFrame( page, clip, pan, center ),
				key = [ page, ...clip, frame.ox, frame.oy, search, minimum, maximum ].join( "|" );
			if ( key !== cacheKey ) {
				projection = projectHuntingGuide(
					load.data,
					{
						search,
						min: minimum ? Math.min( MAX_LEVEL, Number( minimum ) ) : 1,
						max: maximum ? Math.min( MAX_LEVEL, Number( maximum ) ) : MAX_LEVEL
					},
					frame,
					clip
				);
				cacheKey = key;
			}
			return projection;
		},
		/*
		================
		toolbar

		Three compact fields, color bands and honest resource/filter status.
		================
		*/
		toolbar( box: UiRect ) {
			const [x, y, width] = box, white = [ .94, .9, .78, 1 ] as const;
			const quads: UiQuad[] = [], fields: UiControl[] = [], controls: UiControl[] = [];
			const labels: { value: string; rect: UiRect; color: UiQuad["color"]; }[] = [];
			quads.push( { rect: box, clip: box, texture: "", uv: [ 0, 0, 1, 1 ], color: [ .07, .09, .08, 1 ] } );
			labels.push( {
				value: "Monster guide · approximate areas",
				rect: [ x + 4, y, width - 8, 14 ],
				color: white
			} );
			// Leave the native Auto/Manual button's authored row unobstructed.
			const searchWidth = width - 130, fieldY = y + 40;
			for (
				const [id, name, value, left, size, limit] of [
					[ "map-hunting-search", "Search monsters", search, x + 4, searchWidth, SEARCH_LIMIT ],
					[ "map-hunting-min", "Min", minimum, x + searchWidth + 10, 36, 3 ],
					[ "map-hunting-max", "Max", maximum, x + searchWidth + 52, 36, 3 ]
				] as const
			) {
				const rect: UiRect = [ left, fieldY, size, 20 ];
				quads.push( { rect, clip: box, texture: "", uv: [ 0, 0, 1, 1 ], color: [ .15, .18, .15, 1 ] } );
				fields.push( {
					id,
					label: name,
					value,
					rect: [ left + 3, fieldY + 3, size - 6, 14 ],
					kind: "text",
					maxLength: limit
				} );
			}
			const resetRect: UiRect = [ x + width - 34, fieldY, 30, 20 ];
			quads.push( { rect: resetRect, clip: box, texture: "", uv: [ 0, 0, 1, 1 ], color: [ .22, .26, .19, 1 ] } );
			labels.push( { value: "×", rect: resetRect, color: white } );
			controls.push( {
				id: "map-hunting-reset",
				label: "Reset guide filters",
				helpText: "Reset guide filters",
				kind: "button",
				rect: resetRect
			} );
			for ( const [index, band] of huntingLevelColors().entries() ) {
				const left = x + 4 + index * 57;
				quads.push( {
					rect: [ left, y + 66, 7, 7 ],
					clip: box,
					texture: "",
					uv: [ 0, 0, 1, 1 ],
					color: band.color
				} );
				labels.push( { value: band.label, rect: [ left + 10, y + 63, 45, 14 ], color: white } );
			}
			const status = !source ? "Guide unavailable on this server" : load.kind === "failed" ?
				"Guide unavailable" :
				load.kind !== "ready" ?
				"Loading locations…" :
				Number( minimum ) > (maximum ? Number( maximum ) : MAX_LEVEL) ?
				"Min level exceeds max" :
				projection.matches === 0 ?
				"No species match these filters" :
				`${projection.matches} species · hover an area`;
			labels.push( { value: status, rect: [ x + 4, y + 77, width - 8, 14 ], color: [ .7, .75, .67, 1 ] } );
			return { quads, fields, controls, labels };
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( load.kind === "loading" ) assets.cancel( load.id );
			load = { kind: "disposed" };
			source = undefined;
			invalidate();
		}
	};
}
