/*
===========================================================================

map-catalogues.ts - the M window's two catalogues: region names and the guide

Owns the loads the world map's tools read while the M window is open: the
regioncode names (the region pages and zone groups) and the v1.696 field
guide catalogue (the monster drawer, the guide tabs and the level filter).
Map tools (map-tools.ts) asks for them and reads their state; it never
touches the asset owner itself.

Both are interface-lane loads: a player waits on them in an open window,
so they always have a slot of their own and never queue behind world
streaming (foundation/assets/asset-slots.ts, #3977).

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import {
	WORLDMAP_GUIDE_CATALOG,
	worldMapGuideCatalog,
	type WorldMapGuideCatalog
} from "@/engine/foundation/ui/worldmap-guide";

const REGIONCODE_PATH = "/assets/text/regioncode.json";
const GUIDE_BYTE_LIMIT = 4 << 20;

/*
================
RegionNamesLoad
================
*/
export type RegionNamesLoad =
	| { kind: "idle"; }
	| { kind: "loading"; id: number; }
	| { kind: "ready"; names: ReadonlyMap<number, string>; }
	| { kind: "failed"; }
	| { kind: "disposed"; };

/*
================
GuideLoad

The guide catalogue's load. A build without it (no pinned v1.696
client) answers 404 and the tabs fall back to the shard's catalogue.
================
*/
export type GuideLoad =
	| { kind: "idle"; }
	| { kind: "loading"; id: number; }
	| { kind: "ready"; catalog: WorldMapGuideCatalog; }
	| { kind: "absent"; }
	| { kind: "disposed"; };

/*
================
createMapCatalogues
================
*/
export function createMapCatalogues( options: {
	assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">;
	base: string;
} ) {
	const assets = options.assets;
	let regionLoad: RegionNamesLoad = { kind: "idle" };
	let guideLoad: GuideLoad = { kind: "idle" };
	return {
		/*
		================
		step

		Drives both loads while the map is open: the region names always,
		the guide only once something wants it (the drawer or the level
		filter). True when a load landed and the frame should redraw.
		================
		*/
		step( mapOpen: boolean, guideWanted: boolean ): boolean {
			if ( regionLoad.kind === "disposed" ) return false;
			let changed = false;
			if ( mapOpen && guideWanted && guideLoad.kind === "idle" && assets.available( "interface" ) > 0 ) {
				guideLoad = {
					kind: "loading",
					id: assets.request(
						new URL( WORLDMAP_GUIDE_CATALOG, options.base ).href,
						GUIDE_BYTE_LIMIT,
						undefined,
						{ lane: "interface" }
					)
				};
			}
			if ( guideLoad.kind === "loading" ) {
				const result = assets.take( guideLoad.id );
				if ( result ) {
					try {
						if ( result.kind !== "bytes" ) throw Error( "no worldmap guide in this build" );
						guideLoad = {
							kind: "ready",
							catalog: worldMapGuideCatalog(
								JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) )
							)
						};
					} catch ( error ) {
						// The tabs keep the shard's catalogue; the warning
						// names why, so a stale tree names its rebuild.
						console.warn( "[SRO map guide] " + (error instanceof Error ? error.message : String( error )) );
						guideLoad = { kind: "absent" };
					}
					changed = true;
				}
			}
			if ( mapOpen && regionLoad.kind === "idle" && assets.available( "interface" ) > 0 ) {
				regionLoad = {
					kind: "loading",
					id: assets.request( new URL( REGIONCODE_PATH, options.base ).href, undefined, undefined, {
						lane: "interface"
					} )
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
		regions

		The region names' load; a new object only when its state changes, so
		a reader may key a cache on it.
		================
		*/
		regions(): RegionNamesLoad {
			return regionLoad;
		},
		/*
		================
		guide
		================
		*/
		guide(): GuideLoad {
			return guideLoad;
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( regionLoad.kind === "loading" ) assets.cancel( regionLoad.id );
			if ( guideLoad.kind === "loading" ) assets.cancel( guideLoad.id );
			regionLoad = { kind: "disposed" };
			guideLoad = { kind: "disposed" };
		}
	};
}
