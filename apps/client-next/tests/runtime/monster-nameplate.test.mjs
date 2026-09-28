/*
===========================================================================

monster-nameplate.test.mjs - auxiliary party status is independent of grade

Keep the nameplate mark attached to the native auxiliary byte across all
monster grades, and never infer it from a name, target selection or skin.

===========================================================================
*/

import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const { monsterPartyNameplate } = await import( "../../src/engine/foundation/ui/monster-nameplate.ts" );
const monster = { gid: 2, refObjId: 10, kind: "monster", regionId: 1, x: 0, y: 0, z: 0, heading: 0, name: "Graesp" };

test("party marks cover every base grade and move with measured name width", () => {
	for ( let rarity = 0; rarity < 16; rarity++ ) {
		const mark = monsterPartyNameplate( { ...monster, rarity, rarityAuxIcon: 1 }, [ 50, 14 ] );
		assert.ok( mark );
		assert.deepEqual( mark.rect, [ -46.5, -8.5, 16, 16 ] );
		assert.ok( mark.path.endsWith( "/europe_partymob.png" ) );
	}
	assert.equal( monsterPartyNameplate( { ...monster, rarityAuxIcon: 1 }, [ 80, 14 ] )?.rect[0], -61.5 );
});

test("ordinary monsters and other entity kinds cannot inherit a party icon", () => {
	for ( const rarityAuxIcon of [ undefined, 0, 2, 15 ] ) {
		assert.equal( monsterPartyNameplate( { ...monster, rarityAuxIcon }, [ 50, 14 ] ), null );
	}
	for ( const kind of [ "player", "local-player", "npc", "cos", "ground-item" ] ) {
		assert.equal( monsterPartyNameplate( { ...monster, kind, rarityAuxIcon: 1 }, [ 50, 14 ] ), null );
	}
	assert.equal( monsterPartyNameplate( { ...monster, rarityAuxIcon: 1 }, [ 50, 0 ] ), null );
});
