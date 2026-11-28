/*
===========================================================================

laterAnimationSets.test.mjs - the Nasrun import's later body sources

Port-only, not native (SRO_NASRUN, #4407). The import's marker names the
later character BSRs it staged beside the tree and the transformation sets
to take from them. Without the marker, or with no set named, nothing is
merged; a staged path that leaves the tree is refused. The sets' state
auras are published once each, as persistent costume effects.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import {
	laterTransformAuras,
	loadLaterAnimationSources,
	withLaterAnimationSets
} from "../../build/char/laterAnimationSets.mjs";
import { NASRUN_SKILL_MARKER } from "../../build/char/visualChanges.mjs";

/*
================
tree

A scratch extracted tree holding marker as the Nasrun import's marker.
================
*/
function tree( marker ) {
	const root = fs.mkdtempSync( path.join( os.tmpdir(), "later-sets-" ) );
	if ( marker ) fs.writeFileSync( path.join( root, NASRUN_SKILL_MARKER ), JSON.stringify( marker ) );
	return root;
}

test("no marker, or a marker naming no set, merges nothing", () => {
	assert.equal( loadLaterAnimationSources( tree( null ) ), null );
	assert.equal( loadLaterAnimationSources( tree( { animationSources: {}, animationSets: [] } ) ), null );
	const bsr = { animationSets: [ { name: "default", states: [] } ] };
	assert.equal( withLaterAnimationSets( bsr, "res/char/china/chinaman_warrior.bsr", null ), bsr );
});

test("the sources key the bodies case- and separator-blind, and stay in the tree", () => {
	const root = tree( {
		animationSources: {
			"RES\\Char\\China\\ChinaMan_Warrior.bsr": ".opensro-nasrun-bsr/res/char/china/chinaman_warrior.bsr"
		},
		animationSets: [ "AVATAR_NASRUN1" ]
	} );
	const later = loadLaterAnimationSources( root );
	assert.deepEqual( [ ...later.sources.keys() ], [ "res/char/china/chinaman_warrior.bsr" ] );
	assert.deepEqual( [ ...later.setNames ], [ "avatar_nasrun1" ] );
	// A body the marker does not name keeps its own sets untouched.
	const bsr = { animationSets: [] };
	assert.equal( withLaterAnimationSets( bsr, "res/char/europe/europeman_knight.bsr", later ), bsr );
	assert.throws(
		() =>
			loadLaterAnimationSources( tree( {
				animationSources: { "res/char/china/chinaman_warrior.bsr": "../outside.bsr" },
				animationSets: [ "avatar_nasrun1" ]
			} ) ),
		/outside the tree/
	);
});

test("the transformation sets' state auras become one persistent row each, and nothing else does", () => {
	const keep03 = {
		field00: 1,
		effectPath: "system\\Avatar_Nasrun01_Skill01_Keep03.efp",
		boneName: "",
		vector3c: [ 0, 0, 0 ]
	};
	const stateAura = ( stateId, set = "avatar_nasrun3", kind = 1 ) => ({
		kind,
		stateId,
		animationSetName: set,
		entries: [ keep03 ]
	});
	const bodies = new Map( [
		[ "man.bsr", { particleModifiers: [ stateAura( 0 ), stateAura( 61 ), stateAura( 6 ) ] } ],
		// Another body repeats the same aura; a set the marker does not name,
		// and an ambient (kind 2) record, never join the costume.
		[ "woman.bsr", {
			particleModifiers: [ stateAura( 0 ), stateAura( 0, "default" ), stateAura( 0, "avatar_nasrun3", 2 ) ]
		} ]
	] );
	const later = {
		sources: new Map( [ [ "a", "man.bsr" ], [ "b", "woman.bsr" ] ] ),
		setNames: new Set( [ "avatar_nasrun3" ] )
	};
	assert.deepEqual( laterTransformAuras( later, file => bodies.get( file ) ), {
		avatar_nasrun3: [
			{
				effectPath: "system/avatar_nasrun01_skill01_keep03.efp",
				bone: "",
				root: true,
				scale: 1,
				offset: [ 0, 0, 0 ]
			}
		]
	} );
	assert.deepEqual( laterTransformAuras( null ), {} );
});
