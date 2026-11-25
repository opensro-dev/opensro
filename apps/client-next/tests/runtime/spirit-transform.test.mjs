/*
===========================================================================

spirit-transform.test.mjs - the Devil's Spirit transformation on its caster

Port-only, not native (SRO_NASRUN, #4407). While a lent Spirit skill's buff
is attached, its costume replaces the caster's worn avatars, and the
costume's pieces select the transformation's own animation set through the
worn-avatar override; when the buff leaves, both leave with it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

/*
================
load
================
*/
async function load( entry ) {
	return import( sourceFileUrl( entry ).href );
}
const { visualChangeAvatars } = await load( "src/engine/foundation/animation/visual-changes.ts" );
const { selectAvatarOverride } = await load( "src/engine/foundation/animation/avatar-override.ts" );

const CASTER = 7, OTHER = 8, SKILL = 31275, HAT = 2000125100, BODY = 2000125102;
const VISUAL_CHANGES = { [SKILL]: [ HAT, BODY ] };

test("a lent skill's buff dresses its own caster alone, and only while attached", () => {
	const attached = [ { gid: OTHER, skill: 1 }, { gid: CASTER, skill: SKILL } ];
	assert.deepEqual( visualChangeAvatars( CASTER, attached, VISUAL_CHANGES ).map( row => row.refObjId ), [
		HAT,
		BODY
	] );
	assert.equal( visualChangeAvatars( OTHER, attached, VISUAL_CHANGES ), undefined );
	assert.equal( visualChangeAvatars( CASTER, [ { gid: CASTER, skill: 1 } ], VISUAL_CHANGES ), undefined );
	assert.equal( visualChangeAvatars( CASTER, [], VISUAL_CHANGES ), undefined );
	assert.equal( visualChangeAvatars( CASTER, attached, undefined ), undefined );
});

test("the costume selects the transformation set, and its removal returns the native set", () => {
	const overrides = {
		[HAT]: { animation: "avatar_nasrun3", priority: 0, additionalBsr: "" },
		[BODY]: { animation: "avatar_nasrun3", priority: 0, additionalBsr: "" }
	};
	const worn = selectAvatarOverride( undefined, [ HAT, BODY ], overrides );
	assert.equal( overrides[worn.selected].animation, "avatar_nasrun3" );
	// The buff leaves: the costume's ids leave the worn list, no override stays.
	assert.equal( selectAvatarOverride( worn, [], overrides ).selected, undefined );
});
