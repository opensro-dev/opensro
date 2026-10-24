/*
===========================================================================

fortress-map.test.mjs - the battle aide's fortress map

The aide's structure row (5D8FF0 0x1000000, 5D7AD0 action 0x32), the
0x71E1 0x18 list that opens CIFFortressMap, the zone records the 0x3887
roster fills (76C870 case 0xF), the status panel (65D970), the button
rules (65CF40), the repair price (65D340) and the upgrade target lookup
(7E14E0).

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";
const fortress = await import( "../../src/engine/foundation/gameplay/fortress.ts" );
const map = await import( "../../src/engine/foundation/gameplay/fortress-map.ts" );
const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );

const AIDE = 31;
const SELF = 5;
const JANGAN = 1;

/*
================
rawCatalog

A three-zone fortress page: a defensive position and a guard tower, with
the references the upgrade rows name.
================
*/
function rawCatalog() {
	const structure = ( name, level, maxHp, extra = {} ) => ({
		codeName: name,
		name: "SN_" + name,
		icon: "etc/strut_" + name.toLowerCase() + ".ddj",
		level,
		maxHp,
		defence: level * 100,
		skill: 0,
		costRepair: 0,
		costRevive: 0,
		upgradeGold: 1000 * (level + 1),
		upgradeGuildPoints: 100 * (level + 1),
		upgrades: null,
		...extra
	});
	return {
		format: "sro-fortress-map",
		version: 1,
		pages: [ {
			id: 1001,
			world: "INS_FORT_JA",
			image: "interface/worldmap/map/map_fort_jangan.ddj",
			texture: [ 1024, 1024 ],
			picture: [ 1024, 819 ],
			size: [ 1024, 819 ],
			bounds: [ 69, 71, 74, 67, -48, 48, 48, 0 ]
		} ],
		zones: [
			{
				id: 91,
				codeName: "POS_91",
				type: "DEFENSIVE_POSITION",
				world: "INS_FORT_JA",
				defaultStructure: "D_00",
				regionId: 0x4648,
				x: 100,
				y: 0,
				z: 100
			},
			{
				id: 85,
				codeName: "POS_85",
				type: "GUARD_TOWER",
				world: "INS_FORT_JA",
				defaultStructure: "T_01",
				regionId: 0x4648,
				x: 200,
				y: 0,
				z: 200
			},
			{
				id: 109,
				codeName: "POS_109",
				type: "BARRICADE",
				world: "INS_FORT_JA",
				defaultStructure: "B",
				regionId: 0x4648,
				x: 300,
				y: 0,
				z: 300
			}
		],
		structures: {
			10: structure( "D_00", 0, 0, { upgrades: [ 11 ] } ),
			11: structure( "D_01", 1, 1000, { upgrades: [ 12 ], costRepair: 5000, costRevive: 700, skill: 20521 } ),
			12: structure( "D_02", 2, 2000, { upgrades: [ 0 ] } ),
			20: structure( "T_01", 1, 500, { upgrades: [ 21, 22, 23, 24 ] } ),
			21: structure( "T_LIGHTNING_02", 2, 800 )
		}
	};
}

/*
================
catalog
================
*/
function catalog() {
	return map.decodeFortressMap( rawCatalog() );
}

/*
================
roster

A 0x3887 0x0F frame: rows of [zone, ref, hp, state].
================
*/
function roster( rows ) {
	const bytes = [ 0x0f, JANGAN, 0, 0, 0, rows.length ];
	const u32 = v => bytes.push( v & 255, v >>> 8 & 255, v >>> 16 & 255, v >>> 24 & 255 );
	for ( const [zone, ref, hp, state] of rows ) {
		u32( zone );
		u32( ref );
		u32( hp );
		bytes.push( state & 255, state >>> 8 & 255, 0 );
	}
	return { opcode: 0x3887, payload: Uint8Array.from( bytes ) };
}

/*
================
baseState
================
*/
function baseState() {
	return {
		...fortress.fortressBootstrap( {
			gameWorldData: [ { gameWorldId: 7, warName: "FORTRESS_JANGAN" } ],
			siegeFortressData: [ { fortressId: JANGAN, codeName: "FORTRESS_JANGAN", nameStrId: "SN_FORTRESS_JANGAN" } ]
		} ),
		worldId: 7,
		serviceSequence: 0
	};
}

