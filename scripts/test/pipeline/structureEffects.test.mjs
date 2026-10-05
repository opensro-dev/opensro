/*
===========================================================================

structureEffects.test.mjs - atstructeffect.txt's stages, sounds and decals

Pins the CItemVisualScript_ParseCommand (4F9880) reading: commands bind to
the last resolvable #TARGET, indices outside a command's range are
dropped, and a particle type outside 0..2 reads as 0.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { parseStructureEffects } from "../../build/char/structureEffects.mjs";

const BYTE_ORDER_MARK = 0xfeff;

const SOURCE = [
	[ String.fromCharCode( BYTE_ORDER_MARK ) + "//header", '"always=0' ],
	[ 'downgrade=1"' ],
	[ "#TARGET", "STRUCTURE_GUARD_TOWER_01" ],
	[ "#BSR", "0", String.raw`res\Artifact\guild\guard tower\guard_tower.bsr` ],
	[ "#BSR", "2", String.raw`res\artifact\guild\guard tower\guard_tower_destory.bsr` ],
	[ "#BSR", "3", String.raw`res\ignored.bsr` ],
	[ "#SOUND", "1", "SND_STRUCT1", "1" ],
	[ "#SOUND", "2", "SND_STRUCT2", "0" ],
	[ "#EFFECT", "1", String.raw`map\guild\structure_damage_fire_b.efp`, "-60", "4.6", "-17", "0", "1", "0" ],
	[ "#EFFECT", "5", String.raw`map\guild\structure_distroy_bomb_a.efp`, "0", "210.4", "-0.4", "0", "0", "7" ],
	[ "#EFFECT", "6", String.raw`map\guild\ignored.efp`, "0", "0", "0", "0", "0", "0" ],
	[ "#TARGET", "STRUCTURE_UNKNOWN" ],
	[ "#BSR", "0", String.raw`res\orphan.bsr` ]
].map( cells => cells.join( "\t" ) ).join( "\r\n" );

test("commands bind to the last resolvable target", () => {
	const targets = parseStructureEffects( SOURCE, codename => codename === "STRUCTURE_GUARD_TOWER_01" );
	assert.deepEqual( [ ...targets.keys() ], [ "STRUCTURE_GUARD_TOWER_01" ] );
	const tower = targets.get( "STRUCTURE_GUARD_TOWER_01" );
	assert.deepEqual( tower.stages, {
		0: "res/artifact/guild/guard tower/guard_tower.bsr",
		2: "res/artifact/guild/guard tower/guard_tower_destory.bsr"
	} );
	assert.deepEqual( tower.sounds, {
		1: { handle: "SND_STRUCT1", shake: true },
		2: { handle: "SND_STRUCT2", shake: false }
	} );
	assert.deepEqual( Object.keys( tower.levels ), [ "1", "5" ] );
	assert.deepEqual( tower.levels[1][0], {
		effectPath: "map/guild/structure_damage_fire_b.efp",
		offset: [ -60, 4.6, -17 ],
		rotation: 0,
		loop: true,
		particle: 0
	} );
	assert.equal( tower.levels[5][0].particle, 0, "a particle type outside 0..2 reads as 0" );
	assert.equal( tower.levels[5][0].loop, false );
});
