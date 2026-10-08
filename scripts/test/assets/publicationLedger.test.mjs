/*
===========================================================================

publicationLedger.test.mjs - owners claim, replace, merge and audit

Runs the ledger against an isolated generated root: a complete run
replaces its owner's record, a partial run merges, sidecars follow their
base, and the audit reports exactly the swept files nobody claimed.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

const root = await mkdtemp( path.join( os.tmpdir(), "sro-ledger-" ) );
process.env.SRO_GENERATED_ROOT = root;
const ledger = await import( "../../build/shared/publicationLedger.mjs" );
const assets = path.join( root, "client-public", "assets" );
const archiveRoot = path.join( root, "archives" );

/*
================
publicFile
================
*/
async function publicFile( publicPath, bytes = "x" ) {
	const file = path.join( root, "client-public", publicPath.slice( 1 ) );
	await mkdir( path.dirname( file ), { recursive: true } );
	await writeFile( file, bytes );
	return file;
}

test.after( () => rm( root, { recursive: true, force: true } ) );

test("only files under client-public/assets outside the pack tail are public", () => {
	assert.equal( ledger.toPublicPath( path.join( assets, "images", "a.png" ) ), "/assets/images/a.png" );
	assert.equal( ledger.toPublicPath( path.join( assets, "packs", "x.bin" ) ), null );
	assert.equal( ledger.toPublicPath( path.join( assets, "manifest.json" ) ), null );
	assert.equal( ledger.toPublicPath( path.join( root, "intermediate", "a.png" ) ), null );
});

test("claims outside an open publication are ignored", async () => {
	ledger.claimPublicFile( path.join( assets, "stray.png" ) );
	assert.equal( (await ledger.readClaims()).claimed.has( "/assets/stray.png" ), false );
});

test("a complete run replaces its record and a partial run merges", async () => {
	ledger.beginPublication( "outdoor-world" );
	ledger.claimPublicPaths( [ "/assets/world/a.json", "/assets/world/b.json" ] );
	await ledger.commitPublication();
	ledger.beginPublication( "outdoor-world", { complete: false } );
	ledger.claimPublicPaths( [ "/assets/world/c.json" ] );
	await ledger.commitPublication();
	let { owners } = await ledger.readClaims();
	assert.deepEqual( [ ...owners.get( "outdoor-world" ).keys() ].sort(), [
		"/assets/world/a.json",
		"/assets/world/b.json",
		"/assets/world/c.json"
	] );
	ledger.beginPublication( "outdoor-world" );
	ledger.claimPublicPaths( [ "/assets/world/a.json" ] );
	await ledger.commitPublication();
	({ owners } = await ledger.readClaims());
	assert.deepEqual( [ ...owners.get( "outdoor-world" ).keys() ], [ "/assets/world/a.json" ] );
});

test("a precompressed sidecar follows its base, and a base its packed .gz member", () => {
	const claimed = new Set( [ "/assets/data/a.json", "/assets/data/b.json.gz" ] );
	assert.ok( ledger.isClaimed( "/assets/data/a.json.gz", claimed ) );
	assert.ok( ledger.isClaimed( "/assets/data/A.json.br", claimed ) );
	assert.ok( ledger.isClaimed( "/assets/data/b.json", claimed ) );
	assert.ok( !ledger.isClaimed( "/assets/data/c.png", claimed ) );
});

test("an incomplete ledger reports unclaimed files and moves nothing", async () => {
	const stale = await publicFile( "/assets/images/stale.png", "1234" );
	await publicFile( "/assets/images/live.png" );
	ledger.beginPublication( "resource-build" );
	ledger.claimPublicPaths( [ "/assets/images/live.png" ] );
	await ledger.commitPublication();
	const groups = [ { name: "game-images", files: [ "/assets/images/live.png", "/assets/images/stale.png" ] } ];
	const report = await ledger.auditClaims( groups, { archiveRoot } );
	assert.deepEqual( report.unclaimed, [ { path: "/assets/images/stale.png", group: "game-images", bytes: 4 } ] );
	assert.equal( report.archived, false );
	assert.ok( report.missingOwners.includes( "family-quickslot" ) );
	assert.deepEqual( report.groups, groups );
	await stat( stale );
	const written = JSON.parse( await readFile( path.join( root, "unclaimed-assets.json" ), "utf8" ) );
	assert.equal( written.files, 1 );
});