/*
================
zoneOf
================
*/
function zoneOf( state, zone ) {
	return defined( defined( state.zones, "zones" ).find( r => r.zone === zone ), "zone " + zone );
}

test("the roster fills zone records; 0x0B moves a state and the list its minutes", () => {
	let state = defined( fortress.fortressPacket( baseState(), roster( [ [ 91, 11, 600, 0 ], [ 85, 20, 500, 0 ] ] ) ) );
	assert.deepEqual( defined( state.zones ).map( r => [ r.zone, r.ref, r.hp, r.state, r.minutes ] ), [
		[ 91, 11, 600, 0, 0 ],
		[ 85, 20, 500, 0, 0 ]
	] );
	// 0x0B: fortress, object, event zone 85, state 4.
	const destroyed = Uint8Array.from( [ 0x0b, JANGAN, 0, 0, 0, 9, 0, 0, 0, 85, 0, 0, 0, 4, 0 ] );
	state = defined( fortress.fortressPacket( state, { opcode: 0x3887, payload: destroyed } ) );
	assert.equal( zoneOf( state, 85 ).state, 4 );
	// 0xB1E1 0x18: fortress, count, then zone and minutes.
	const list = Uint8Array.from( [ 0x18, 1, JANGAN, 0, 0, 0, 1, 91, 0, 0, 0, 30, 0, 0, 0 ] );
	state = defined( fortress.fortressPacket( state, { opcode: 0xb1e1, payload: list } ) );
	assert.equal( zoneOf( state, 91 ).minutes, 30 );
	assert.equal( zoneOf( state, 91 ).hp, 600, "the list keeps the roster's hit points" );
});

test("the panel shows rank, durability and state, and the top rank", () => {
	const c = catalog();
	const status = defined(
		map.fortressMapStatus( c, { zone: 91, ref: 11, hp: 600, state: 0, guild: "", minutes: 0 } )
	);
	assert.equal( status.name, "SN_D_01" );
	assert.equal( status.state, "UIIT_STT_FORT_BATTLEAIDE_STRUCTSTATUS_02" );
	assert.deepEqual( [ status.rank, status.defence, status.hp, status.maxHp, status.feature ], [
		1,
		100,
		600,
		1000,
		20521
	] );
	assert.equal( status.topRank, false );
	const full = defined(
		map.fortressMapStatus( c, { zone: 91, ref: 11, hp: 1000, state: 0, guild: "", minutes: 0 } )
	);
	assert.equal( full.state, "UIIT_STT_FORT_BATTLEAIDE_STRUCTSTATUS_01" );
	const fallen = defined( map.fortressMapStatus( c, { zone: 91, ref: 11, hp: 0, state: 4, guild: "", minutes: 0 } ) );
	assert.equal( fallen.state, "UIIT_STT_FORT_BATTLEAIDE_STRUCTSTATUS_03" );
	const empty = defined( map.fortressMapStatus( c, { zone: 91, ref: 10, hp: 0, state: 0, guild: "", minutes: 0 } ) );
	assert.equal( empty.state, "", "an empty position (rank 0) shows no state" );
	assert.equal(
		defined( map.fortressMapStatus( c, { zone: 91, ref: 12, hp: 2000, state: 0, guild: "", minutes: 0 } ) ).topRank,
		true
	);
});

test("the upgrade target is the second target, else the first, else the structure", () => {
	const c = catalog();
	assert.equal( map.fortressUpgradeTarget( c, 20 ), 22 );
	assert.equal( map.fortressUpgradeTarget( c, 11 ), 12 );
	assert.equal( map.fortressUpgradeTarget( c, 12 ), 0 );
	assert.equal( map.fortressUpgradeTarget( c, 21 ), 21 );
});

