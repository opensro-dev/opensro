/*
===========================================================================

resources.ts - title metadata admission and collection across screen changes

Visible title rendering admits work; the UI frame collects it even after
world entry so completed metadata cannot retain shared asset slots.

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import type { FontAtlas } from "@/engine/foundation/rendering/ui-glyphs";
import { decodeUiFont } from "@/engine/foundation/rendering/ui-glyphs";
import type { TitleLayout } from "./internal/title-contract";
const TITLE_RESOURCE_BYTES = 4 << 20;
/*
================
createTitleResources
================
*/
export function createTitleResources(
	assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">,
	base: string
) {
	// 0x748630 loads pstitle.txt; its EUROPE_SYSTEM branch differs from the
	// separate legacy pstitle_europe.txt (which includes Korean rating badges).
	const paths = [
		"/assets/cif/layouts/pstitle.json",
		"/assets/fonts/native-ui-font-atlas.json",
		"/assets/text/textuisystem.en.json",
		"/assets/cif/layouts/pscharacterselect_europe.json",
		"/assets/cif/layouts/pscharactercreate_europe.json",
		"/assets/cif/layouts/pscharactercreatechina.json"
	];
	const jobs = new Map<number, number>(), values: unknown[] = [];
	let error: string | null = null, disposed = false;
	return {
		/*
		================
		step

		Demand gates new requests only, never collection of admitted work.
		================
		*/
		step( needed = true ) {
			if ( disposed ) return false;
			let changed = false;
			for ( const [index, id] of jobs ) {
				const result = assets.take( id );
				if ( !result ) continue;
				jobs.delete( index );
				changed = true;
				try {
					if ( result.kind !== "bytes" ) throw Error( "Retail title resource unavailable: " + paths[index] );
					const value: unknown = JSON.parse(
						new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer )
					);
					values[index] = index === 1 ? decodeUiFont( value ) : value;
				} catch ( reason ) {
					error = String( reason );
				}
			}
			if ( needed && !error ) {
				for ( let i = 0; i < paths.length && assets.available() > 0; i++ ) {
					if ( !values[i] && !jobs.has( i ) ) {
						jobs.set( i, assets.request( new URL( paths[i]!, base ).href, TITLE_RESOURCE_BYTES ) );
					}
				}
			}
			return changed;
		},
		/*
		================
		data
		================
		*/
		data() {
			return values[0] && values[1] && values[2] && values[3] ?
				{
					creation: [ values[4], values[5] ] as (TitleLayout | undefined)[],
					nodes: (values[0] as TitleLayout).controlsByName,
					dockNodes: (values[3] as TitleLayout).controlsByName,
					dockSections: (values[3] as TitleLayout).sections,
					font: values[1] as FontAtlas,
					text: (values[2] as { entries: Record<string, string>; }).entries
				} :
				null;
		},
		/*
		================
		error
		================
		*/
		error: () => error,
		/*
		================
		dispose
		================
		*/
		dispose() {
			disposed = true;
			for ( const id of jobs.values() ) assets.cancel( id );
			jobs.clear();
			values.length = 0;
		}
	};
}
