/*
===========================================================================

stall-network-categories.ts - the stall network's search category tree

textdata/fmncategorytreedata.txt (UTF-16) rows are [service][codename]
[label key][parent codename, xxx for a root][category id][degrees]: the
roots fill CIFStallNetwork's large-category combo (id 41), their
children the medium one (id 42), and a child with degrees offers them in
the small one (id 43). The search names the child's category id (0x76F9).

===========================================================================
*/

const ROOT_PARENT = "xxx";
const SERVICE_ON = "1";

/*
================
StallNetworkCategory
================
*/
export interface StallNetworkCategory {
	readonly codename: string;
	readonly label: string;
	readonly id: number;
	readonly degrees: number;
	readonly children: readonly StallNetworkCategory[];
}

/*
================
decodeStallNetworkCategories

The roots in table order with their children. A child naming an unknown
parent, or a malformed row, is a broken table.
================
*/
export function decodeStallNetworkCategories( bytes: ArrayBuffer ): readonly StallNetworkCategory[] {
	const raw = new Uint8Array( bytes );
	const encoding = raw[0] === 0xff && raw[1] === 0xfe || raw[1] === 0 ? "utf-16le" : "utf-8";
	const text = new TextDecoder( encoding, { fatal: true } ).decode( raw ).replace( /^﻿/, "" );
	const roots: { codename: string; label: string; children: StallNetworkCategory[]; }[] = [];
	for ( const line of text.split( /\r?\n/ ) ) {
		if ( !line.trim() || line.startsWith( "//" ) ) continue;
		const fields = line.split( "\t" );
		if ( fields.length < 6 ) throw Error( "Invalid stall network category row" );
		if ( fields[0] !== SERVICE_ON ) continue;
		const [, codename, label, parent] = fields as [string, string, string, string];
		const id = Number( fields[4] ), degrees = Number( fields[5] );
		if ( !Number.isInteger( id ) || !Number.isInteger( degrees ) || id < 0 || degrees < 0 ) {
			throw Error( "Invalid stall network category numbers" );
		}
		if ( parent === ROOT_PARENT ) {
			roots.push( { codename, label, children: [] } );
			continue;
		}
		const root = roots.find( row => row.codename === parent );
		if ( !root ) throw Error( "Stall network category has no parent: " + codename );
		root.children.push( { codename, label, id, degrees, children: [] } );
	}
	return roots.map( row => ({
		codename: row.codename,
		label: row.label,
		id: 0,
		degrees: 0,
		children: row.children
	}) );
}
