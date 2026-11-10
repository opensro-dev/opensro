/*
===========================================================================

localRun.test.mjs - `pnpm local` names its own stops (#3081)

The local runner's three pre-boot behaviours over fixtures, never a
live stack: an authority behind the current schema is copied aside and
upgraded (a refused one is never touched), the doctor compares the
client's startup assets against the published pack manifest, and a
stdin close announces itself. The upgrade tool is Go's and has its own
suite; here the tool is the injected `run` seam over a fixture
authority directory.

===========================================================================
*/
import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import {
	authorityDirFor,
	authorityIsCurrent,
	unpublishedStartupAssets,
	upgradeLocalAuthority
} from "../../local/preboot.mjs";
import { stopAnnouncement } from "../../local/run.mjs";

const REPO_ROOT = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", "..", ".." );

// The two verdicts the offline upgrader prints
// (cmd/tools/sro-authority-upgrade), and one refusal.
const VALIDATED_BEHIND = {
	status: 0,
	stdout: "Source authority validated; rerun with -commit to upgrade.",
	stderr: ""
};
const VALIDATED_CURRENT = {
	status: 0,
	stdout: "Authority already in the current format; nothing to upgrade.",
	stderr: ""
};
const REFUSED_NEWER = {
	status: 1,
	stdout: "",
	stderr: "authority upgrade: unsupported schema 42 for the current binary"
};

/*
================
fixtureAuthority

A stand-in authority directory: the store's database file plus one
sibling, so a copy-aside is observable file by file.
================
*/
function fixtureAuthority( t ) {
	const root = mkdtempSync( path.join( os.tmpdir(), "sro-local-run-" ) );
	t.after( () => rmSync( root, { recursive: true, force: true } ) );
	const dir = path.join( root, "authority" );
	mkdirSync( dir, { recursive: true } );
	writeFileSync( path.join( dir, "state.db" ), "fixture store" );
	writeFileSync( path.join( dir, "notes.txt" ), "fixture" );
	return { dir, backupsDir: path.join( root, "backups" ) };
}

/*
================
recordingRun

The upgrade tool as the runner drives it: scripted verdicts, recorded
calls.
================
*/
function recordingRun( verdicts ) {
	const calls = [];
	const run = ( tool, args ) => {
		calls.push( args );
		return verdicts[calls.length - 1];
	};
	return { calls, run };
}

/*
================
announcedBackup

The backup path out of the runner's own output, as a reader of the
done-when ("a backup named in the output") sees it.
================
*/
function announcedBackup( lines ) {
	const line = lines.find( candidate => candidate.includes( "copied it to " ) );
	assert.ok( line, `no line names the backup: ${lines.join( " | " )}` );
	return line.split( "copied it to " )[1].split( " and upgrading" )[0];
}

/*
================
TestAuthorityDirResolution
================
*/
test("the local authority directory resolves like the server's", t => {
	const saved = process.env.SRO_AUTHORITY_STATE_DIR;
	t.after( () => {
		if ( saved === undefined ) delete process.env.SRO_AUTHORITY_STATE_DIR;
		else process.env.SRO_AUTHORITY_STATE_DIR = saved;
	} );
	const settings = { env: new Map() };

	delete process.env.SRO_AUTHORITY_STATE_DIR;
	assert.equal(
		authorityDirFor( settings, "global-official" ),
		path.join( REPO_ROOT, "apps", "server", ".state", "shards", "global-official", "authority" )
	);

	process.env.SRO_AUTHORITY_STATE_DIR = path.join( os.tmpdir(), "from-process" );
	assert.equal( authorityDirFor( settings, "global-official" ), path.join( os.tmpdir(), "from-process" ) );

	settings.env.set( "SRO_AUTHORITY_STATE_DIR", path.join( os.tmpdir(), "from-settings" ) );
	assert.equal( authorityDirFor( settings, "global-official" ), path.join( os.tmpdir(), "from-settings" ) );
});

/*
================
TestAuthorityUpgradeCopiesAsideThenCommits
================
*/
test("a behind authority is backed up, named in the output, then upgraded", t => {
	const { dir, backupsDir } = fixtureAuthority( t );
	const { calls, run } = recordingRun( [
		VALIDATED_BEHIND,
		{ status: 0, stdout: "Authority upgraded to the current format and validated.", stderr: "" }
	] );
	const lines = [];
	const outcome = upgradeLocalAuthority( { dir, tool: "tool", backupsDir, run, log: line => lines.push( line ) } );

	assert.equal( outcome, "upgraded" );
	assert.deepEqual( calls.map( args => args.slice( 1 ) ), [
		[ dir ],
		[ dir, "-commit" ]
	] );
	const backup = announcedBackup( lines );
	assert.ok( existsSync( backup ), `expected the named backup to exist, lines: ${lines.join( " | " )}` );
	assert.equal( readFileSync( path.join( backup, "state.db" ), "utf8" ), "fixture store" );
	assert.equal( readFileSync( path.join( backup, "notes.txt" ), "utf8" ), "fixture" );
	assert.ok( lines.some( line => line.includes( `the pre-upgrade copy stays at ${backup}` ) ), lines.join( " | " ) );
});

/*
================
TestAuthorityUpgradeLeavesCurrentAlone
================
*/
test("a current authority is validated and left alone", t => {
	const { dir, backupsDir } = fixtureAuthority( t );
	const { calls, run } = recordingRun( [ VALIDATED_CURRENT ] );
	const outcome = upgradeLocalAuthority( { dir, tool: "tool", backupsDir, run, log: () => {} } );

	assert.equal( outcome, "current" );
	assert.equal( calls.length, 1 );
	assert.ok( !existsSync( backupsDir ) );
});

