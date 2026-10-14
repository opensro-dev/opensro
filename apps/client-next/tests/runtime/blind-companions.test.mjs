/*
===========================================================================

blind-companions.test.mjs - tests for name-visibility.ts's V rules

Holding V hides monsters and players (blindableCharacter, the native class
mask); the port-only Experimental row adds other players' companions
(blindableCompanion). The local player and its own companions always stay.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { blindableCharacter, blindableCompanion } = await import(
	"../../src/engine/foundation/ui/name-visibility.ts"
);

test("V's native mask leaves every COS drawn; the Experimental rule takes only other players' companions", () => {
	const local = 100;
	const pet = { kind: "cos", gid: 7, ownerGid: 200 }, own = { kind: "cos", gid: 8, ownerGid: local };
	assert.equal( blindableCharacter( pet, local ), false, "854680 draws a COS through its owner, ignoring V" );
	assert.equal( blindableCharacter( { kind: "player", gid: 200 }, local ), true );
	assert.equal( blindableCharacter( { kind: "player", gid: local }, local ), false );
	assert.equal( blindableCompanion( pet, local ), true );
	assert.equal( blindableCompanion( own, local ), false, "the player's own pet stays, as the player does" );
	assert.equal( blindableCompanion( { kind: "player", gid: 200 }, local ), false );
	assert.equal( blindableCompanion( { kind: "monster", gid: 9 }, local ), false );
});
