/*
===========================================================================

job-suit-armor.test.mjs - the server's SRO_JOB_SUIT_HIDES_ARMOR reaches the HUD

The world-entry flag is published into the gameplay snapshot the actor
appearance owner reads, and clears on a native reentry.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createGameplay } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ).href
);

test("the server's job-suit armour rule reaches the snapshot and clears on a native reentry", () => {
	const game = createGameplay( () => {} );
	const pose = {
		gid: 7,
		refObjId: 1907,
		kind: "player",
		name: "Tester",
		regionId: 0x6b4f,
		x: 60,
		y: 10,
		z: 100,
		heading: 0
	};
	game.bootstrap( { simulationProtocolVersion: 1, jobSuitHidesArmor: true } );
	game.seed( pose );
	assert.equal( game.take()?.jobSuitHidesArmor, true );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( pose );
	assert.equal( game.take()?.jobSuitHidesArmor, undefined );
	game.dispose();
});