/*
================
TestAuthorityUpgradeNeverTouchesRefused
================
*/
test("a schema the upgrader refuses (newer than the binary) is never touched", t => {
	const { dir, backupsDir } = fixtureAuthority( t );
	const { calls, run } = recordingRun( [ REFUSED_NEWER ] );
	assert.throws(
		() => upgradeLocalAuthority( { dir, tool: "tool", backupsDir, run, log: () => {} } ),
		/unsupported schema 42/
	);
	assert.equal( calls.length, 1 );
	assert.ok( !existsSync( backupsDir ) );
});

/*
================
TestAuthorityUpgradeCommitFailureKeepsBackup
================
*/
test("a failed commit stops the run and names the surviving backup", t => {
	const { dir, backupsDir } = fixtureAuthority( t );
	const { run } = recordingRun( [
		VALIDATED_BEHIND,
		{ status: 1, stdout: "", stderr: "authority upgrade: store is locked" }
	] );
	let failure = null;
	try {
		upgradeLocalAuthority( { dir, tool: "tool", backupsDir, run, log: () => {} } );
	} catch ( error ) {
		failure = error;
	}
	assert.ok( failure, "expected the failed commit to stop the run" );
	assert.ok( failure.message.includes( "store is locked" ), failure.message );
	const [name] = readdirSync( backupsDir );
	const backup = path.join( backupsDir, name );
	assert.ok( failure.message.includes( `the pre-upgrade copy stays at ${backup}` ), failure.message );
	assert.equal( readFileSync( path.join( backup, "state.db" ), "utf8" ), "fixture store" );
});

/*
================
TestAuthorityUpgradeSkipsAbsent
================
*/
test("no authority yet: nothing to validate, nothing built", t => {
	const { calls, run } = recordingRun( [] );
	const outcome = upgradeLocalAuthority( {
		dir: path.join( os.tmpdir(), "sro-local-run-no-such-authority" ),
		tool: "tool",
		backupsDir: path.join( os.tmpdir(), "sro-local-run-no-such-backups" ),
		run,
		log: () => {}
	} );
	assert.equal( outcome, "absent" );
	assert.equal( calls.length, 0 );
});

/*
================
TestAuthorityCurrentPhrase
================
*/
test("the current verdict is the upgrader's own sentence", () => {
	assert.ok( authorityIsCurrent( VALIDATED_CURRENT.stdout ) );
	assert.ok( !authorityIsCurrent( VALIDATED_BEHIND.stdout ) );
});

/*
================
fixturePublicRoot

A minimal built client tree: the loose files exist; the published
manifest says what the packs hold. A null list publishes no manifest.
================
*/
function fixturePublicRoot( t, publishedPaths ) {
	const root = mkdtempSync( path.join( os.tmpdir(), "sro-local-public-" ) );
	t.after( () => rmSync( root, { recursive: true, force: true } ) );
	const publicRoot = path.join( root, "client-public" );
	// The sweep roots the pack collector stats even when empty (the real
	// tree always has them; runtimeTextAssets.test.mjs creates the same).
	for ( const directory of [ "char/vat", "npc/vat", "anim", "audio", "textdata", "config" ] ) {
		mkdirSync( path.join( publicRoot, "assets", directory ), { recursive: true } );
	}
	for (
		const relative of [
			"assets/data/event-interface.json",
			"assets/data/skillData.json",
			"assets/models/m.glb"
		]
	) {
		mkdirSync( path.dirname( path.join( publicRoot, relative ) ), { recursive: true } );
		writeFileSync( path.join( publicRoot, relative ), "{}" );
	}
	if ( publishedPaths !== null ) {
		mkdirSync( path.join( publicRoot, "assets", "packs" ), { recursive: true } );
		writeFileSync(
			path.join( publicRoot, "assets", "packs", "manifest.json" ),
			JSON.stringify( { assets: publishedPaths.map( assetPath => ({ path: assetPath }) ) } )
		);
	}
	return publicRoot;
}

/*
================
TestDoctorNamesMissingStartupPath
================
*/
test("a manifest lacking a HUD startup path names it and the publish command", async t => {
	const publicRoot = fixturePublicRoot( t, [ "/assets/data/skillData.json", "/assets/models/m.glb" ] );
	const problem = await unpublishedStartupAssets( publicRoot );
	assert.ok( problem !== null, "expected the stale manifest to be named a problem" );
	assert.ok( problem.includes( "/assets/data/event-interface.json" ), problem );
	assert.ok( problem.includes( "pnpm assets build full" ), problem );
	assert.ok( !problem.includes( "m.glb" ), `lazy assets are not the doctor's business: ${problem}` );
});

/*
================
TestDoctorAcceptsPublishedStartupSet
================
*/
test("a manifest holding the startup set (one as its .json.gz pack entry) passes", async t => {
	const publicRoot = fixturePublicRoot( t, [
		"/assets/data/event-interface.json.gz",
		"/assets/data/skillData.json",
		"/assets/models/m.glb"
	] );
	assert.equal( await unpublishedStartupAssets( publicRoot ), null );
});

/*
================
TestDoctorNamesMissingManifest
================
*/
test("no published manifest yet names the publish command", async t => {
	const publicRoot = fixturePublicRoot( t, null );
	const problem = await unpublishedStartupAssets( publicRoot );
	assert.ok( problem !== null && problem.includes( "pnpm assets build full" ), problem ?? "no problem" );
});

/*
================
TestStopAnnouncement
================
*/
test("a stdin close announces itself; a console stop does not", () => {
	assert.equal(
		stopAnnouncement( "stdin" ),
		"stdin closed: stopping (start with a terminal, or keep stdin open)"
	);
	assert.equal( stopAnnouncement( "console" ), "stopping..." );
});
