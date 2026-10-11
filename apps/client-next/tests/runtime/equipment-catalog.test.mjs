import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { loadEquipmentRecords, visualSocket } from "../../../../scripts/build/char/equipmentVisualRecords.mjs";
import { publicRoot, retailTextdataRoot } from "../../../../scripts/build/world/paths.mjs";
const rows = loadEquipmentRecords( retailTextdataRoot );
const roster = JSON.parse( fs.readFileSync( path.join( publicRoot, "assets/char/roster.json" ), "utf8" ) );

test("every enabled reference has an exact native equipment visual classification and resource provenance", () => {
	const counts = { CH: 0, EU: 0, empty: 0, linked: 0, visual: 0 };
	for ( const row of rows.values() ) {
		const record = roster.dress.equipment[row.id];
		assert.ok( record, `unclassified ${row.id} ${row.code}` );
		assert.equal( record.slot, row.slot );
		assert.equal( record.model, row.resolvedModel );
		assert.equal( record.source, row.modelSource );
		if ( row.slot === null && record.avatarSlot === undefined ) continue;
		counts.visual++;
		if ( row.resolvedModel === null ) counts.empty++;
		if ( row.modelSource !== null && row.modelSource !== row.id ) counts.linked++;
		for ( const race of [ "CH", "EU" ] ) {
			for ( const sex of [ "M", "W" ] ) {
				const body = `${race}_${sex}`,
					allowed = (row.country === 3 || row.country === (race === "CH" ? 0 : 1)) &&
						(row.sex === 2 || row.sex === (sex === "M" ? 1 : 0));
				assert.equal( Object.hasOwn( record.bodies, body ), allowed, `${row.code} ${body}` );
				if ( !allowed ) continue;
				const entry = record.bodies[body];
				if ( !row.resolvedModel ) {
					assert.equal( entry, null );
					continue;
				}
				counts[race]++;
				// An animated resource draws through its own handle (#730).
				assert.ok( (entry?.auxiliary ?? entry)?.parts.length, `${row.code} ${body} has no parts` );
				assert.ok( fs.existsSync( path.join( publicRoot, entry.glb ) ), `${row.code} missing ${entry.glb}` );
			}
		}
	}
	assert.ok( counts.CH > 1000 && counts.EU > 1000 );
	console.log( "equipment census", counts );
});
test("all six armor families share native visual slots; jewelry and ammunition have no visual socket", () => {
	for ( const family of [ 1, 2, 3, 9, 10, 11 ] ) {
		assert.deepEqual( [ 1, 2, 3, 4, 5, 6 ].map( part => visualSocket( [ 3, 1, family, part ] ) ), [
			0,
			2,
			1,
			4,
			3,
			5
		] );
	}
	assert.equal( visualSocket( [ 3, 3, 4, 1 ] ), null );
	assert.equal( visualSocket( [ 3, 1, 5, 1 ] ), null );
});
test("White Twin Horn Crown is a valid head reference; supplied male and female resources are both empty", () => {
	const crown = rows.get( 347 ), linked = [ ...rows.values() ].find( r => r.code === crown.linkedCode );
	assert.equal( crown.slot, 0 );
	assert.equal( crown.model, null );
	assert.equal( linked.model, null );
	assert.equal( roster.dress.equipment[347].bodies.CH_M, null );
	assert.ok( roster.dress.equipment[311].bodies.CH_M, "separate helmet resource must remain available" );
});
