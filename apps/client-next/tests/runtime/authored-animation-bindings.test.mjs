import { test } from "node:test";
import assert from "node:assert/strict";
import { authoredAnimationBindings } from "../../../../scripts/build/char/authoredAnimationBindings.mjs";
import { parseCharacterBsr } from "../../../../scripts/build/char/formats.mjs";
import { loadDataAsset } from "../../../../scripts/build/shared/jmxAssetIO.mjs";
import { loadOptionalDataAsset } from "../../../../scripts/build/shared/optionalDataAsset.mjs";
import { readPublishedAssetJsonSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { publicRoot } from "../../../../scripts/build/world/paths.mjs";
import { npcManifestModels } from "../../../../scripts/build/shared/npcManifest.mjs";

test("original NPC and monster state selectors retain independent events without duplicating shared BAN clips", async () => {
	const manifest = readPublishedAssetJsonSync( "/assets/npc/manifest.json", publicRoot ), seen = new Set();
	let selectors = 0, state122 = 0, aliases = 0;
	for ( const [key, row] of Object.entries( npcManifestModels( manifest ) ) ) {
		if ( !/KISAENG|SHAMAN|MANGNYANG/.test( key ) || seen.has( row.bsr ) ) continue;
		seen.add( row.bsr );
		const bsr = parseCharacterBsr( await loadDataAsset( row.bsr ), row.bsr ),
			clips = [],
			bindings = await authoredAnimationBindings( bsr, clips, loadOptionalDataAsset );
		assert.equal( bindings.length, bsr.animationSets.reduce( ( n, s ) => n + s.states.length, 0 ) );
		assert.equal( new Set( clips.map( c => c.path ) ).size, clips.length );
		for ( const set of bsr.animationSets ) {
			for ( const state of set.states ) {
				const binding = bindings.find( b => b.set === set.name && b.stateId === state.stateId );
				assert.ok( binding );
				if ( binding.clip ) {
					assert.equal( clips.find( c => c.role === binding.clip ).path, state.animationPath );
					selectors++;
					if ( state.stateId === 122 ) state122++;
				} else assert.ok( binding.reason );
			}
		}
		if ( /KISAENG2|SHAMAN/.test( key ) ) {
			assert.ok( bsr.particleModifiers.some( m => m.stateId === 6 ) );
			assert.ok(
				!bindings.some( b => b.stateId === 6 ),
				"unreferenced particle selector manufactured an animation"
			);
		}
		aliases += bindings.filter( b => b.clip ).length - clips.length;
	}
	assert.ok( selectors > 20 );
	assert.ok( state122 > 0 );
	assert.ok( aliases > 0 );
});

test("absent and malformed optional BAN resources remain per-state failures and are read once", async () => {
	const state = ( stateId, animationPath ) => ({ stateId, animationPath }),
		bsr = {
			animationSets: [ {
				name: "default",
				states: [ state( 1, "missing" ), state( 2, "missing" ), state( 3, "bad" ), state( 4, null ) ]
			} ]
		},
		reads = [];
	const result = await authoredAnimationBindings( bsr, [], async path => {
		reads.push( path );
		return path === "missing" ? null : Buffer.from( "invalid" );
	} );
	assert.deepEqual( reads, [ "missing", "bad" ] );
	assert.deepEqual( result.map( r => r.reason ), [
		"absent-animation",
		"absent-animation",
		"unreadable-animation",
		"no-authored-animation"
	] );
	assert.ok( result.every( r => r.clip === null ) );
});
