/*
===========================================================================

res-layout.ts - the later client's window layouts, decoded

Pure model of one res_ui layout (scripts/res_ui_layout.py): the
control records a later client's window is authored from, keyed by
their id path below the root ("115/56"). It decodes the format the
asset step publishes (assets/data/res-ui/*.json, issue #4245) and the
texture/frame stems into public image paths; the window instances the
runtime keeps live in runtime/ui/res-layout/res-layout.ts.

Control facts a reader needs first: `rect` is absolute inside the
layout's own canvas, `texture`/`frame` are stems below
interface/... (debug binary extensions stripped at import), `text`
the authored default line, `color` an ARGB u32 (alpha in the top byte)
and `flags`/`rootWord` pass through raw, uninterpreted (the format
owner decodes nothing more; see scripts/res_ui_layout.py).
===========================================================================
*/

// The published texture/frame art lives here; stems stay below
// interface/... (the import strips .ddj extensions and publishes
// every named face with whichever variants the archive holds).
export const RES_LAYOUT_IMAGE_ROOT = "/assets/images/Media_extracted/";

/*
================
ResLayoutControl

One authored control of a window. `key` is its id path below the
root, `parent` the same path of its container (the root's is null).
================
*/
export interface ResLayoutControl {
	readonly key: string;
	readonly id: number;
	readonly parent: string | null;
	readonly children: readonly string[];
	readonly type: number;
	readonly className: string;
	readonly rect: readonly [number, number, number, number];
	readonly frame: string;
	readonly texture: string;
	readonly text: string;
	readonly color: number;
	readonly flags: number;
	readonly rootWord: number;
}

/*
================
ResLayout

One decoded window layout: its root control id and every control by
key, in the published file order (root first, every subtree
parent-before-child). Later code reads the graph only through here.
================
*/
export interface ResLayout {
	readonly format: "res-ui-layout";
	readonly version: number;
	readonly name: string;
	readonly root: ResLayoutControl;
	readonly controls: Readonly<Record<string, ResLayoutControl>>;
	readonly order: readonly string[];
}

/*
================
decodeResLayout
================
*/
export function decodeResLayout( value: unknown ): ResLayout {
	const record = ( v: unknown ): v is Record<string, unknown> =>
		typeof v === "object" && v !== null && !Array.isArray( v );
	const number = ( v: unknown ) => {
		if ( typeof v !== "number" || !Number.isFinite( v ) ) throw Error( "Invalid res layout number" );
		return v;
	};
	const string = ( v: unknown ) => {
		if ( typeof v !== "string" ) throw Error( "Invalid res layout string" );
		return v;
	};
	if ( !record( value ) || value.format !== "res-ui-layout" || value.version !== 1 ) {
		throw Error( "Not a res_ui layout" );
	}
	const name = string( value.name ), rootId = number( value.root );
	if ( !Array.isArray( value.controls ) || !value.controls.length ) {
		throw Error( "Invalid res layout control list" );
	}
	const controls: Record<string, ResLayoutControl> = {};
	const order: string[] = [];
	for ( const raw of value.controls ) {
		if ( !record( raw ) ) throw Error( "Invalid res layout control" );
		const rectValue = raw.rect;
		if ( !Array.isArray( rectValue ) || rectValue.length !== 4 ) {
			throw Error( "Invalid res layout rect" );
		}
		const key = string( raw.key );
		const control: ResLayoutControl = {
			key,
			id: number( raw.id ),
			parent: raw.parent === null ? null : string( raw.parent ),
			children: (raw.children as unknown[]).map( string ),
			type: number( raw.type ),
			className: string( raw.className ),
			rect: [ number( rectValue[0] ), number( rectValue[1] ), number( rectValue[2] ), number( rectValue[3] ) ],
			frame: string( raw.frame ),
			texture: string( raw.texture ),
			text: string( raw.text ),
			color: number( raw.color ),
			flags: number( raw.flags ),
			rootWord: number( raw.rootWord )
		};
		if ( controls[key] ) throw Error( "Duplicate res layout control " + key );
		controls[key] = control;
		order.push( key );
	}
	const root = controls[String( rootId )];
	if ( !root || root.parent !== null ) throw Error( "Invalid res layout root" );
	for ( const control of Object.values( controls ) ) {
		for ( const child of control.children ) {
			if ( controls[child]?.parent !== control.key ) {
				throw Error( "Invalid res layout child link in " + control.key );
			}
		}
		if ( control.parent !== null && !controls[control.parent!]?.children.includes( control.key ) ) {
			throw Error( "Invalid res layout parent link in " + control.key );
		}
	}
	return { format: "res-ui-layout", version: 1, name, root, controls, order };
}

/*
================
resLayoutImagePath

The public image path for a texture/frame stem, or "" for none.
Stems the import publishes never leave interface/... .
================
*/
export function resLayoutImagePath( stem: string ): string {
	if ( !stem ) return "";
	const cleaned = stem.toLowerCase().replace( /\.ddj$|\.png$/, "" ).replaceAll( "\\", "/" );
	if ( !cleaned.startsWith( "interface/" ) ) throw Error( "Foreign res layout texture " + stem );
	return RES_LAYOUT_IMAGE_ROOT + cleaned + ".png";
}

/*
================
resLayoutFrameParts

The nine image parts of a window frame stem: the left/up tile set
the frame ring draws (foundation frame-ring's own order).
================
*/
export function resLayoutFrameParts( frame: string ): readonly string[] {
	if ( !frame ) return [];
	return [ "left_up", "right_up", "right_down", "left_down", "left_side", "mid_up", "right_side", "mid_down" ]
		.map( part => resLayoutImagePath( frame + part ) );
}
