/*
===========================================================================

unique-nests.ts - loads the world map's unique nest table (port-only)

Port-only, not native. One bounded byte request, started only while the M
window is open with the Experimental row on; the decoded markers are kept
for the session. A failed load stays failed: the map simply draws no
nest signs.

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import type { MapMarker } from "@/engine/foundation/ui/world-map";
import { decodeUniqueNests, UNIQUE_NESTS_BYTES_LIMIT, UNIQUE_NESTS_PATH } from "@/engine/foundation/ui/unique-nests";

const NONE: readonly MapMarker[] = [];

/*
================
Load
================
*/
type Load =
	| { kind: "idle"; }
	| { kind: "loading"; id: number; }
	| { kind: "ready"; nests: readonly MapMarker[]; }
	| { kind: "failed"; }
	| { kind: "disposed"; };

/*
================
createUniqueNestsHud
================
*/
export function createUniqueNestsHud(
	assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">,
	base: string
) {
	let load: Load = { kind: "idle" };
	return {
		/*
		================
		step

		True when the table arrived (or failed), so the open map repaints.
		================
		*/
		step( needed: boolean ) {
			if ( load.kind === "idle" && needed && assets.available() > 0 ) {
				load = {
					kind: "loading",
					id: assets.request( new URL( UNIQUE_NESTS_PATH, base ).href, UNIQUE_NESTS_BYTES_LIMIT )
				};
			}
			if ( load.kind !== "loading" ) return false;
			const result = assets.take( load.id );
			if ( !result ) return false;
			try {
				if ( result.kind !== "bytes" || result.buffer.byteLength > UNIQUE_NESTS_BYTES_LIMIT ) {
					throw Error( "Unique nest resource mismatch" );
				}
				load = {
					kind: "ready",
					nests: decodeUniqueNests(
						JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) )
					)
				};
			} catch {
				load = { kind: "failed" };
			}
			return true;
		},
		/*
		================
		nests
		================
		*/
		nests(): readonly MapMarker[] {
			return load.kind === "ready" ? load.nests : NONE;
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( load.kind === "loading" ) assets.cancel( load.id );
			load = { kind: "disposed" };
		}
	};
}
