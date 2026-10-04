/*
===========================================================================

skin-change.test.mjs - the skin change scroll's window draft and tail

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const skin = await import( "../../src/engine/foundation/gameplay/skin-change.ts" );
const cos = await import( "../../src/engine/foundation/gameplay/cos-item-use.ts" );

// Sex selector +0x1AC: 1 male, 0 female.
const MODELS = [
	{ refObjId: 1907, sex: 1 },
	{ refObjId: 1908, sex: 1 },
	{ refObjId: 1920, sex: 0 },
	{ refObjId: 1921, sex: 0 },
	{ refObjId: 1922, sex: 0 }
];
const word = ( t1, t2, t3, t4 ) => (t1 & 7) << 2 | (t2 & 3) << 5 | (t3 & 15) << 7 | (t4 & 31) << 11;

test("only the 3/3/13/9 type word is the skin scroll", () => {
	assert.equal( skin.isSkinChangeScroll( word( 3, 3, 13, 9 ) ), true );
	assert.equal( skin.isSkinChangeScroll( word( 3, 3, 13, 10 ) ), false );
	assert.equal( skin.isSkinChangeScroll( word( 3, 3, 13, 9 ) | 2 ), false );
});

test("the draft starts at the worn body and confirms only a change", () => {
	const draft = skin.initialSkinDraft( MODELS, 1908, 0x31 );
	assert.deepEqual( draft, { sex: 1, figure: 2, height: 1, volume: 3 } );
	assert.equal( skin.skinDraftChanged( MODELS, draft, 1908, 0x31 ), false );
	const taller = skin.setSkinDraft( MODELS, draft, "height", 9 );
	assert.equal( taller.height, 4 );
	assert.deepEqual( skin.skinDraftChoice( MODELS, taller ), { model: 1908, shape: 0x34 } );
	assert.equal( skin.skinDraftChanged( MODELS, taller, 1908, 0x31 ), true );
	assert.deepEqual( skin.initialSkinDraft( MODELS, 1907, 0xff ), { sex: 1, figure: 1, height: 2, volume: 2 } );
});

test("switching sex keeps the figure inside the other list", () => {
	const female = skin.setSkinDraft( MODELS, { sex: 0, figure: 3, height: 2, volume: 2 }, "sex", 1 );
	assert.deepEqual( female, { sex: 1, figure: 2, height: 2, volume: 2 } );
	assert.deepEqual( skin.skinDraftRange( MODELS, female, "figure" ), [ 1, 2 ] );
});

test("the use carries [u32 model][u8 shape] and needs the window's choice", () => {
	const flags = word( 3, 3, 13, 9 );
	assert.deepEqual( [ ...cos.cosItemUseTail( flags, [], { records: [], skin: { model: 1920, shape: 0x22 } } ) ], [
		0x80,
		7,
		0,
		0,
		0x22
	] );
	assert.throws( () => cos.cosItemUseTail( flags, [], { records: [] } ), /Choose a skin/ );
});

test("the gender change tool dropped on armour uses itself with the target slot", () => {
	// Only the slot and type word take part in the drop.
	const tool = /** @type {any} */ ({ slot: 25, typeFlags: word( 3, 3, 13, 8 ) }),
		helmet = /** @type {any} */ ({ slot: 21, typeFlags: word( 3, 1, 1, 1 ) });
	assert.deepEqual( cos.companionItemTargetCommand( tool, helmet ), { kind: "item-use", slot: 25, targetSlot: 21 } );
	assert.deepEqual( [ ...cos.cosItemUseTail( tool.typeFlags, [], { records: [], targetSlot: 21 } ) ], [ 21 ] );
	assert.throws( () => cos.cosItemUseTail( tool.typeFlags, [], { records: [] } ), /Drop the tool/ );
});
