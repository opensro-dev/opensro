/*
===========================================================================

effectClosureGaps.test.mjs - tests for buildEffectPrograms.mjs
splitEffectClosureGaps

A native build keeps every reachable effect's mesh and texture closure
mandatory and names every hole at once. A variant build publishes an added
effect as unavailable only when every file it lacks is on the committed
never-shipped list (scripts/build/reference/effect-closure-gaps.json); any
other missing file means the extraction is incomplete and stays fatal, as
does a DDJ without its converted PNG and an ambiguous mesh name.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import { test } from "node:test";
import {
	EFFECT_CLOSURE_GAP_EVIDENCE,
	effectClosureRerunHint,
	splitEffectClosureGaps
} from "../../build/effects/buildEffectPrograms.mjs";

// The list the builder reads, as committed.
const NEVER_SHIPPED = new Set(
	Object.keys(
		JSON.parse( fs.readFileSync( new URL( `../../../${EFFECT_CLOSURE_GAP_EVIDENCE}`, import.meta.url ), "utf8" ) )
			.files
	)
);
const CURSEFIRE = "monster/arabia/skill_ar_cursefire_at6_shot.efp";

/*
================
closure

Four effects: one complete; the v1.696 cursefire shot naming beam_r and
season_circle (both on the list); one naming a mesh an archive ships but
the extraction lost; and one whose DDJ is present but did not convert.
================
*/
function closure( expanded ) {
	return splitEffectClosureGaps( {
		effectMeshes: new Map( [
			[ "map/ok.efp", new Set( [ "meshes/ok.bms" ] ) ],
			[ CURSEFIRE, new Set( [ "meshes/season_circle.bms" ] ) ],
			[ "system/lost.efp", new Set( [ "meshes/lost.bms" ] ) ],
			[ "system/unconverted.efp", new Set() ]
		] ),
		effectTextures: new Map( [
			[ "map/ok.efp", new Set( [ "textures/ok.png" ] ) ],
			[ CURSEFIRE, new Set( [ "textures/beam_r.png", "textures/ok.png" ] ) ],
			[ "system/lost.efp", new Set( [ "textures/ok.png" ] ) ],
			[ "system/unconverted.efp", new Set( [ "textures/raw.png" ] ) ]
		] ),
		hasMesh: ( name ) => name === "meshes/ok.bms",
		hasTexture: ( name ) => name === "textures/ok.png",
		hasTextureSource: ( name ) => name === "textures/ok.png" || name === "textures/raw.png",
		neverShipped: NEVER_SHIPPED,
		expanded
	} );
}

test("the committed list holds the files no archive ships, and not one an archive does", () => {
	assert.ok( NEVER_SHIPPED.has( "textures/beam_r.ddj" ) );
	assert.ok( NEVER_SHIPPED.has( "meshes/season_circle.bms" ) );
	// v1.696 ships beam.ddj and beam_p.ddj; only beam_r is missing.
	assert.ok( !NEVER_SHIPPED.has( "textures/beam.ddj" ) );
});

test("a variant build publishes only an effect whose every missing file no archive ships", () => {
	const { gapEffects, fatal } = closure( true );
	assert.deepEqual( gapEffects, { [CURSEFIRE]: [ "meshes/season_circle.bms", "textures/beam_r.ddj" ] } );
	assert.deepEqual( fatal, [
		"system/lost.efp: meshes/lost.bms is missing from the extracted tree, and an archive ships it",
		"system/unconverted.efp: textures/raw.png has its DDJ source but no converted PNG"
	] );
});

test("a variant effect missing one listed and one shipped file is fatal for the shipped one", () => {
	const { gapEffects, fatal } = splitEffectClosureGaps( {
		effectMeshes: new Map( [ [ CURSEFIRE, new Set( [ "meshes/lost.bms" ] ) ] ] ),
		effectTextures: new Map( [ [ CURSEFIRE, new Set( [ "textures/beam_r.png" ] ) ] ] ),
		hasMesh: () => false,
		hasTexture: () => false,
		hasTextureSource: () => false,
		neverShipped: NEVER_SHIPPED,
		expanded: true
	} );
	assert.deepEqual( gapEffects, {} );
	assert.deepEqual( fatal, [
		`${CURSEFIRE}: meshes/lost.bms is missing from the extracted tree, and an archive ships it`
	] );
});

test("a native build refuses every hole, named together", () => {
	const { gapEffects, fatal } = closure( false );
	assert.deepEqual( gapEffects, {} );
	assert.deepEqual( fatal, [
		`${CURSEFIRE}: meshes/season_circle.bms is missing (no archive ships it; only a variant build may publish its effect as unavailable)`,
		`${CURSEFIRE}: textures/beam_r.ddj is missing (no archive ships it; only a variant build may publish its effect as unavailable)`,
		"system/lost.efp: meshes/lost.bms is missing from the extracted tree, and an archive ships it",
		"system/unconverted.efp: textures/raw.png has its DDJ source but no converted PNG"
	] );
});

test("an ambiguous mesh name joins the list instead of stopping the scan", () => {
	const { fatal } = splitEffectClosureGaps( {
		effectMeshes: new Map( [ [ "a.efp", new Set( [ "x.bms" ] ) ], [ "b.efp", new Set( [ "meshes/lost.bms" ] ) ] ] ),
		effectTextures: new Map(),
		hasMesh: ( name ) => {
			if ( name === "x.bms" ) throw new Error( "[effect-programs] ambiguous BMS x.bms: a/x.bms, b/x.bms" );
			return false;
		},
		hasTexture: () => false,
		hasTextureSource: () => false,
		neverShipped: NEVER_SHIPPED,
		expanded: true
	} );
	assert.deepEqual( fatal, [
		"a.efp: [effect-programs] ambiguous BMS x.bms: a/x.bms, b/x.bms",
		"b.efp: meshes/lost.bms is missing from the extracted tree, and an archive ships it"
	] );
});

test("the rerun hint names the native extraction or the variant root with both later clients", () => {
	assert.match( effectClosureRerunHint( false, "G:/sro" ), /pnpm assets prepare/ );
	const variant = effectClosureRerunHint( true, "G:/variant" );
	assert.match( variant, /build_alexandria_variant\.py --variant-root G:\/variant/ );
	assert.match( variant, /SRO_VSRO_ROOT=.*SRO_V1696_ROOT=/ );
});
