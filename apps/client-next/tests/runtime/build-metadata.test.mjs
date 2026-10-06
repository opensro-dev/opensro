/*
===========================================================================

build-metadata.test.mjs - execute the beta compiler's stamped application

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, readFile, rm } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { runInNewContext } from "node:vm";
import os from "node:os";
import path from "node:path";
import { buildApplication } from "../../tools/beta/build.mjs";
import { clientBuildDefinitions } from "../../tools/build-metadata.mjs";
import { files } from "../../tools/beta/policy.mjs";

test("beta compiler embeds the same revision and subject as development", async t => {
	const base = await mkdtemp( path.join( os.tmpdir(), "sro-build-stamp-" ) );
	t.after( () => rm( base, { recursive: true, force: true } ) );
	const source = {
		"index.html": '<script type="module" src="/src/main.ts"></script>',
		"src/main.ts":
			"globalThis.stampedBuild = {revision: import.meta.env.SRO_CLIENT_REVISION, subject: import.meta.env.SRO_CLIENT_SUBJECT};"
	};
	for ( const [name, text] of Object.entries( source ) ) {
		await mkdir( path.dirname( path.join( base, name ) ), { recursive: true } );
		await writeFile( path.join( base, name ), text );
	}
	execFileSync( "git", [ "init", "-q" ], { cwd: base } );
	execFileSync( "git", [ "add", "." ], { cwd: base } );
	const subject = 'Build "quotes" and beta metadata';
	execFileSync( "git", [
		"-c",
		"user.name=Fixture",
		"-c",
		"user.email=fixture@example.invalid",
		"commit",
		"-qm",
		subject
	], { cwd: base } );
	const revision = execFileSync( "git", [ "rev-parse", "HEAD" ], { cwd: base, encoding: "utf8" } ).trim();
	const directory = path.join( base, "release/package/application" );
	await buildApplication( { base, source, directory } );
	/** @type {{ document: any; stampedBuild: null | { revision: string; subject: string; }; }} */
	const context = { document: { createElement: () => ({ relList: { supports: () => true } }) }, stampedBuild: null };
	for ( const name of await files( directory ) ) {
		if ( name.endsWith( ".js" ) ) {
			runInNewContext( await readFile( path.join( directory, name ), "utf8" ), context );
		}
	}
	assert.ok( context.stampedBuild );
	assert.equal( context.stampedBuild.revision, revision );
	assert.equal( context.stampedBuild.subject, subject );
	const dev = clientBuildDefinitions( base );
	assert.equal( JSON.parse( dev["import.meta.env.SRO_CLIENT_REVISION"] ), revision );
	assert.equal( JSON.parse( dev["import.meta.env.SRO_CLIENT_SUBJECT"] ), subject );
	assert.equal(
		JSON.parse( clientBuildDefinitions( path.join( base, "missing" ) )["import.meta.env.SRO_CLIENT_REVISION"] ),
		""
	);
});
