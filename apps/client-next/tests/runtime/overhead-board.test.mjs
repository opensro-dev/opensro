/*
===========================================================================

overhead-board.test.mjs - an overhead icon never shows without its name

Natively the icon pass (85F5FD) ran past the 300-unit name range, so a far
beginner showed a lone sprout. The board decision now shows the name with
any icon or overhead line, for players and party monsters alike.

===========================================================================
*/

import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const { defaultGameOptions } = await import( "../../src/engine/foundation/gameplay/game-options.ts" );
const { overheadBoardVisible, beginnerMarkShown, nameVisible } = await import(
	"../../src/engine/foundation/ui/name-visibility.ts"
);

const options = defaultGameOptions(),
	local = { gid: 1, refObjId: 1907, kind: "local-player", name: "Me", heading: 0, regionId: 257, x: 0, y: 0, z: 0 },
	far = { ...local, gid: 2, kind: "player", name: "Lacrimosa", x: 1000 };

test("a far beginner shows the name with the sprout", () => {
	const beginner = { ...far, visualFlags: 1 };
	assert.equal( nameVisible( beginner, local, false, options ), false );
	assert.equal( beginnerMarkShown( beginner, options ), true );
	assert.equal( overheadBoardVisible( beginner, local, false, options, undefined, false ), true );
});

test("a far player with no icon or line shows neither", () => {
	assert.equal( overheadBoardVisible( far, local, false, options, undefined, false ), false );
});

test("a far party monster shows its name with the party mark", () => {
	const monster = { ...far, kind: "monster", name: "Graesp" };
	assert.equal(
		overheadBoardVisible( { ...monster, rarityAuxIcon: 1 }, local, false, options, undefined, false ),
		true
	);
	assert.equal(
		overheadBoardVisible( { ...monster, rarityAuxIcon: 0 }, local, false, options, undefined, false ),
		false
	);
});

test("a guild line or status bar brings its name even with player names off", () => {
	const off = { ...options, playerNames: false, guildNames: false };
	const near = { ...far, x: 100 };
	assert.equal( nameVisible( near, local, false, off ), false );
	assert.equal( overheadBoardVisible( near, local, false, off, undefined, true ), true );
});
