import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import fs from "node:fs";
import {
	parseAvatarVisualOverrides,
	loadAvatarVisualOverrides,
	avatarAnimationRequirements
} from "../../../../scripts/build/char/avatarVisualOverrides.mjs";
import { loadEquipmentRecords } from "../../../../scripts/build/char/equipmentVisualRecords.mjs";
import { retailTextdataRoot, clientV150ResinfoRoot, publicRoot } from "../../../../scripts/build/world/paths.mjs";
import { defined } from "../helpers/defined.mjs";

test("all four retail angel/devil rows retain separate wing resource, animation and priority", () => {
	const rows = loadEquipmentRecords( retailTextdataRoot ),
		overrides = loadAvatarVisualOverrides( path.join( clientV150ResinfoRoot, "avataritemdata.txt" ), rows );
	const roster = JSON.parse( fs.readFileSync( path.join( publicRoot, "assets/char/roster.json" ), "utf8" ) );
	assert.equal( Object.keys( overrides ).length, 4 );
	// The four retail rows publish exactly; a later row beside them (the
	// Devil's Spirit transformation, #4441) overrides the animation only
	// and carries no wing model of its own.
	for ( const [id, override] of Object.entries( overrides ) ) {
		assert.deepEqual( roster.dress.avatarVisualOverrides[id], override );
	}
	for ( const [id, override] of Object.entries( roster.dress.avatarVisualOverrides ) ) {
		if ( !Object.hasOwn( overrides, id ) ) assert.equal( override.additionalBsr, "", id );
	}
	for ( const [id, override] of Object.entries( overrides ) ) {
		assert.equal( override.animation, "avatar_wing" );
		assert.equal( override.priority, 50 );
		assert.match( override.additionalBsr, /avatar_[mw]_(angel|devil)_wing\.bsr$/ );
		assert.notEqual( override.additionalBsr, rows.get( Number( id ) ).resolvedModel );
		const auxiliary = roster.dress.avatarAuxiliary[id];
		assert.ok( auxiliary );
		assert.equal( auxiliary.bone, "Bip01 Spine1" );
		assert.ok( auxiliary.clips.includes( "stand" ) );
		assert.ok( auxiliary.clips.includes( "run" ) );
		assert.ok( !auxiliary.clips.includes( "walk" ) );
		const bytes = fs.readFileSync( path.join( publicRoot, auxiliary.glb ) ),
			json = JSON.parse( bytes.subarray( 20, 20 + bytes.readUInt32LE( 12 ) ).toString() );
		assert.ok( json.skins[0].joints.length > 1, "retain private wing skeleton" );
		for ( const name of [ "stand", "run" ] ) {
			assert.ok(
				json.animations.some( a => a.name === name && a.channels.length ),
				"retain authored animated channels"
			);
		}
	}
});
test("avatar table rejects invalid references/priorities and preserves native first insertion", () => {
	const items = new Map( [ [ 1, { id: 1, code: "DRESS" } ] ] ),
		row = [ "1", "DRESS", "avatar_wing", "res/item/wing.bsr", "50" ];
	assert.deepEqual( parseAvatarVisualOverrides( [ row, [ ...row.slice( 0, 4 ), "0" ] ], items )[1], {
		animation: "avatar_wing",
		priority: 50,
		additionalBsr: "res/item/wing.bsr"
	} );
	assert.deepEqual( parseAvatarVisualOverrides( [ [ "0" ] ], items ), {} );
	for ( const priority of [ "-1", "256", "1.5", "NaN" ] ) {
		assert.throws( () => parseAvatarVisualOverrides( [ [ ...row.slice( 0, 4 ), priority ] ], items ), /priority/ );
	}
	assert.throws( () => parseAvatarVisualOverrides( [ [ "1", "UNKNOWN", ...row.slice( 2 ) ] ], items ), /Unknown/ );
});

test("avatar animation publication includes every authored override state without changing skill requirements", () => {
	const required = new Map( [ [ "default", new Set( [ 2 ] ) ], [ "avatar_wing", new Set( [ 99 ] ) ] ] );
	const bsr = {
		animationSets: [ {
			name: "AVATAR_WING",
			states: [ { stateId: 0, animationPath: "stand.ban" }, { stateId: 7, animationPath: "run.ban" }, {
				stateId: 1,
				animationPath: null
			} ]
		} ]
	};
	const result = avatarAnimationRequirements( bsr, {
		1: { animation: "avatar_wing" },
		2: { animation: "missing" },
		3: { animation: "" }
	}, required );
	assert.deepEqual( [ ...result.get( "avatar_wing" ) ], [ 99, 0, 7 ] );
	assert.equal( result.has( "missing" ), false );
	assert.deepEqual( [ ...required.get( "avatar_wing" ) ], [ 99 ] );
	defined( result.get( "default" ) ).add( 3 );
	assert.deepEqual( [ ...required.get( "default" ) ], [ 2 ] );
});

test("devil wing environment-map payload survives character parsing and publication", async () => {
	const { loadDataAsset } = await import( "../../../../scripts/build/shared/jmxAssetIO.mjs" );
	const { parseCharacterBsr } = await import( "../../../../scripts/build/char/formats.mjs" );
	const { parseModDataSection } = await import( "../../../../scripts/build/shared/bsrModifiers.mjs" );
	const roster = JSON.parse( fs.readFileSync( path.join( publicRoot, "assets/char/roster.json" ), "utf8" ) );
	for ( const [id, row] of Object.entries( roster.dress.avatarVisualOverrides ) ) {
		// A row with no wing model of its own (#4441) has no payload to keep.
		if ( !row.additionalBsr ) continue;
		const bytes = await loadDataAsset( row.additionalBsr ),
			raw = parseModDataSection( bytes, bytes.readUInt32LE( 0x24 ) ),
			parsed = parseCharacterBsr( bytes );
		assert.deepEqual( parsed.environmentModifiers, raw.environmentModifiers );
		assert.deepEqual( roster.dress.avatarAuxiliary[id].environmentModifiers, raw.environmentModifiers );
		if ( row.additionalBsr.includes( "devil" ) ) {
			assert.equal( parsed.environmentModifiers.length, 1 );
			const m = parsed.environmentModifiers[0];
			assert.equal( m.animationSetName, "ambient" );
			assert.deepEqual( m.baseWords, [ 0x3f000000, 1, 256, 0xffffffff, 0, 3 ] );
			assert.deepEqual( m.words24, [ 1, 65536, 0, 1879048192 ] );
		}
	}
});

test("published player override sets retain run metadata and existing raw BAN files", () => {
	const manifest = JSON.parse( fs.readFileSync( path.join( publicRoot, "assets/anim/manifest.json" ), "utf8" ) );
	const rows = Object.entries( manifest.models ).filter( ( [name] ) => /^CHAR_(CH|EU)_/.test( name ) );
	assert.equal( rows.length, 52 );
	for ( const [name, row] of rows ) {
		const states = row.animationSets.avatar_wing;
		assert.deepEqual( Object.keys( states ), [ "7" ], name );
		assert.ok( states[7].durationMs > 0 );
		assert.ok( fs.existsSync( path.join( publicRoot, states[7].url ) ), name );
	}
});
