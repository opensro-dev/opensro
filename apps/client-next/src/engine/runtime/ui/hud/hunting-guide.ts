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
import type { UiRect } from "@/engine/contracts/ui";
import { HUNTING_GUIDE_BYTES_LIMIT, type HuntingGuideSource } from "@/engine/contracts/hunting-guide";
import {
	decodeHuntingGuide,
	decodeHuntingPortraits,
	projectHuntingGuide,
	huntingGuideDetails,
	HUNTING_PORTRAITS,
	type HuntingCatalogue,
	type HuntingProjection
} from "@/engine/foundation/ui/hunting-guide";
import { worldMapFrame } from "@/engine/foundation/ui/world-map";

const EMPTY: HuntingProjection = { quads: [], controls: [], labels: [], paths: [], areas: [] };
const ART_BYTES_LIMIT = 512 * 1024;
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
			if ( art.kind === "idle" && needed && source && assets.available() > 0 ) {
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
			hover: string | null = null
		) {
			if ( page !== 0 || load.kind !== "ready" || center.regionId & 0x8000 ) {
				invalidate();
				return EMPTY;
			}
			const frame = worldMapFrame( page, clip, pan, center ),
				key = [ page, ...clip, frame.ox, frame.oy, hover ?? "" ].join( "|" );
			if ( key !== cacheKey ) {
				projection = projectHuntingGuide(
					load.data,
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
		details
		================
		*/
		details( hover: string | null, viewport: UiRect ) {
			return huntingGuideDetails( projection, art.kind === "ready" ? art.data : new Map(), hover, viewport );
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
