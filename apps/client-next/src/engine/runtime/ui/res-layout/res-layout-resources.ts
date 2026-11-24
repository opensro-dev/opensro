/*
===========================================================================

res-layout-resources.ts - res_ui layout admission and lifetime

Owns the quest window layouts' requests and decoding. The UI calls
step every frame, demand gates admission only, completions are always
collected so a closed window cannot retain the shared asset slots.
Dispose cancels any request this module still owns.
===========================================================================
*/

import { decodeResLayout, type ResLayout } from "@/engine/foundation/ui/res-layout";
import type { AssetOwner } from "@/engine/contracts/assets";
const QUEST_LIST_PATH = "/assets/data/res-ui/questlist.json";
const QUEST_MINI_LIST_PATH = "/assets/data/res-ui/questminilist.json";
const MAX_LAYOUT_BYTES = 1 << 20;
/*
================
Load
================
*/
type Load = { kind: "idle"; } | { kind: "loading"; id: number; } | { kind: "ready"; value: ResLayout; } | {
	kind: "failed";
	message: string;
} | { kind: "disposed"; };
/*
================
createResLayoutResources
================
*/
export function createResLayoutResources(
	assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">,
	base: string
) {
	const paths = [ QUEST_LIST_PATH, QUEST_MINI_LIST_PATH ] as const;
	const states: Load[] = paths.map( () => ({ kind: "idle" }) );
	let error: string | null = null;
	return {
		/*
================
		step
================
		*/
		step( needed = true ) {
			let changed = false;
			for ( let i = 0; i < states.length; i++ ) {
				const s = states[i]!;
				if ( s.kind === "loading" ) {
					const r = assets.take( s.id );
					if ( !r ) continue;
					changed = true;
					try {
						if ( r.kind !== "bytes" ) throw Error( "Res layout unavailable" );
						const value = JSON.parse(
							new TextDecoder( "utf-8", { fatal: true } ).decode( r.buffer )
						);
						states[i] = { kind: "ready", value: decodeResLayout( value ) };
					} catch ( e ) {
						states[i] = { kind: "failed", message: String( e ) };
						error = String( e );
					}
				} else if ( needed && s.kind === "idle" && assets.available() > 0 ) {
					states[i] = { kind: "loading", id: assets.request( new URL( paths[i]!, base ).href, MAX_LAYOUT_BYTES ) };
				}
			}
			return changed;
		},
		/*
================
		data
================
		*/
		data(): { questList: ResLayout; questMiniList: ResLayout; } | null {
			const [list, mini] = states;
			if ( list?.kind !== "ready" || mini?.kind !== "ready" ) return null;
			return { questList: list.value, questMiniList: mini.value };
		},
		/*
================
		error
================
		*/
		error(): string | null {
			return error;
		},
		/*
================
		dispose
================
		*/
		dispose() {
			for ( const s of states ) if ( s.kind === "loading" ) assets.cancel( s.id );
			states.fill( { kind: "disposed" } );
		}
	};
}
