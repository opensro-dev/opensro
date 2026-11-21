/*
===========================================================================

auditSharedRules.test.mjs - the build rules the BSR parity audit asks

The audit re-checks published rows against the retail files. Three build
rules decide what "published correctly" means, and the audit calls the
build's own function for each: owned models have no retail BSR, a compound
bake keeps skill-scoped animation sets, and an all-zero-length archive
effect is published as native-unavailable.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { isOwnedModel } from "../../build/char/ownedModels.mjs";
import { CROW_EGG_BSR } from "../../build/char/crowEgg.mjs";
import { PORT_PET_DROP_MODEL } from "../../build/shared/portItemRows.mjs";
import { skillScopedAnimationSets } from "../../build/char/npcResourceBake.mjs";
import { isEmptyArchiveEffect } from "../../build/effects/buildEffectPrograms.mjs";

/*
================
owned models
================
*/
test("owned models are the egg as an npc and the pet gear as an itemdrop only", () => {
	assert.equal( isOwnedModel( "npc", CROW_EGG_BSR ), true );
	assert.equal( isOwnedModel( "itemdrop", `res/${PORT_PET_DROP_MODEL}` ), true );
	// The domain is part of the identity: the same path elsewhere is retail.
	assert.equal( isOwnedModel( "itemdrop", CROW_EGG_BSR ), false );
	assert.equal( isOwnedModel( "npc", `res/${PORT_PET_DROP_MODEL}` ), false );
	assert.equal( isOwnedModel( "npc", "res/mob/jupiter/jupiter.bsr" ), false );
});

/*
================
compound animation scope
================
*/
test("a compound keeps the default set whole and skill-selected states of the others", () => {
	const state = id => ({ stateId: id, animationPath: `a${id}.ban` });
	const sets = [
		{ name: "Default", states: [ state( 1 ), state( 2 ) ] },
		{ name: "Spear", states: [ state( 10 ), state( 11 ), state( 12 ) ] },
		{ name: "bow", states: [ state( 10 ) ] },
		{ name: "sword", states: [ state( 20 ) ] }
	];
	const skillStates = new Map( [ [ "spear", new Set( [ 11, 12 ] ) ], [ "sword", new Set( [ 21 ] ) ] ] );
	const scoped = skillScopedAnimationSets( sets, skillStates );
	assert.deepEqual( scoped.map( set => [ set.name, set.states.map( s => s.stateId ) ] ), [
		[ "Default", [ 1, 2 ] ],
		[ "Spear", [ 11, 12 ] ]
	] );
	// The input sets are not narrowed in place.
	assert.equal( sets[1].states.length, 3 );
	assert.deepEqual( skillScopedAnimationSets( undefined, skillStates ), [] );
});

/*
================
empty archive effects
================
*/
test("only an effect whose every archive file is zero-length is empty", () => {
	const dir = fs.mkdtempSync( path.join( os.tmpdir(), "empty-efp-" ) );
	try {
		const file = ( name, bytes ) => {
			const at = path.join( dir, name.replaceAll( "/", "_" ) );
			fs.writeFileSync( at, Buffer.alloc( bytes ) );
			return [ name, at ];
		};
		const index = new Map( [
			file( "dun/empty.efp", 0 ),
			file( "dun/full.efp", 16 ),
			file( "a/twin.efp", 0 ),
			file( "b/twin.efp", 0 ),
			file( "a/mixed.efp", 0 ),
			file( "b/mixed.efp", 4 )
		] );
		assert.equal( isEmptyArchiveEffect( "dun/empty.efp", index ), true );
		assert.equal( isEmptyArchiveEffect( "dun/full.efp", index ), false );
		// Absent is the certificate's case, not this one.
		assert.equal( isEmptyArchiveEffect( "dun/absent.efp", index ), false );
		// A bare name resolves to every basename match; all must be empty.
		assert.equal( isEmptyArchiveEffect( "twin.efp", index ), true );
		assert.equal( isEmptyArchiveEffect( "mixed.efp", index ), false );
	} finally {
		fs.rmSync( dir, { recursive: true, force: true } );
	}
});
