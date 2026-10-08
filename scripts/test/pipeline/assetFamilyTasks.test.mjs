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
import { PUBLISH_FAMILIES, REFRESH_FAMILIES } from "../../tasks/assets.mjs";

test("the task lists and the loose family table name the same families, by kind", () => {
	const byKind = kind =>
		Object.entries( LOOSE_FAMILIES ).filter( ( [, family] ) => family.kind === kind ).map(
			( [name] ) => name
		).sort();
	assert.deepEqual( [ ...REFRESH_FAMILIES ].sort(), byKind( "refresh" ) );
	assert.deepEqual( [ ...PUBLISH_FAMILIES ].sort(), byKind( "publish" ) );
});

test("every loose family names its pack folder once", () => {
	const folders = Object.values( LOOSE_FAMILIES ).map( family => family.packFolder );
	assert.equal( new Set( folders ).size, folders.length );
});

test("the effect family runs after entity-bsr, whose program rebuild changes the closure it records", () => {
	const order = Object.keys( LOOSE_FAMILIES );
	assert.ok( order.indexOf( "entity-bsr" ) < order.indexOf( "effect" ) );
});
