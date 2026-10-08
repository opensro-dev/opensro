/*
===========================================================================

player-attack.test.mjs - tests for player-attack.ts: a click on another
player attacks only when it was selected or Alt is held, CICUser_CanAttack
admits, it is no party member and the attacker meets the level rule

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { canAttackCos, canAttackPlayer, hoverAttack, petPlayerAttack, playerInteraction, skillTargetAdmission } =
	await import( "../../src/engine/foundation/gameplay/player-attack.ts" );

/** @param {object} [fields] */
const player = ( fields = {} ) => ({
	gid: 9,
	kind: "player",
	name: "other",
	refObjId: 1907,
	regionId: 0x62aa,
	x: 0,
	y: 0,
	z: 0,
	heading: 0,
	...fields
});
/** @param {object} [fields] */
const context = ( fields = {} ) => ({
	local: {
		gid: 1,
		kind: "local-player",
		name: "me",
		refObjId: 1907,
		regionId: 0x62aa,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		level: 30
	},
	social: { leader: 0, members: [] },
	fortress: { registered: [] },
	capeTeam: () => undefined,
	...fields
});
const press = { selected: true, alt: false, guildWar: false };

test("a neutral player is attacked only with Alt", () => {
	assert.equal( canAttackPlayer( player(), context(), false ), false );
	assert.equal( canAttackPlayer( player(), context(), true ), true );
	assert.deepEqual( playerInteraction( player(), context(), press ), { kind: "none" } );
	assert.deepEqual( playerInteraction( player(), context(), { ...press, alt: true } ), { kind: "attack" } );
});

test("an aggressor, a murderer or the last attacker is attackable on a second click", () => {
	assert.deepEqual( playerInteraction( player( { pvpState: 1 } ), context(), press ), { kind: "attack" } );
	assert.deepEqual( playerInteraction( player( { pvpState: 2 } ), context(), press ), { kind: "attack" } );
	assert.deepEqual( playerInteraction( player(), context( { attackedName: "other" } ), press ), { kind: "attack" } );
	assert.deepEqual(
		playerInteraction( player( { pvpState: 2 } ), context(), { ...press, selected: false } ),
		{ kind: "none" },
		"an unselected player is only selected by the first click"
	);
});

test("a party member is never attacked", () => {
	const social = { leader: 1, members: [ { name: "other" } ] };
	assert.deepEqual( playerInteraction( player( { pvpState: 2 } ), context( { social } ), press ), { kind: "none" } );
});

test("an attacker below level 20 without a PvP cape is refused", () => {
	const low = context();
	const local = { ...low.local, level: 12 };
	assert.deepEqual( playerInteraction( player( { pvpState: 2 } ), { ...low, local }, press ), { kind: "low-level" } );
	assert.deepEqual(
		playerInteraction( player( { pvpState: 2 } ), { ...low, local }, { ...press, guildWar: true } ),
		{ kind: "attack" }
	);
});

test("an event match decides by team alone", () => {
	const c = context();
	const local = { ...c.local, arenaTeam: 1 };
	assert.equal( canAttackPlayer( player( { arenaTeam: 1, pvpState: 2 } ), { ...c, local }, true ), false );
	assert.equal( canAttackPlayer( player( { arenaTeam: 2 } ), { ...c, local }, false ), true );
});

test("an attack pet is sent at an attackable player by an owner of level 20", () => {
	assert.deepEqual( petPlayerAttack( player( { pvpState: 2 } ), context(), false ), { kind: "attack" } );
	assert.deepEqual( petPlayerAttack( player(), context(), false ), { kind: "none" } );
	const low = context();
	assert.deepEqual(
		petPlayerAttack( player( { pvpState: 2 } ), { ...low, local: { ...low.local, level: 19 } }, false ),
		{ kind: "low-level" }
	);
});

// A COS type word: TID 1/2/3 with TID4 in bits 11..15.
const cosWord = tid4 => 0x1c6 | tid4 << 11;
/** @param {object} [fields] */
const pet = ( fields = {} ) =>
	player( { gid: 20, kind: "cos", name: "pet", tidWord: cosWord( 3 ), ownerGid: 9, ...fields } );
/** @param {readonly object[]} rows */
const lookup = rows => ({
	entity: gid => rows.find( row => row.gid === gid ),
	rider: gid => rows.find( row => row.mountedOn === gid )
});
const none = lookup( [] );

test("6FCD50: an offensive skill at a neutral player needs Alt", () => {
	assert.deepEqual( skillTargetAdmission( player(), context(), false, true, none ), { kind: "none" } );
	assert.deepEqual( skillTargetAdmission( player(), context(), true, true, none ), { kind: "cast" } );
	assert.deepEqual( skillTargetAdmission( player( { pvpState: 1 } ), context(), false, true, none ), {
		kind: "cast"
	} );
	assert.deepEqual(
		skillTargetAdmission( player(), context( { attackedName: "other" } ), false, true, none ),
		{ kind: "cast" },
		"the player this one just hit stays hostile for the attacked-name window"
	);
	assert.deepEqual( skillTargetAdmission( player(), context(), false, false, none ), { kind: "cast" } );
});

test("6FCD50: an offensive skill at a party member, or its pet, raises 4:0x22 / 4:0x23", () => {
	const social = { leader: 1, members: [ { name: "other" } ] };
	assert.deepEqual( skillTargetAdmission( player(), context( { social } ), true, true, none ), {
		kind: "notice",
		code: 0x22
	} );
	assert.deepEqual( skillTargetAdmission( pet(), context( { social } ), true, true, lookup( [ player() ] ) ), {
		kind: "notice",
		code: 0x23
	} );
});

test("CICCos_CanAttack: pets answer for their owner, horses for their rider, pickup pets never", () => {
	const owner = player( { pvpState: 1 } );
	assert.equal( canAttackCos( pet(), context(), false, lookup( [ owner ] ) ), true );
	assert.equal( canAttackCos( pet(), context(), false, lookup( [ player() ] ) ), false );
	const horse = pet( { tidWord: cosWord( 1 ), ownerGid: undefined } );
	assert.equal(
		canAttackCos( horse, context(), false, lookup( [ player( { mountedOn: 20, pvpState: 2 } ) ] ) ),
		true
	);
	assert.equal( canAttackCos( horse, context(), false, none ), false );
	assert.equal( canAttackCos( pet( { tidWord: cosWord( 4 ) } ), context(), true, lookup( [ owner ] ) ), false );
	assert.deepEqual( skillTargetAdmission( pet(), context(), false, true, lookup( [ player() ] ) ), { kind: "none" } );
	assert.deepEqual( skillTargetAdmission( pet(), context(), true, true, lookup( [ player() ] ) ), { kind: "cast" } );
});

test("6875F0: the hover verdict judges both Alt states; party members and own pets are never targets", () => {
	assert.equal( hoverAttack( player(), context(), none ), 2, "a neutral player only with Alt" );
	assert.equal( hoverAttack( player( { pvpState: 1 } ), context(), none ), 3 );
	const social = { leader: 1, members: [ { name: "other" } ] };
	assert.equal( hoverAttack( player( { pvpState: 1 } ), context( { social } ), none ), 0 );
	assert.equal( hoverAttack( pet(), context(), lookup( [ player() ] ) ), 2 );
	assert.equal( hoverAttack( pet( { ownerGid: 1 } ), context(), none ), 0, "the local player's own pet" );
	assert.equal( hoverAttack( pet( { tidWord: cosWord( 4 ) } ), context(), lookup( [ player() ] ) ), 0 );
	assert.equal( hoverAttack( { ...player(), kind: "monster" }, context(), none ), 0 );
});