test("the buttons follow 65CF40", () => {
	const c = catalog(), [position, tower] = [ c.zones[0], c.zones[1] ];
	const record = ( zone, ref, hp ) => ({ zone, ref, hp, state: 0, guild: "", minutes: 0 });
	const buttons = ( zone, rec, role = 1 ) => map.fortressMapButtons( c, zone, rec, role );
	assert.deepEqual( buttons( position, record( 91, 10, 0 ) ), { construct: true, repair: false, upgrade: false } );
	assert.deepEqual( buttons( position, record( 91, 11, 600 ) ), { construct: false, repair: true, upgrade: false } );
	assert.deepEqual( buttons( position, record( 91, 11, 1000 ) ), { construct: false, repair: false, upgrade: true } );
	assert.deepEqual( buttons( position, record( 91, 12, 2000 ) ), {
		construct: false,
		repair: false,
		upgrade: false
	} );
	assert.deepEqual( buttons( tower, record( 85, 20, 500 ) ), { construct: false, repair: false, upgrade: true } );
	assert.deepEqual( buttons( tower, record( 85, 20, 500 ), 4 ), { construct: false, repair: false, upgrade: true } );
	assert.deepEqual( buttons( tower, record( 85, 20, 500 ), 2 ), { construct: false, repair: false, upgrade: false } );
	assert.equal( map.fortressNeedsTowerType( c, tower, record( 85, 20, 500 ) ), true );
	assert.equal( map.fortressNeedsTowerType( c, tower, record( 85, 21, 800 ) ), false );
});

test("the repair price follows 65D340", () => {
	const c = catalog(), d1 = defined( c.structures.get( 11 ) );
	// 5000 / 1000 = 5 gold per point.
	assert.equal( map.fortressRepairPrice( d1, 600 ), 2000 );
	// A destroyed structure counts one point and adds its revive cost.
	assert.equal( map.fortressRepairPrice( d1, 0 ), 700 + 999 * 5 );
	assert.equal( map.fortressRepairPrice( d1, 1000 ), 0 );
	// Below one gold per point the price per point is one.
	assert.equal( map.fortressRepairPrice( { ...d1, costRepair: 10 }, 600 ), 400 );
});

test("markers use the structure icon by hit points and the gate for a barricade", () => {
	const c = catalog(), [position, , barricade] = c.zones;
	const marker = hp =>
		defined( map.fortressMarker( c, position, { zone: 91, ref: 11, hp, state: 0, guild: "", minutes: 0 } ) );
	assert.equal( marker( 1000 ).image, "/assets/images/Media_extracted/icon/etc/strut_d_01.png" );
	assert.equal( marker( 600 ).image, "/assets/images/Media_extracted/icon/etc/strut_d_01_r.png" );
	assert.equal( marker( 0 ).image, "/assets/images/Media_extracted/icon/etc/strut_d_01_d.png" );
	assert.equal( marker( 1000 ).size, 24 );
	assert.equal( map.fortressMarker( c, position, undefined ), null );
	assert.deepEqual( map.fortressMarker( c, barricade, undefined ), {
		image: "/assets/images/Media_extracted/interface/worldmap/map/gate.png",
		size: 16
	} );
});

/*
================
aideFixture

An open talk menu with the battle aide (capability 0x1000000).
================
*/
function aideFixture( t, holder ) {
	const sent = [];
	const published = { "/assets/data/fortressMap.json": Buffer.from( JSON.stringify( rawCatalog() ) ) };
	const f = uiFixture(
		message => {
			if ( message.kind === "gameplay" ) sent.push( message.command );
		},
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		{},
		published
	);
	t.after( () => f.dispose() );
	f.state.entities.push( { ...f.state.entities[0], gid: AIDE, refObjId: 2200, kind: "npc", name: "Aide" } );
	Object.assign( f.state.gameplay, {
		target: AIDE,
		targetCapabilities: 0x1000000,
		npcConversation: { phase: "menu", gid: AIDE },
		fortress: { ...baseState(), wars: [ { id: JANGAN, name: holder, flags: 0 } ] },
		social: {
			...emptySocial( "Player" ),
			self: SELF,
			guild: {
				id: 9,
				name: "Holders",
				level: 3,
				gp: 0,
				subject: "",
				contents: "",
				crest: 0,
				members: [ { id: SELF, name: "Player", grade: 0, role: 1, level: 60, permissions: 31 } ]
			}
		}
	} );
	let now = 0, last = null;
	const step = () => {
		for ( let i = 0; i < 16; i++ ) last = f.ui.step( f.state, now += 100 ) ?? last;
		return last;
	};
	return { f, sent, step };
}

