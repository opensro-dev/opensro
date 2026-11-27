/*
===========================================================================

checkStampsShared.test.mjs - one worktree's gate pass is every worktree's

ops #335 row 1: stamps lived in each checkout's .state, so every fresh
worktree paid each skippable gate in full once (the UI action census: 173 s).
They now live in the main checkout's .state; writers race there, so a torn
stamp must read as no pass rather than crash the gate.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdirSync, rmSync, utimesSync, writeFileSync } from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { findPass, findResult, recordPass, recordResult, stampPath } from "../../checks/checkStamps.mjs";
import { resolveMainCheckout } from "../../lib/generatedRoot.mjs";

const repository = fileURLToPath( new URL( "../../../", import.meta.url ) );
const NAME = `shared-stamp-test-${process.pid}`;

test("a stamp is kept in the main checkout, which every worktree resolves to", () => {
	assert.equal(
		path.dirname( stampPath( NAME ) ),
		path.join( resolveMainCheckout( repository ), ".state", "check-stamps" )
	);
});

test("a recorded pass is found, and a torn stamp file is no pass", () => {
	const saved = process.env.SRO_CHECK_FORCE;
	delete process.env.SRO_CHECK_FORCE;
	try {
		recordPass( NAME, "key-a" );
		assert.ok( findPass( NAME, "key-a" ), "the pass just recorded" );
		assert.equal( findPass( NAME, "key-b" ), undefined, "another fingerprint is not a pass" );
		mkdirSync( path.dirname( stampPath( NAME ) ), { recursive: true } );
		writeFileSync( stampPath( NAME ), '[{"fingerprint":"key-a"' );
		assert.equal( findPass( NAME, "key-a" ), undefined, "a half-written stamp is no pass" );
		recordPass( NAME, "key-a" );
		assert.ok( findPass( NAME, "key-a" ), "the next pass replaces the torn file" );
	} finally {
		if ( saved === undefined ) delete process.env.SRO_CHECK_FORCE;
		else process.env.SRO_CHECK_FORCE = saved;
		rmSync( stampPath( NAME ), { force: true } );
	}
});

test("a recorded result is read back whole, the newest four are kept, and force ignores them", () => {
	const saved = process.env.SRO_CHECK_FORCE;
	delete process.env.SRO_CHECK_FORCE;
	const name = `${NAME}-result`;
	try {
		recordResult( name, "key-a", "table a\n" );
		assert.equal( findResult( name, "key-a" ), "table a\n" );
		assert.equal( findResult( name, "key-b" ), undefined, "another fingerprint has no result" );
		process.env.SRO_CHECK_FORCE = "1";
		assert.equal( findResult( name, "key-a" ), undefined, "SRO_CHECK_FORCE=1 rebuilds" );
		delete process.env.SRO_CHECK_FORCE;
		for ( const key of [ "key-b", "key-c", "key-d", "key-e" ] ) recordResult( name, key, key );
		const kept = [ "key-a", "key-b", "key-c", "key-d", "key-e" ].filter( key => findResult( name, key ) );
		assert.equal( kept.length, 4, "a gate keeps its four newest results" );
		assert.equal( findResult( name, "key-e" ), "key-e" );
	} finally {
		if ( saved === undefined ) delete process.env.SRO_CHECK_FORCE;
		else process.env.SRO_CHECK_FORCE = saved;
		rmSync( path.join( path.dirname( stampPath( name ) ), name ), { recursive: true, force: true } );
	}
});

test("the result just written is kept even when every other result looks newer", () => {
	const saved = process.env.SRO_CHECK_FORCE;
	delete process.env.SRO_CHECK_FORCE;
	const name = `${NAME}-tie`, directory = path.join( path.dirname( stampPath( name ) ), name );
	try {
		// Clock ties (results written within one tick) can order the new
		// result anywhere; dating the others ahead of it is the worst case.
		const ahead = new Date( Date.now() + 60_000 );
		for ( const key of [ "key-a", "key-b", "key-c", "key-d" ] ) {
			recordResult( name, key, key );
			utimesSync( path.join( directory, `${key}.txt` ), ahead, ahead );
		}
		recordResult( name, "key-e", "key-e" );
		assert.equal( findResult( name, "key-e" ), "key-e", "the new result survives its own trim" );
		const kept = [ "key-a", "key-b", "key-c", "key-d", "key-e" ].filter( key => findResult( name, key ) );
		assert.equal( kept.length, 4 );
	} finally {
		if ( saved === undefined ) delete process.env.SRO_CHECK_FORCE;
		else process.env.SRO_CHECK_FORCE = saved;
		rmSync( directory, { recursive: true, force: true } );
	}
});
