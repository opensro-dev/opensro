/*
===========================================================================

generatedAssetLock.test.mjs - publication and verification across worktrees

Real child processes use copied, unmodified Node and Python lock owners.
Synthetic junctions and SRO_GENERATED_ROOT redirects exercise the same shared
generated tree as local worktrees without reading or modifying licensed assets
or another process's locks.

===========================================================================
*/

import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { copyFile, mkdir, mkdtemp, readFile, rm, symlink } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const SCRIPTS_ROOT = fileURLToPath( new URL( "../../", import.meta.url ) );
// The caller's generated-root override would point every child at the real tree.
const LOCK_ENV = [ "SRO_REBUILD_LOCK_NAME", "SRO_REBUILD_LOCK_TOKEN", "SRO_REBUILD_LOCK_DIR", "SRO_GENERATED_ROOT" ];
// The lock owners and the generated-root owners they resolve the tree through.
const LOCK_SOURCES = [ "rebuildLock.mjs", "rebuild_lock.py", "sro_paths.py", "lib/generatedRoot.mjs" ];
const NODE_DRIVER = `
const { withGeneratedAssetsLock } = await import('./scripts/rebuildLock.mjs');
await withGeneratedAssetsLock('test reader or publisher', async () => {
  console.log('ENTERED PID=' + process.pid);
  if (process.env.HOLD === '1') {
    await new Promise(resolve => process.stdin.once('data', resolve));
  }
  if (process.env.FAIL === '1') throw new Error('intentional callback failure');
});
`;
const PYTHON_DRIVER = `
import os, sys
sys.path.insert(0, 'scripts')
from rebuild_lock import generated_assets_lock
with generated_assets_lock('test reader or publisher'):
    print('ENTERED PID=%d' % os.getpid(), flush=True)
    if os.environ.get('HOLD') == '1':
        sys.stdin.readline()
    if os.environ.get('FAIL') == '1':
        raise RuntimeError('intentional callback failure')
`;

/*
================
fixture
================
*/
async function fixture( t ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-asset-lock-" ) );
	const children = [];
	t.after( async () => {
		for ( const child of children ) {
			if ( child.exitCode === null ) child.kill();
		}
		await Promise.all( children.map( ( child ) => child.finished ) );
		await rm( root, { recursive: true, force: true } );
	} );
	const owner = path.join( root, "owner" );
	const alias = path.join( root, "alias" );
	const separate = path.join( root, "separate" );
	const redirected = path.join( root, "redirected" );
	for ( const checkout of [ owner, alias, separate, redirected ] ) {
		await mkdir( path.join( checkout, "scripts", "lib" ), { recursive: true } );
		for ( const name of LOCK_SOURCES ) {
			await copyFile( path.join( SCRIPTS_ROOT, name ), path.join( checkout, "scripts", name ) );
		}
	}
	await mkdir( path.join( owner, ".generated" ) );
	await mkdir( path.join( separate, ".generated" ) );
	await symlink( path.join( owner, ".generated" ), path.join( alias, ".generated" ), "junction" );
	return {
		root,
		owner,
		alias,
		separate,
		redirected,
		children,
		lockDir: path.join( owner, ".state/locks/generated-assets.lock" )
	};
}

/*
================
start
================
*/
function start( f, options ) {
	const env = { ...process.env };
	for ( const key of LOCK_ENV ) delete env[key];
	Object.assign( env, {
		SRO_REBUILD_LOCK_TIMEOUT_MS: "500",
		SRO_REBUILD_LOCK_POLL_MS: "250",
		SRO_REBUILD_LOCK_STALE_MS: "60000",
		...options.env
	} );
	const python = options.language === "python";
	const child = spawn(
		python ? (process.platform === "win32" ? "py" : "python3") : process.execPath,
		python ? [ "-B", "-c", PYTHON_DRIVER ] : [ "--input-type=module", "-e", NODE_DRIVER ],
		{ cwd: options.cwd, env, windowsHide: true, stdio: [ "pipe", "pipe", "pipe" ] }
	);
	child.output = "";
	child.stdout.on( "data", ( chunk ) => child.output += chunk );
	child.stderr.on( "data", ( chunk ) => child.output += chunk );
	child.finished = new Promise( ( resolve, reject ) => {
		child.once( "error", reject );
		child.once( "close", resolve );
	} );
	f.children.push( child );
	return child;
}

