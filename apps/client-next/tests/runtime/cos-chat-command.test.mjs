/*
===========================================================================

cos-chat-command.test.mjs - the pet's chat commands (6AD990)

The nine UIIT_KEY_COS_* words parse from word 0 of the line and name
the command ui.ts routes onto the COS command dispatch.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { cosChatCommand, cosChatStance } = await import( "../../src/engine/foundation/gameplay/cos-chat-command.ts" );

/** @type {Record<string, string>} */
const TEXT = {
	UIIT_KEY_COS_ATTACK: "/attack",
	UIIT_KEY_COS_RETURN: "/unsummon",
	UIIT_KEY_COS_RIDING: "/board",
	UIIT_KEY_COS_DISMOUNT: "/dismount",
	UIIT_KEY_COS_LEAVEOUT: "/vanish",
	UIIT_KEY_COS_FOLLOW: "/follow",
	UIIT_KEY_COS_AGGRESSIVE: "/offensive",
	UIIT_KEY_COS_DEFENSIVE: "/defensive",
	UIIT_KEY_COS_CHARM: "/charm"
};
/** @param {string} symbol */
const copy = symbol => TEXT[symbol] ?? symbol;

test("each command parses from word 0; board and dismount share the ride toggle", () => {
	const cases = [
		[ "/attack", "attack" ],
		[ "/unsummon", "return" ],
		[ "/board", "ride" ],
		[ "/dismount", "ride" ],
		[ "/vanish", "clean" ],
		[ "/follow", "follow" ],
		[ "/offensive", "aggressive" ],
		[ "/defensive", "defensive" ],
		[ "/charm", "charm" ]
	];
	for ( const [line, kind] of cases ) assert.deepEqual( cosChatCommand( line, copy ), { kind }, line );
});

test("the commands take no argument, so trailing words are ignored", () => {
	assert.deepEqual( cosChatCommand( "/attack that one", copy ), { kind: "attack" } );
	assert.deepEqual( cosChatCommand( "  /follow  ", copy ), { kind: "follow" } );
});

test("the compare is by the localized word, without case", () => {
	/** @param {string} symbol */
	const korean = symbol => symbol === "UIIT_KEY_COS_ATTACK" ? "/공격" : (TEXT[symbol] ?? symbol);
	assert.deepEqual( cosChatCommand( "/공격", korean ), { kind: "attack" } );
	assert.deepEqual( cosChatCommand( "/ATTACK", copy ), { kind: "attack" } );
});

test("other lines are not pet commands", () => {
	for ( const line of [ "attack", "/attackpet", "/attac", "/", "hello", "/party" ] ) {
		assert.equal( cosChatCommand( line, copy ), null, line );
	}
});

/**
 * @param {object} [fields]
 * @returns {any}
 */
function context( fields = {} ) {
	const record = {
		gid: 77,
		refObjId: 1,
		band: 3,
		hp: 10,
		mp: 10,
		status: 0,
		dead: false,
		commandMode: 0,
		...fields
	};
	return { record, reference: undefined, ownerDead: false, mounted: false };
}

test("a stance word orders the change on the attack class", () => {
	assert.deepEqual( cosChatStance( "aggressive", context() ), { kind: "cos-behavior", gid: 77, mode: 1 } );
	assert.deepEqual(
		cosChatStance( "defensive", context( { commandMode: 1 } ) ),
		{ kind: "cos-behavior", gid: 77, mode: 0 }
	);
});

test("a stance word sends nothing where 6A2350 would not", () => {
	// Already in the stance, mode unknown, or a class without one:
	// 82D330 admits band 3 only, so a pickup pet (band 4) whose
	// stance would change still gets no order.
	assert.equal( cosChatStance( "aggressive", context( { commandMode: 1 } ) ), null );
	assert.equal( cosChatStance( "defensive", context( { band: 4, commandMode: 1 } ) ), null );
	assert.equal( cosChatStance( "aggressive", context( { band: 4, commandMode: 0 } ) ), null );
	assert.equal( cosChatStance( "aggressive", context( { commandMode: undefined } ) ), null );
	assert.equal( cosChatStance( "aggressive", context( { band: 2 } ) ), null );
	// No pet, a dead pet, or a dead owner.
	assert.equal( cosChatStance( "aggressive", { ...context(), record: undefined } ), null );
	assert.equal( cosChatStance( "aggressive", context( { dead: true } ) ), null );
	assert.equal( cosChatStance( "aggressive", context( { hp: 0 } ) ), null );
	assert.equal( cosChatStance( "aggressive", { ...context(), ownerDead: true } ), null );
});
