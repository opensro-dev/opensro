/*
===========================================================================

authored-layout.ts - immutable controls decoded from native resource sections

Numeric IDs distinguish controls even when authored names repeat.

===========================================================================
*/
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
/*
================
AuthoredControl
================
*/
export interface AuthoredControl {
	readonly name: string;
	readonly id: number;
	readonly type: string;
	readonly rect: UiRect;
	readonly client: UiRect;
	readonly uv: UiRect;
	readonly texture: string;
	readonly size: readonly [number, number];
	readonly text: string;
	readonly color: UiQuad["color"];
	readonly fontIndex: number;
	readonly hAlign: number;
	readonly vAlign: number;
	readonly creationSection?: number;
}
/*
================
AuthoredLayout
================
*/
export type AuthoredLayout = Readonly<Record<string, AuthoredControl>>;
// Admit only the immutable presentation fields. Never copy a foreign control
// object or interpret its debug Color as texture modulation.
/*
================
decodeAuthoredLayout
================
*/
export function decodeAuthoredLayout( value: unknown, sections?: readonly string[] ): AuthoredLayout {
	const record = ( v: unknown ): v is Record<string, unknown> =>
		typeof v === "object" && v !== null && !Array.isArray( v );
	const number = ( v: unknown ) => {
		if ( typeof v !== "number" || !Number.isFinite( v ) ) throw Error( "Invalid authored UI number" );
		return v;
	};
	const channel = ( v: unknown ) => {
		const n = number( v );
		if ( !Number.isSafeInteger( n ) ) throw Error( "Invalid authored color byte" );
		return (n & 255) / 255;
	};
	const string = ( v: unknown ) => {
		if ( typeof v !== "string" ) throw Error( "Invalid authored UI string" );
		return v;
	};
	const rect = ( v: unknown ): UiRect => {
		if ( !record( v ) ) throw Error( "Invalid authored UI rectangle" );
		return [ number( v.x ), number( v.y ), number( v.width ), number( v.height ) ];
	};
	if ( !record( value ) || !record( value.controlsByName ) ) throw Error( "Invalid authored UI layout" );
	let nodes = value.controlsByName;
	const creationSections = new Map<string, number>();
	if ( sections ) {
		if ( !Array.isArray( value.sections ) ) throw Error( "Missing authored sections" );
		nodes = {};
		for ( const [index, name] of sections.entries() ) {
			const section: unknown = value.sections.find( s => record( s ) && s.name === name );
			if ( !record( section ) || !Array.isArray( section.nodes ) ) {
				throw Error( "Missing authored section " + name );
			}
			for ( const node of section.nodes ) {
				if ( !record( node ) || typeof node.name !== "string" ) throw Error( "Invalid section node" );
				const previous = nodes[node.name];
				if ( record( previous ) && previous.id !== node.id ) {
					const preserved = node.name + "#" + previous.id;
					nodes[preserved] = previous;
					creationSections.set( preserved, creationSections.get( node.name ) ?? index );
				}
				delete nodes[node.name];
				nodes[node.name] = node;
				creationSections.set( node.name, index );
			}
		}
	}
	const out: Record<string, AuthoredControl> = {};
	for ( const [name, node] of Object.entries( nodes ) ) {
		if ( !record( node ) || !record( node.properties ) ) throw Error( "Invalid authored UI control" );
		const properties = node.properties;
		const point = ( key: string ) => {
			const property = properties[key];
			if ( !record( property ) || !record( property.value ) ) throw Error( "Missing authored UV" );
			return [ number( property.value.x ), number( property.value.y ) ] as const;
		};
		const lt = point( "UV_LT" ), rb = point( "UV_RB" ), ddj = node.ddj, color = node.fontColor;
		const texture = record( ddj ) ? string( ddj.publicPath ) : "";
		if ( texture && !texture.startsWith( "/assets/images/" ) ) throw Error( "Foreign UI texture" );
		if ( !record( color ) ) throw Error( "Missing authored font color" );
		out[name] = {
			name,
			id: number( node.id ),
			type: string( node.type ),
			rect: rect( node.rect ),
			client: rect( node.clientRect ),
			uv: [ lt[0], lt[1], rb[0] - lt[0], rb[1] - lt[1] ],
			texture,
			size: record( ddj ) && ddj.width !== undefined ? [ number( ddj.width ), number( ddj.height ) ] : [ 0, 0 ],
			text: string( node.text ?? "" ),
			color: [ channel( color.r ), channel( color.g ), channel( color.b ), channel( color.a ) ],
			fontIndex: number( node.fontIndex ),
			hAlign: number( node.hAlign ),
			vAlign: number( node.vAlign ),
			...(sections ? { creationSection: creationSections.get( name )! } : {})
		};
	}
	return out;
}
/*
================
authoredRect
================
*/
export function authoredRect( node: AuthoredControl, x: number, y: number ): UiRect {
	return [ x + node.rect[0], y + node.rect[1], node.rect[2] || node.size[0], node.rect[3] || node.size[1] ];
}
/*
================
authoredClientRect
================
*/
export function authoredClientRect( node: AuthoredControl, x: number, y: number ): UiRect {
	const r = authoredRect( node, x, y ), c = node.client;
	return [ r[0] + c[0], r[1] + c[1], r[2] - c[0] - c[2], r[3] - c[1] - c[3] ];
}
// 783F80 creates each section separately. Reverse the resource list within a
// creation call, preserving the order of later calls (58DE80 -> 58DEE0).
/*
================
authoredPaintOrder
================
*/
export function authoredPaintOrder( layout: AuthoredLayout ): AuthoredControl[] {
	return Object.values( layout ).reverse().sort( ( a, b ) => (a.creationSection ?? 0) - (b.creationSection ?? 0) );
}
