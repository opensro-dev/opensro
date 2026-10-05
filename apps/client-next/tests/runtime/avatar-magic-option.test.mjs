/*
===========================================================================

avatar-magic-option.test.mjs - the smith's avatar magic option grant

Expectations follow the grant window (6EB570 drop, 6EBB10 confirm,
703710 request, 770140 answer), the menu row (5D9100 row 0x2F) and the
avatar line formatter (553980); they are worked from those, not read back.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const {
	avatarMagicOptionParts,
	avatarMagicOptionRequest,
	avatarMagicOptionText,
	grantableAvatarPart
} = await import( "../../src/engine/foundation/gameplay/avatar-magic-option.ts" );
const { createMagicOptionGrant } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/magic-option/magic-option.ts"
);
const { itemTooltipMagic } = await import( "../../src/engine/foundation/ui/item-tooltip-magic.ts" );

const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 & 255 ];
const flags = ( tid1, tid2, tid3, tid4 ) => tid1 << 2 | tid2 << 5 | tid3 << 7 | tid4 << 11;
const HAT = flags( 3, 1, 13, 1 );
/** @type {Map<number, any>} */
const references = new Map( [
	[ 246, { paramId: 246, optionName: "MATTR_AVATAR_STR", paramName: "+", degree: 1, rangeWords: [ 1, 1, 1 ] } ],
	[ 250, { paramId: 250, optionName: "MATTR_AVATAR_HP", paramName: "+", degree: 1, rangeWords: [ 1, 150, 150 ] } ]
] );
const parts = [ { part: 1, options: [ "MATTR_AVATAR_STR" ] }, { part: 2, options: [ "MATTR_AVATAR_HP" ] } ];
const text = symbol => symbol;

/*
================
hat
================
*/
/** @returns {any} */
const hat = ( slot, magic = [] ) => ({
	slot,
	refObjId: 9001,
	typeFlags: HAT,
	quantity: 1,
	plus: 0,
	durability: 0,
	variance: "0",
	magic,
	magicReferences: [ ...references.values() ],
	tooltip: { fields: { maxMagicOptions51c: 2 } }
});

test("the window takes avatar hats, dresses and attachments, not flags", () => {
	assert.equal( grantableAvatarPart( HAT ), 1 );
	assert.equal( grantableAvatarPart( flags( 3, 1, 13, 3 ) ), 3 );
	assert.equal( grantableAvatarPart( flags( 3, 1, 13, 4 ) ), null );
	assert.equal( grantableAvatarPart( flags( 3, 1, 1, 1 ) ), null );
	assert.equal( grantableAvatarPart( HAT | 2 ), null );
});

test("the assignment rows resolve through the option definitions", () => {
	assert.deepEqual( avatarMagicOptionParts( parts, references ), [
		{ part: 1, options: [ { codename: "MATTR_AVATAR_STR", value: 1 } ] },
		{ part: 2, options: [ { codename: "MATTR_AVATAR_HP", value: 150 } ] }
	] );
	assert.throws( () => avatarMagicOptionParts( [ { part: 1, options: [ "MATTR_AVATAR_ER" ] } ], references ) );
});

test("the request carries the inventory slot and the codename", () => {
	assert.deepEqual( avatarMagicOptionRequest( 14, "MATTR_AVATAR_STR" ), {
		opcode: 0x361a,
		payload: Uint8Array.of( 14, 16, 0, ...Array.from( "MATTR_AVATAR_STR", c => c.charCodeAt( 0 ) ) )
	} );
});

test("553980 formats stat options plain and rate options with a percent", () => {
	assert.equal( avatarMagicOptionText( "MATTR_AVATAR_HP", 150, text ), "PARAM_HP 150 PARAM_INCREASE" );
	assert.equal( avatarMagicOptionText( "MATTR_AVATAR_HR", 5, text ), "PARAM_HR 5% PARAM_INCREASE" );
	assert.equal(
		avatarMagicOptionText( "MATTR_AVATAR_MDIA", 1, text ),
		"PARAM_AVATAR_MDIA 1% UIIT_STT_PROBABILITY"
	);
	assert.deepEqual( itemTooltipMagic( hat( 14, [ String( 1n << 32n | 246n ) ] ), text ), [
		{ value: "PARAM_STR 1 PARAM_INCREASE", color: 0xff00eaff }
	] );
});

test("the grant window opens on its lock, takes a hat and sends one option", () => {
	const grant = createMagicOptionGrant();
	grant.bootstrap( parts, references );
	assert.deepEqual( grant.open( 300 ), {
		opcode: 0x7338,
		payload: Uint8Array.of( ...u32( 300 ), ...u32( 0x80000000 ) )
	} );
	assert.equal( grant.opened( Uint8Array.of( 1, ...u32( 0x4000 ) ) ), false );
	assert.equal( grant.opened( Uint8Array.of( 1, ...u32( 0x80000000 ) ) ), true );
	assert.equal( grant.state().visible, true );
	const flag = { ...hat( 15 ), typeFlags: flags( 3, 1, 13, 4 ) };
	assert.equal( grant.take( flag ), "UIIT_STT_AVATAR_MAGICOPTION_ONLY_AVATAR" );
	assert.equal( grant.take( hat( 14 ) ), null );
	assert.equal( grant.state().item, 14 );
	assert.throws( () => grant.grant( "MATTR_AVATAR_HP", hat( 14 ) ) );
	assert.equal( grant.grant( "MATTR_AVATAR_STR", hat( 14 ) ).frame?.opcode, 0x361a );
	assert.equal( grant.state().phase, "waiting" );
	assert.equal( grant.result( Uint8Array.of( 2, 0x0a ) ), null );
	assert.equal( grant.state().error, 0x0a );
	assert.equal( grant.result( Uint8Array.of( 1, 1, 14, 0 ) ), 14 );
	assert.equal( grant.state().phase, "idle" );
});

test("a hat with no free option slot is refused before any request", () => {
	const grant = createMagicOptionGrant();
	grant.bootstrap( parts, references );
	grant.open( 300 );
	grant.opened( Uint8Array.of( 1, ...u32( 0x80000000 ) ) );
	const full = hat( 14, [ String( 1n << 32n | 246n ), String( 150n << 32n | 250n ) ] );
	grant.take( full );
	assert.deepEqual( grant.grant( "MATTR_AVATAR_STR", full ), { notice: "UIIT_MSG_AVATAR_MAGICOPTION_ADD_ERORR" } );
	assert.equal( grant.state().phase, "idle" );
});
