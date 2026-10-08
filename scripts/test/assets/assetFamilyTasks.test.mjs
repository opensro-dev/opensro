/*
===========================================================================

assetFamilyTasks.test.mjs - every loose family has exactly one task

The task table names the families (it must stay cheap to import), while
build/families/looseFamilies.mjs says how each is produced. A family without
a task is unreachable; a task without a family fails at run time.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { LOOSE_FAMILIES } from "../../build/families/looseFamilies.mjs";
import { REFRESH_FAMILIES } from "../../tasks/assets.mjs";

test("the refresh tasks and the loose family table name the same families", () => {
	assert.deepEqual( [ ...REFRESH_FAMILIES ].sort(), Object.keys( LOOSE_FAMILIES ).sort() );
});

test("every loose family names its pack folder once", () => {
	const folders = Object.values( LOOSE_FAMILIES ).map( family => family.packFolder );
	assert.equal( new Set( folders ).size, folders.length );
});
