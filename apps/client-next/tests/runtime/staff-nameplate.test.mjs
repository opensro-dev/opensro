/*
===========================================================================

staff-nameplate.test.mjs - who wears the animated staff nameplate

Port-only, not native (owner 2026-11-28). A rostered player whose
nameplate switch is on wears it; one who sent /NAMEPLATE OFF, a name that
only starts with "[GM]", a job alias, a monster and a plain player do not. The layers survive the UI's admission copy (a field
the copy drops never reaches the device, as #3446 lost the additive
blend), and the Experimental row is on unless turned off.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { isStaffNameplate, staffNameplateAura, staffNameplateSparks } = await import(
	"../../src/engine/foundation/ui/staff-nameplate.ts"
);
const { copyUi } = await import( "../../src/engine/foundation/ui/ui.ts" );
const { sameUiQuad } = await import( "../../src/engine/foundation/ui/ui-equality.ts" );
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );

const ROSTER = [ { name: "Harry", tag: "[MOD]", nameColor: 0xff40c0ff, chatColor: 0xff40c0ff } ];
const BOARD = [ -30, -8, 60, 16 ];
const FULL = [ 0, 0, 800, 600 ];

/*
================
who wears it
================
*/
test("a rostered player with the switch on wears the staff nameplate; others do not", () => {
	assert.equal( isStaffNameplate( { kind: "player", name: "harry" }, ROSTER ), true );
	assert.equal( isStaffNameplate( { kind: "local-player", name: "Harry" }, ROSTER ), true );
	// /NAMEPLATE OFF: the server's 0x3FD3 folded into the roster.
	assert.equal(
		isStaffNameplate( { kind: "player", name: "Harry" }, [ { ...ROSTER[0], nameplateOff: true } ] ),
		false
	);
	// A name is not a grant: only staff the server confirms glow.
	assert.equal( isStaffNameplate( { kind: "local-player", name: "[GM]Numz" }, undefined ), false );
	assert.equal( isStaffNameplate( { kind: "player", name: "Ron" }, ROSTER ), false );
	// A job suit's alias hides who wears it.
	assert.equal( isStaffNameplate( { kind: "player", name: "Harry", boardName: "Hunter" }, ROSTER ), false );
	// Only players: a monster named like staff is not staff.
	assert.equal( isStaffNameplate( { kind: "monster", name: "[GM]Mob" }, ROSTER ), false );
	assert.equal( isStaffNameplate( { kind: "player", name: "" }, ROSTER ), false );
});

/*
================
layers
================
*/
test("the halo surrounds the board and the sparks sit along it, all anchored", () => {
	const aura = staffNameplateAura( BOARD, FULL, 7 );
	assert.equal( aura.fx, "staff-aura" );
	assert.equal( aura.characterAnchor, 7 );
	assert.ok( aura.rect[0] < BOARD[0] && aura.rect[1] < BOARD[1] );
	assert.ok( aura.rect[0] + aura.rect[2] > BOARD[0] + BOARD[2] );
	const sparks = staffNameplateSparks( BOARD, FULL, 7 );
	assert.ok( sparks.length > 1 );
	for ( const spark of sparks ) {
		assert.equal( spark.fx, "staff-spark" );
		assert.equal( spark.characterAnchor, 7 );
		const centre = spark.rect[0] + spark.rect[2] / 2;
		assert.ok( centre > BOARD[0] && centre < BOARD[0] + BOARD[2] );
	}
	// Distinct positions give each spark its own phase in the shader.
	assert.equal( new Set( sparks.map( s => s.rect[0] ) ).size, sparks.length );
});

/*
================
admission keeps the layer
================
*/
test("the UI admission copy keeps fx, equality tells layers apart, a bad fx is refused", () => {
	const aura = staffNameplateAura( BOARD, FULL, 7 );
	const scene = { revision: 1, width: 800, height: 600, quads: [ aura ] };
	assert.equal( copyUi( scene ).quads[0].fx, "staff-aura" );
	assert.equal( sameUiQuad( aura, { ...aura, fx: "staff-spark" } ), false );
	assert.equal( sameUiQuad( aura, { ...aura } ), true );
	assert.throws( () => copyUi( { ...scene, quads: [ { ...aura, fx: "rainbow" } ] } ) );
});

/*
================
Experimental row
================
*/
test("the staff nameplate row is on unless explicitly turned off", () => {
	for ( const value of [ undefined, null, {}, { staffNameplateFx: 1 }, { staffNameplateFx: "false" } ] ) {
		assert.equal( experimentalOptions( value ).staffNameplateFx, true );
	}
	assert.equal( experimentalOptions( { staffNameplateFx: false } ).staffNameplateFx, false );
});
