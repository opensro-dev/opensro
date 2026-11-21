/*
===========================================================================

vfx-proved-data.test.mjs - tests for effects.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import { parseSkillAniSet, buildEffectRecordTable } from "../../../../scripts/build/char/parseSkillEffect.mjs";
import { loadCharacterActionEffectRows } from "../../../../scripts/build/data/buildSkillDataAsset.mjs";

const { createEffectDecoder } = await import(
	sourceFileUrl( "src/engine/runtime/assets/worker/effects/effects.ts" ).href
);
function fixture( run ) {
	const dir = fs.mkdtempSync( path.join( os.tmpdir(), "vfx-proved-data-" ) );
	try {
		run( dir );
	} finally {
		fs.rmSync( dir, { recursive: true, force: true } );
	}
}
function row( service = 1 ) {
	return [
		service,
		"audit",
		"AUDIT_SKILL",
		7,
		"FALSE",
		1,
		"DEFAULT",
		...Array( 6 ).fill( "ANI_ATTACK1,ANI_ATTACK2" ),
		"defense.efp",
		"attack.efp",
		0,
		"0,0,0,0",
		"ONE",
		"none",
		"critical.efp",
		"none",
		"none",
		"none",
		"none",
		"none",
		0,
		1
	];
}
function write( dir, lines ) {
	const file = path.join( dir, "skilleffect.txt" );
	fs.writeFileSync(
		file,
		"#section skillaniset2\n" + lines.map( r => r.join( "\t" ) ).join( "\n" ) + "\n",
		"utf16le"
	);
	return file;
}
test("proved 91DAE0 admits any nonzero service integer and rejects zero", () =>
	fixture( dir => {
		for ( const enabled of [ 0, 1, 2, -1 ] ) {
			assert.equal( parseSkillAniSet( write( dir, [ row( enabled ) ] ) ).has( "AUDIT_SKILL" ), enabled !== 0 );
		}
	} ));
test("all six native animation tables and separate resource slots survive build and worker decode", () =>
	fixture( dir => {
		const file = write( dir, [ row() ] );
		fs.writeFileSync(
			path.join( dir, "SkillData_5000.txt" ),
			"1\t77\t0\tAUDIT_SKILL_01\t0\tAUDIT_SKILL\n",
			"utf16le"
		);
		const table = buildEffectRecordTable( dir, file ).table, record = table[77];
		assert.ok( record );
		for ( let i = 0; i < 6; i++ ) {
			assert.equal( record["animCount" + i], 2 );
			assert.deepEqual( record["animTable" + i], [ "ANI_ATTACK1", "ANI_ATTACK2" ] );
		}
		assert.equal( record.flags02, 1 );
		assert.equal( record.byteBe, 1, "+BE is the final column, independently of +BD=0" );
		const decoder = createEffectDecoder();
		const value = decoder.decode( new TextEncoder().encode( JSON.stringify( table ) ) )[77];
		assert.deepEqual(
			value.phaseClips,
			Array.from( { length: 6 }, () => [ "native:default:2", "native:default:5" ] ),
			"native DEFAULT set and CCD620 state IDs survive all six phase tables"
		);
		assert.deepEqual( value.attachedAction, { priority: 7, defense: "defense.efp", attack: "attack.efp" } );
		assert.equal( value.secondaryEffect, true );
	} ));
test("proved character range shares anchor data for inclusive aliases and later registrations replace earlier ones", () =>
	fixture( dir => {
		const file = path.join( dir, "character.txt" ),
			make = ( name, height ) =>
				[ name, "human", height, "none", "none", "none", "none", "none", "1,2,3", "none", "none", 0 ].join(
					"\t"
				);
		fs.writeFileSync(
			file,
			"#section characterInfo\n" +
				[ make( "PET_001~003", 2 ), make( "PET_002", 4 ), make( "EMPTY_003~001", 2 ) ].join( "\n" ),
			"utf16le"
		);
		const rows = loadCharacterActionEffectRows( file );
		assert.deepEqual( rows.map( r => r.codename ), [ "PET_001", "PET_002", "PET_003" ] );
		assert.deepEqual( rows.map( r => r.heightFactor ), [ 1, 2, 1 ] );
		assert.ok( rows.every( r => r.anchorOffset.y === 2 ) );
	} ));