test("the aide's structure row asks for the list and its answer opens the map", t => {
	const other = aideFixture( t, "Others" );
	assert.equal( other.step().controls.some( c => c.id === "npc-fortress-structure" ), false );
	const { f, sent, step } = aideFixture( t, "Holders" );
	const row = step().controls.find( c => c.id === "npc-fortress-structure" );
	assert.ok( row, "the holder's member sees the structure row" );
	f.ui.event( { kind: "activate", id: "npc-fortress-structure" } );
	assert.deepEqual( sent.at( -1 ), { kind: "fortress-structure", gid: AIDE, fortress: JANGAN, action: 0x18 } );
	const state = f.state.gameplay.fortress;
	f.state.gameplay.fortress = {
		...state,
		serviceSequence: state.serviceSequence + 1,
		service: { action: 0x18, result: 1, fortress: JANGAN, structures: [] }
	};
	const shown = step();
	assert.ok( shown.controls.some( c => c.id === "fortress-map-close" ), "the map window opened" );
	f.state.gameplay.npcConversation = null;
	assert.equal( step().controls.some( c => c.id === "fortress-map-close" ), false, "ending the talk closes it" );
});

/*
================
openMap

Opens the map for the holder's commander over a roster with an empty
defensive position (zone 91) and a rank-1 guard tower (zone 85), the
player standing in the fortress.
================
*/
function openMap( t ) {
	const fixture = aideFixture( t, "Holders" );
	const { f, step } = fixture;
	f.state.gameplay.pose = { ...f.state.gameplay.pose, regionId: 0x4648, x: 150, y: 0, z: 150, angle: 0 };
	f.state.gameplay.fortress = fortress.fortressPacket(
		f.state.gameplay.fortress,
		roster( [ [ 91, 10, 0, 0 ], [ 85, 20, 500, 0 ] ] )
	);
	step();
	f.ui.event( { kind: "activate", id: "npc-fortress-structure" } );
	const state = f.state.gameplay.fortress;
	f.state.gameplay.fortress = {
		...state,
		serviceSequence: state.serviceSequence + 1,
		service: { action: 0x18, result: 1, fortress: JANGAN, structures: [] }
	};
	return fixture;
}

test("a zone marker selects it; construction asks, then sends 0x0B type 0", t => {
	const { f, sent, step } = openMap( t );
	const shown = step();
	for ( const id of [ "fortress-map-zone:91", "fortress-map-zone:85", "fortress-map-zone:109" ] ) {
		assert.ok( shown.controls.some( c => c.id === id ), id + " has a marker" );
	}
	assert.equal( shown.controls.find( c => c.id === "fortress-map-construct" )?.disabled, true, "nothing selected" );
	f.ui.event( { kind: "activate", id: "fortress-map-zone:91" } );
	const selected = step();
	assert.equal( selected.controls.find( c => c.id === "fortress-map-construct" )?.disabled, false );
	assert.equal( selected.controls.find( c => c.id === "fortress-map-upgrade" )?.disabled, true );
	f.ui.event( { kind: "activate", id: "fortress-map-construct" } );
	assert.ok( step().controls.some( c => c.id === "fortress-map-yes" ), "the construction box asks" );
	f.ui.event( { kind: "activate", id: "fortress-map-yes" } );
	assert.deepEqual( sent.at( -1 ), {
		kind: "fortress-structure",
		gid: AIDE,
		fortress: JANGAN,
		action: 0x0b,
		zone: 91,
		type: 0
	} );
});

test("a rank-1 guard tower asks its type first and sends the chosen one", t => {
	const { f, sent, step } = openMap( t );
	step();
	f.ui.event( { kind: "activate", id: "fortress-map-zone:85" } );
	assert.equal( step().controls.find( c => c.id === "fortress-map-upgrade" )?.disabled, false );
	f.ui.event( { kind: "activate", id: "fortress-map-upgrade" } );
	const popup = step();
	assert.deepEqual(
		popup.controls.filter( c => c.id.startsWith( "fortress-map-type:" ) ).map( c => c.id ),
		[ 0, 1, 2, 3 ].map( i => "fortress-map-type:" + i )
	);
	f.ui.event( { kind: "activate", id: "fortress-map-type:2" } );
	step();
	f.ui.event( { kind: "activate", id: "fortress-map-type-ok" } );
	assert.ok( step().controls.some( c => c.id === "fortress-map-yes" ), "the upgrade box asks" );
	f.ui.event( { kind: "activate", id: "fortress-map-yes" } );
	assert.deepEqual( sent.at( -1 ), {
		kind: "fortress-structure",
		gid: AIDE,
		fortress: JANGAN,
		action: 0x0b,
		zone: 85,
		type: 2
	} );
});
