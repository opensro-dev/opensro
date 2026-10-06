/*
===========================================================================

workspaceGuard.test.mjs - tasks refuse a linked node_modules

A node_modules link (a Windows junction or a symlink) at the root or in a
workspace package is reported and refused; real directories and missing
ones pass. The link is removed without following it.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmdirSync, rmSync, symlinkSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { assertRealNodeModules, linkedNodeModules } from "../../tasks/workspaceGuard.mjs";

test("a linked package node_modules is refused; real and missing ones pass", t => {
	const root = mkdtempSync( path.join( os.tmpdir(), "workspace-guard-" ) );
	const elsewhere = mkdtempSync( path.join( os.tmpdir(), "workspace-guard-target-" ) );
	const link = path.join( root, "apps", "client", "node_modules" );
	t.after( () => {
		try {
			rmdirSync( link ); // the link itself, never what it points at
		} catch {
			// already removed
		}
		rmSync( root, { recursive: true, force: true } );
		rmSync( elsewhere, { recursive: true, force: true } );
	} );
	mkdirSync( path.join( root, "node_modules" ) );
	mkdirSync( path.join( root, "apps", "client" ), { recursive: true } );
	mkdirSync( path.join( root, "apps", "server" ) );
	assert.deepEqual( linkedNodeModules( root ), [] );
	assert.doesNotThrow( () => assertRealNodeModules( root ) );

	symlinkSync( elsewhere, link, "junction" );
	assert.deepEqual( linkedNodeModules( root ), [ link ] );
	assert.throws( () => assertRealNodeModules( root ), /must be a real directory/ );
});
