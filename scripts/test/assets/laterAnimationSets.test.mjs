/*
===========================================================================

laterAnimationSets.test.mjs - the Nasrun import's later body sources

Port-only, not native (SRO_NASRUN, #4407). The import's marker names the
later character BSRs it staged beside the tree and the transformation sets
to take from them. Without the marker, or with no set named, nothing is
merged; a staged path that leaves the tree is refused.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { loadLaterAnimationSources, withLaterAnimationSets } from "../../build/char/laterAnimationSets.mjs";
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
