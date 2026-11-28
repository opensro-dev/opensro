/*
===========================================================================

effectClosureGaps.test.mjs - tests for buildEffectPrograms.mjs
splitEffectClosureGaps

A native build keeps every reachable effect's mesh and texture closure
mandatory and names every hole at once. A variant build publishes an added
effect whose BMS or DDJ source no archive shipped as unavailable, with the
files it lacks; a DDJ without its converted PNG stays fatal in both.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { splitEffectClosureGaps } from "../../build/effects/buildEffectPrograms.mjs";

/*
================
closure

Two effects: one complete, one naming textures/beam_r (absent, as in the
v1.696 archive) and meshes/season_circle.bms (absent), plus a third whose
DDJ is present but did not convert.
================
*/
function closure( expanded ) {
	return splitEffectClosureGaps( {
		effectMeshes: new Map( [
			[ "map/ok.efp", new Set( [ "meshes/ok.bms" ] ) ],
			[ "monster/arabia/skill_ar_cursefire_at6_shot.efp", new Set( [ "meshes/season_circle.bms" ] ) ],
			[ "system/unconverted.efp", new Set() ]
		] ),
		effectTextures: new Map( [
			[ "map/ok.efp", new Set( [ "textures/ok.png" ] ) ],
			[
				"monster/arabia/skill_ar_cursefire_at6_shot.efp",
				new Set( [ "textures/beam_r.png", "textures/ok.png" ] )
			],
			[ "system/unconverted.efp", new Set( [ "textures/raw.png" ] ) ]
		] ),
		hasMesh: ( name ) => name === "meshes/ok.bms",
		hasTexture: ( name ) => name === "textures/ok.png",
		hasTextureSource: ( name ) => name === "textures/ok.png" || name === "textures/raw.png",
		expanded
	} );
}

test("a variant build publishes an effect with archive-absent files as a closure gap", () => {
	const { gapEffects, fatal } = closure( true );
	assert.deepEqual( gapEffects, {
		"monster/arabia/skill_ar_cursefire_at6_shot.efp": [ "meshes/season_circle.bms", "textures/beam_r.ddj" ]
	} );
	assert.deepEqual( fatal, [ "system/unconverted.efp: textures/raw.png has its DDJ source but no converted PNG" ] );
});

test("a native build refuses every hole, named together", () => {
	const { gapEffects, fatal } = closure( false );
	assert.deepEqual( gapEffects, {} );
	assert.deepEqual( fatal, [
		"monster/arabia/skill_ar_cursefire_at6_shot.efp: missing meshes/season_circle.bms, textures/beam_r.ddj",
		"system/unconverted.efp: textures/raw.png has its DDJ source but no converted PNG"
	] );
});
