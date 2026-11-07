/*
===========================================================================

minimap.ts - the minimap's catalogues: NPC positions and dungeon floors

Catalog jobs share the UI lifetime and the existing asset scheduler. The
later floor catalogue holds the Ch'in Tomb art the v1.150 archives lack
(import_vsro_tomb_minimap.py); a build without it leaves those floors empty.

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import { type CaveMapCatalog, caveMapCatalog } from "@/engine/foundation/ui/cave-map";
import { minimapNpcPositions } from "@/engine/foundation/ui/minimap-catalog";
import {
	minimapArt,
	minimapDungeonRooms,
	minimapDungeons,
	minimapWithLaterFloors
} from "@/engine/foundation/ui/minimap-tiles";

const RETAIL_FLOOR_CATALOG = "/assets/data/mission-dungeon-minimap.json";
const LATER_FLOOR_CATALOG = "/assets/data/later-dungeon-minimap.json";
const CAVE_MAP_CATALOG = "/assets/data/later-dungeon-map.json";
const CATALOG_BYTE_LIMIT = 4 << 20;

type MinimapAssets = Pick<AssetOwner, "available" | "request" | "take" | "cancel">;

/*
================
createMinimapResources
================
*/
export function createMinimapResources( assets: MinimapAssets, base: string ) {
	const dungeon = createDungeonResources( assets, base, RETAIL_FLOOR_CATALOG ),
		laterDungeon = createDungeonResources( assets, base, LATER_FLOOR_CATALOG );
	const caveMaps = createCaveMapResources( assets, base );
	let state:
		| { kind: "idle"; }
		| { kind: "loading"; id: number; }
		| {
			kind: "ready";
			positions: ReturnType<typeof minimapNpcPositions>;
		}
		| { kind: "failed"; error: string; }
		| { kind: "disposed"; } = { kind: "idle" };
	return {
		step( needed: boolean, worldVisible = false ) {
			const retailChanged = dungeon.step( worldVisible ),
				laterChanged = laterDungeon.step( worldVisible ),
				caveChanged = caveMaps.step( worldVisible ),
				changed = retailChanged || laterChanged || caveChanged;
			if ( state.kind === "idle" && needed && assets.available() > 0 ) {
				state = {
					kind: "loading",
					id: assets.request( new URL( "/assets/data/npcpos.json", base ).href, CATALOG_BYTE_LIMIT )
				};
			}
			if ( state.kind === "loading" ) {
				const result = assets.take( state.id );
				if ( result ) {
					try {
						if ( result.kind !== "bytes" ) {
							throw Error( result.kind === "error" ? result.error : "Invalid minimap catalog response" );
						}
						state = {
							kind: "ready",
							positions: minimapNpcPositions(
								JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) )
							)
						};
					} catch ( error ) {
						state = { kind: "failed", error: String( error ) };
					}
					return true;
				}
			}
			return changed;
		},
		positions: () => state.kind === "ready" ? state.positions : undefined,
		caveMaps: caveMaps.data,
		dungeons() {
			const retail = dungeon.data(), extra = laterDungeon.data();
			return retail && extra ? minimapWithLaterFloors( retail, extra ) : retail;
		},
		rooms: dungeon.rooms,
		art() {
			const retail = dungeon.art(), extra = laterDungeon.art();
			return retail && extra ? new Map( [ ...retail, ...extra ] ) : retail;
		},
		// A build without later tiles is not an error: those floors stay empty.
		error: () => state.kind === "failed" ? state.error : dungeon.error(),
		dispose() {
			dungeon.dispose();
			laterDungeon.dispose();
			caveMaps.dispose();
			if ( state.kind === "loading" ) assets.cancel( state.id );
			state = { kind: "disposed" };
		}
	};
}

/*
================
createCaveMapResources

The M window's dungeon pages. A build without them keeps the world map.
================
*/
function createCaveMapResources( assets: MinimapAssets, base: string ) {
	let state: "idle" | "loading" | "done" | "disposed" = "idle", id = 0, data: CaveMapCatalog | undefined;
	return {
		step( needed: boolean ) {
			if ( state === "idle" && needed && assets.available() > 0 ) {
				id = assets.request( new URL( CAVE_MAP_CATALOG, base ).href, CATALOG_BYTE_LIMIT );
				state = "loading";
			}
			if ( state !== "loading" ) return false;
			const result = assets.take( id );
			if ( !result ) return false;
			state = "done";
			try {
				if ( result.kind === "bytes" ) {
					data = caveMapCatalog(
						JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) )
					);
				}
			} catch {
				data = undefined;
			}
			return true;
		},
		data: () => data,
		dispose() {
			if ( state === "loading" ) assets.cancel( id );
			state = "disposed";
			data = undefined;
		}
	};
}

/*
================
createDungeonResources

One floor catalogue: its floors and the art paths they resolve to.
================
*/
function createDungeonResources( assets: MinimapAssets, base: string, catalog: string ) {
	let state: "idle" | "loading" | "ready" | "failed" | "disposed" = "idle",
		id = 0,
		data: ReturnType<typeof minimapDungeons> | undefined,
		error: string | null = null;
	let art: ReturnType<typeof minimapArt> | undefined;
	let rooms: ReturnType<typeof minimapDungeonRooms> | undefined;
	return {
		step( needed: boolean ) {
			if ( state === "idle" && needed && assets.available() > 0 ) {
				id = assets.request( new URL( catalog, base ).href, CATALOG_BYTE_LIMIT );
				state = "loading";
			}
			if ( state === "loading" ) {
				const result = assets.take( id );
				if ( result ) {
					try {
						if ( result.kind !== "bytes" ) {
							throw Error( result.kind === "error" ? result.error : "Invalid minimap response" );
						}
						const value = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) ),
							nextArt = minimapArt( value ),
							nextData = minimapDungeons( value ),
							nextRooms = minimapDungeonRooms( value );
						art = nextArt;
						data = nextData;
						rooms = nextRooms;
						state = "ready";
					} catch ( e ) {
						error = String( e );
						state = "failed";
					}
					return true;
				}
			}
			return false;
		},
		data: () => data,
		rooms: () => rooms,
		art: () => art,
		error: () => error,
		dispose() {
			if ( state === "loading" ) assets.cancel( id );
			state = "disposed";
			data = undefined;
			art = undefined;
			rooms = undefined;
		}
	};
}
