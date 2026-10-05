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
const { canAttackPlayer, petPlayerAttack, playerInteraction } = await import(
	"../../src/engine/foundation/gameplay/player-attack.ts"
);

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
