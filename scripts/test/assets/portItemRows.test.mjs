/*
===========================================================================

portItemRows.test.mjs - the port-only pet gear rows follow their template

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";

import { completeItemReferenceProjection } from "../../build/shared/itemTextCompletions.mjs";
import { PORT_ITEM_TEMPLATE, PORT_ITEMS, PORT_PET_DROP_MODEL } from "../../build/shared/portItemRows.mjs";

// A template row long enough to carry the icon and stack columns.
function templateRow() {
	const columns = Array.from( { length: 64 }, () => "0" );
	columns[0] = "1";
	columns[1] = "5882";
	columns[2] = PORT_ITEM_TEMPLATE;
	columns[54] = "item\\etc\\e051021_redpearl.ddj";
	columns[53] = "item/etc/drop_ch_acc.bsr";
	columns[57] = "1000";
	return columns.join( "\t" );
}

test("each itemdata projection appends the pet gear after its template once, in both encodings", () => {
	const other = "1\t42\tITEM_OTHER\tOther";
	for ( const encoding of [ "utf8", "utf16le" ] ) {
		const source = Buffer.from( "﻿" + other + "\r\n" + templateRow() + "\r\n" + other + "\r\n", encoding );
		const result = completeItemReferenceProjection( source );
		const records = result.toString( encoding ).split( "\r\n" );
		assert.equal( records.length, 4 + PORT_ITEMS.length );
		PORT_ITEMS.forEach( ( item, i ) => {
			const columns = records[2 + i].split( "\t" );
			assert.equal( columns[1], String( item.id ) );
			assert.equal( columns[2], item.codename );
			assert.equal( columns[5], "SN_" + item.codename );
			assert.equal( columns[54], item.icon );
			assert.equal( columns[53], PORT_PET_DROP_MODEL );
			assert.equal( columns[57], "1" );
			assert.equal( columns[15], item.seal ? "2" : "0" );
			assert.equal( columns[61], String( (item.degree - 1) * 3 + (item.seal || 1) ) );
		} );
		assert.equal( records[1], templateRow() );
		assert.deepEqual( completeItemReferenceProjection( result ), result );
	}
});

test("a projection without the template is unchanged", () => {
	const source = Buffer.from( "1\t42\tITEM_OTHER\tOther\r\n", "utf8" );
	assert.deepEqual( completeItemReferenceProjection( source ), source );
});

test("all108 pet items share slot-and-degree icons across ordinary and sealed references", () => {
	assert.equal( PORT_ITEMS.length, 108 );
	assert.equal( new Set( PORT_ITEMS.map( item => item.id ) ).size, 108 );
	assert.equal( new Set( PORT_ITEMS.map( item => item.icon ) ).size, 27 );
	for ( const item of PORT_ITEMS ) {
		assert.match( item.description, /Up to [1-6] blues/ );
		assert.equal(
			item.icon,
			`item\\pet\\${item.slot.toLowerCase()}_${String( item.degree ).padStart( 2, "0" )}.ddj`
		);
	}
});
