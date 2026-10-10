/*
===========================================================================
animation-sound-bindings.test.mjs - native whole-motion-binding sound fallback.
Synthetic bindings distinguish absence from silence; licensed Exorcist data
proves staff inheritance without replacing cart silence or exact staff cues.
===========================================================================
*/
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";
import { pickAnimationSetSoundEvents, pickDefaultSetSoundEvents } from "../../build/char/animationUtils.mjs";
import { parseCharacterBsr } from "../../build/char/formats.mjs";
import { resolveRoster } from "../../build/char/resolveCharRoster.mjs";
import { loadDataAsset } from "../../build/shared/jmxAssetIO.mjs";
import { retailTextdataRoot } from "../../build/world/paths.mjs";

/*
================
binding
================
*/
function binding( animationSetName, stateId = 7, count = 1 ) {
	return { kind: 1, animationSetName, stateId, count };
}

/*
================
sound
================
*/
function sound( animationSetName, stateId = 7, cursorMs = 289 ) {
	return {
		...binding( animationSetName, stateId ),
		entries: [ { animationName: "default", tracks: [ { triggerFrame: cursorMs, cueName: "snd_run1" } ] } ]
	};
}

/*
================
fixture
================
*/
function fixture() {
	return { modifierSets: [ binding( "default" ) ], soundModifiers: [ sound( "default" ) ] };
}

/*
================
expected
================
*/
function expected( cursorMs ) {
	return [ { cursorMs, cue: "snd_run1" } ];
}

test("exact named sound binding overrides default case-insensitively", () => {
	const bsr = fixture();
	bsr.modifierSets.push( binding( "TWOHAND_STAFF" ) );
	bsr.soundModifiers.push( sound( "TWOHAND_STAFF", 7, 234 ) );
	assert.deepEqual( pickAnimationSetSoundEvents( bsr, "twohand_staff", 7 ), expected( 234 ) );
});

test("missing whole binding inherits default for arbitrary sets and motion states", () => {
	for ( const stateId of [ 0, 1, 7, 80, 131 ] ) {
		const bsr = {
			modifierSets: [ binding( "default", stateId ) ],
			soundModifiers: [ sound( "default", stateId ) ]
		};
		assert.deepEqual( pickAnimationSetSoundEvents( bsr, "other_set", stateId ), expected( 289 ) );
		assert.deepEqual( pickDefaultSetSoundEvents( bsr, stateId, "SND_RUN1" ), expected( 289 ) );
		assert.deepEqual( pickAnimationSetSoundEvents( bsr, "other_set", stateId, "other_cue" ), [] );
	}
});

test("explicit zero-modifier and non-sound bindings both suppress fallback", () => {
	for ( const count of [ 0, 1, 3 ] ) {
		const bsr = fixture();
		bsr.modifierSets.push( binding( "twohand_staff", 7, count ) );
		assert.deepEqual( pickAnimationSetSoundEvents( bsr, "twohand_staff", 7 ), [] );
	}
});

test("explicit empty sound entries and empty variant tracks suppress fallback", () => {
	for (
		const entries of [ [], [ { animationName: "default", tracks: [] } ], [ {
			animationName: "alternate",
			tracks: []
		} ] ]
	) {
		const bsr = fixture();
		bsr.modifierSets.push( binding( "twohand_staff" ) );
		bsr.soundModifiers.push( { ...binding( "twohand_staff" ), entries } );
		assert.deepEqual( pickAnimationSetSoundEvents( bsr, "twohand_staff", 7 ), [] );
	}
});

test("bindings for another state or modifier kind do not shadow same-state default", () => {
	const bsr = fixture();
	bsr.modifierSets.push( binding( "twohand_staff", 1 ), { ...binding( "twohand_staff" ), kind: 2 } );
	assert.deepEqual( pickAnimationSetSoundEvents( bsr, "twohand_staff", 7 ), expected( 289 ) );
	assert.deepEqual( pickAnimationSetSoundEvents( bsr, "twohand_staff", 1 ), [] );
	assert.deepEqual( pickAnimationSetSoundEvents( bsr, "absent", 80 ), [] );
	assert.deepEqual( pickAnimationSetSoundEvents( {}, "absent", 7 ), [] );
});

test("whole binding stays exact across variants and retains existing variant selection", () => {
	const bsr = fixture();
	bsr.modifierSets.push( binding( "twohand_staff" ) );
	const exact = sound( "twohand_staff", 7, 234 );
	exact.entries.unshift( { animationName: "alternate", tracks: [ { triggerFrame: 100, cueName: "voice" } ] } );
	bsr.soundModifiers.push( exact );
	assert.deepEqual( pickAnimationSetSoundEvents( bsr, "twohand_staff", 7 ), expected( 234 ) );
	exact.entries.pop();
	assert.deepEqual( pickAnimationSetSoundEvents( bsr, "twohand_staff", 7 ), [ { cursorMs: 100, cue: "voice" } ] );
});

test("licensed Exorcist staff inherits 289/619 while exact onehand staff and cart remain authored", async () => {
	const roster = resolveRoster( retailTextdataRoot, [ "CHAR_EU_MAN_EXORCIST" ] );
	assert.deepEqual( roster.missing, [] );
	assert.equal( roster.resolved.length, 1 );
	const model = roster.resolved[0];
	const bytes = await loadDataAsset( model.bsrPath );
	assert.equal(
		createHash( "sha256" ).update( bytes ).digest( "hex" ),
		"8a4e62268f99acf938ebb5dd9fee2fe73e84b4077307c01f4dafbff75b115a09"
	);
	const bsr = parseCharacterBsr( bytes, model.bsrPath );
	assert.equal(
		bsr.modifierSets.some( row => row.animationSetName === "twohand_staff" && row.stateId === 7 ),
		false
	);
	assert.deepEqual( pickDefaultSetSoundEvents( bsr, 7 ), [ ...expected( 289 ), ...expected( 619 ) ] );
	assert.deepEqual( pickAnimationSetSoundEvents( bsr, "twohand_staff", 7 ), [
		...expected( 289 ),
		...expected( 619 )
	] );
	assert.deepEqual( pickAnimationSetSoundEvents( bsr, "onehand_staff", 7 ), [
		...expected( 234 ),
		...expected( 597 )
	] );
	for ( const stateId of [ 1, 7 ] ) {
		assert.ok(
			bsr.modifierSets.some( row => row.kind === 1 && row.animationSetName === "cart" && row.stateId === stateId )
		);
		assert.deepEqual( pickAnimationSetSoundEvents( bsr, "cart", stateId ), [] );
	}
});