/*
================
entered
================
*/
async function entered( child ) {
	const deadline = Date.now() + 10000;
	while ( !child.output.includes( "ENTERED" ) ) {
		assert.equal( child.exitCode, null, child.output );
		assert.ok( Date.now() < deadline, child.output );
		await new Promise( ( resolve ) => setTimeout( resolve, 20 ) );
	}
}

for ( const holderLanguage of [ "node", "python" ] ) {
	for ( const readerLanguage of [ "node", "python" ] ) {
		test(`${holderLanguage} publisher blocks ${readerLanguage} verification through SRO_GENERATED_ROOT`, async ( t ) => {
			const f = await fixture( t );
			const holder = start( f, { cwd: f.owner, language: holderLanguage, env: { HOLD: "1" } } );
			await entered( holder );
			const shared = { SRO_GENERATED_ROOT: path.join( f.owner, ".generated" ) };
			const reader = start( f, { cwd: f.redirected, language: readerLanguage, env: shared } );
			assert.notEqual( await reader.finished, 0, reader.output );
			assert.match( reader.output, /timed out waiting/ );
			holder.stdin.end( "\n" );
			assert.equal( await holder.finished, 0, holder.output );
			const nextReader = start( f, { cwd: f.redirected, language: readerLanguage, env: shared } );
			assert.equal( await nextReader.finished, 0, nextReader.output );
			assert.match( nextReader.output, /ENTERED/ );
		});

		test(`${holderLanguage} publisher blocks ${readerLanguage} verification through a junction`, async ( t ) => {
			const f = await fixture( t );
			const holder = start( f, { cwd: f.owner, language: holderLanguage, env: { HOLD: "1" } } );
			await entered( holder );
			const reader = start( f, { cwd: f.alias, language: readerLanguage } );
			assert.notEqual( await reader.finished, 0, reader.output );
			assert.match( reader.output, /timed out waiting/ );
			assert.doesNotMatch( reader.output, /ENTERED/ );
			assert.equal( holder.exitCode, null );
			holder.stdin.end( "\n" );
			assert.equal( await holder.finished, 0, holder.output );
			const nextReader = start( f, { cwd: f.alias, language: readerLanguage } );
			assert.equal( await nextReader.finished, 0, nextReader.output );
			assert.match( nextReader.output, /ENTERED/ );
		});
	}

	test(`${holderLanguage} does not steal a lock before owner metadata is published`, async ( t ) => {
		const f = await fixture( t );
		await mkdir( f.lockDir, { recursive: true } );
		const reader = start( f, { cwd: f.alias, language: holderLanguage } );
		assert.notEqual( await reader.finished, 0, reader.output );
		assert.match( reader.output, /timed out waiting/ );
		assert.doesNotMatch( reader.output, /ENTERED/ );
	});

	test(`${holderLanguage} releases ownership when verification fails`, async ( t ) => {
		const f = await fixture( t );
		const failed = start( f, { cwd: f.alias, language: holderLanguage, env: { FAIL: "1" } } );
		assert.notEqual( await failed.finished, 0, failed.output );
		assert.match( failed.output, /intentional callback failure/ );
		const reader = start( f, { cwd: f.owner, language: holderLanguage } );
		assert.equal( await reader.finished, 0, reader.output );
	});

	test(`${holderLanguage} inherits only ownership of the same physical tree`, async ( t ) => {
		const f = await fixture( t );
		const holder = start( f, { cwd: f.owner, language: "node", env: { HOLD: "1" } } );
		await entered( holder );
		const owner = JSON.parse( await readFile( path.join( f.lockDir, "owner.json" ), "utf8" ) );
		const inherited = {
			SRO_REBUILD_LOCK_NAME: owner.name,
			SRO_REBUILD_LOCK_TOKEN: owner.token,
			SRO_REBUILD_LOCK_DIR: owner.lockDir
		};
		const nested = start( f, { cwd: f.alias, language: holderLanguage, env: inherited } );
		assert.equal( await nested.finished, 0, nested.output );
		const separate = start( f, { cwd: f.separate, language: holderLanguage, env: { ...inherited, HOLD: "1" } } );
		await entered( separate );
		const separateOwner = JSON.parse(
			await readFile(
				path.join( f.separate, ".state/locks/generated-assets.lock/owner.json" ),
				"utf8"
			)
		);
		assert.equal( separateOwner.pid, Number( separate.output.match( /ENTERED PID=(\d+)/ )[1] ) );
		assert.notEqual( separateOwner.token, owner.token );
		separate.stdin.end( "\n" );
		holder.stdin.end( "\n" );
		assert.equal( await separate.finished, 0, separate.output );
		assert.equal( await holder.finished, 0, holder.output );
	});
}