test("a complete ledger soft-archives unclaimed files and retired owners' records", async () => {
	const stale = await publicFile( "/assets/images/stale.png", "1234" );
	await publicFile( "/assets/images/live.png" );
	const staleBase = await publicFile( "/assets/data/stale.json", "{}" );
	const staleBrotli = await publicFile( "/assets/data/stale.json.br" );
	await publicFile( "/assets/data/stale.json.gz" );
	for ( const owner of ledger.expectedOwners() ) {
		ledger.beginPublication( owner );
		if ( owner === "resource-build" ) ledger.claimPublicPaths( [ "/assets/images/live.png" ] );
		await ledger.commitPublication();
	}
	ledger.beginPublication( "family-renamed-away" );
	ledger.claimPublicPaths( [ "/assets/images/stale.png" ] );
	await ledger.commitPublication();
	const groups = [
		{ name: "game-images", files: [ "/assets/images/live.png", "/assets/images/stale.png" ] },
		{ name: "game-data", files: [ "/assets/data/stale.json.gz" ] }
	];
	const report = await ledger.auditClaims( groups, { archiveRoot } );
	assert.equal( report.archived, true );
	// The stale member's base and sibling sidecars go with it, or the next build regenerates it.
	await assert.rejects( stat( staleBase ), { code: "ENOENT" } );
	await assert.rejects( stat( staleBrotli ), { code: "ENOENT" } );
	assert.deepEqual( report.retiredOwners, [ "family-renamed-away" ] );
	assert.deepEqual( report.groups, [
		{ name: "game-images", files: [ "/assets/images/live.png" ] },
		{ name: "game-data", files: [] }
	] );
	await assert.rejects( stat( stale ), { code: "ENOENT" } );
	assert.equal( (await ledger.readClaims()).owners.has( "family-renamed-away" ), false );
	const index = { assets: [ { path: "/assets/images/live.png" }, { path: "/assets/images/gone.png" } ] };
	assert.match(
		(await ledger.verifyIndexClaims( index )).join( "" ),
		/1 packed asset\(s\) claimed by no build owner/
	);
	assert.deepEqual( await ledger.verifyIndexClaims( { assets: [ { path: "/assets/images/live.png" } ] } ), [] );
});

test("a file a claimed manifest names but no step produced is local-only, not garbage", async () => {
	await publicFile( "/assets/npc/Manifest.json", JSON.stringify( { models: { a: "/assets/npc/catalog.json" } } ) );
	await publicFile( "/assets/npc/catalog.json", JSON.stringify( { glb: "/assets/npc/Model.glb" } ) );
	await publicFile( "/assets/npc/Model.glb" );
	await publicFile( "/assets/npc/orphan.glb" );
	for ( const owner of ledger.expectedOwners() ) {
		ledger.beginPublication( owner );
		if ( owner === "resource-build" ) {
			ledger.claimPublicPaths( [ "/assets/npc/Manifest.json", "/assets/npc/catalog.json" ] );
		}
		await ledger.commitPublication();
	}
	const index = {
		assets: [ "/assets/npc/catalog.json", "/assets/npc/Model.glb", "/assets/npc/orphan.glb" ].map( path => ({
			path,
			length: 1
		}) )
	};
	const report = await ledger.indexClaimReport( index );
	assert.deepEqual( report.localOnly.map( row => row.path ), [ "/assets/npc/Model.glb" ] );
	assert.deepEqual( report.unclaimed.map( row => row.path ), [ "/assets/npc/orphan.glb" ] );
	assert.match( (await ledger.verifyIndexClaims( index )).join( "; " ), /produced by no current build step/ );
	const groups = [ { name: "game-models", files: [ "/assets/npc/Model.glb", "/assets/npc/orphan.glb" ] } ];
	await assert.rejects( ledger.auditClaims( groups, { archiveRoot } ), /a fresh clone would not have them/ );
	await stat( path.join( assets, "npc", "orphan.glb" ) );
});

test("a second open publication is refused", () => {
	ledger.beginPublication( "family-a" );
	assert.throws( () => ledger.beginPublication( "family-b" ), /still open/ );
	ledger.abandonPublication();
});
