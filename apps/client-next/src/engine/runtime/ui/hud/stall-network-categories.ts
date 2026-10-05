/*
===========================================================================

stall-network-categories.ts - the stall network's category tree on demand

The v1.150 client reads fmncategorytreedata.txt at startup; the browser
client fetches it the first time the stall network window opens, so a
HUD without the published table still loads. step() is called every frame;
a request is made only while needed, and a completion is always collected.

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import {
	decodeStallNetworkCategories,
	type StallNetworkCategory
} from "@/engine/foundation/ui/stall-network-categories";

const CATEGORY_URL = "/assets/textdata/fmncategorytreedata.txt";
// The table is about 6 KB; the cap only bounds a wrong response.
const CATEGORY_MAX_BYTES = 1 << 20;

type CategoryLoad =
	| { readonly kind: "idle"; }
	| { readonly kind: "loading"; readonly id: number; }
	| { readonly kind: "ready"; readonly roots: readonly StallNetworkCategory[]; }
	| { readonly kind: "failed"; readonly message: string; };

/*
================
createStallNetworkCategories
================
*/
export function createStallNetworkCategories(
	assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">,
	base: string
) {
	let load: CategoryLoad = { kind: "idle" };
	return {
		/*
		================
		step

		True when the tree became ready or failed this step.
		================
		*/
		step( needed: boolean ): boolean {
			if ( load.kind === "loading" ) {
				const result = assets.take( load.id );
				if ( !result ) return false;
				try {
					if ( result.kind !== "bytes" ) throw Error( "Stall network categories unavailable" );
					load = { kind: "ready", roots: decodeStallNetworkCategories( result.buffer ) };
				} catch ( error ) {
					load = { kind: "failed", message: String( error ) };
				}
				return true;
			}
			if ( load.kind === "idle" && needed && assets.available() ) {
				load = {
					kind: "loading",
					id: assets.request( new URL( CATEGORY_URL, base ).href, CATEGORY_MAX_BYTES )
				};
			}
			return false;
		},
		/*
		================
		roots

		The decoded tree, or null while it loads or after it failed.
		================
		*/
		roots(): readonly StallNetworkCategory[] | null {
			return load.kind === "ready" ? load.roots : null;
		},
		/*
		================
		error
		================
		*/
		error(): string | null {
			return load.kind === "failed" ? load.message : null;
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( load.kind === "loading" ) assets.cancel( load.id );
			load = { kind: "idle" };
		}
	};
}
