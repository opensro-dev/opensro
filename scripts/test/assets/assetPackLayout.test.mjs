/*
===========================================================================

assetPackLayout.test.mjs - a content change ships only the packs it touches

The 2026-10-05 data release added 43 guild models (17 MiB): path-ordered
chunking moved 569 unchanged models into new packs and re-shipped 366 MiB.
These cases pin the stable layout: an insert, a change and a removal each
rebuild only the packs they touch, kept packs keep their slot and folder
(their URL), and drift past the slack repacks the group whole. The last
case builds real packs in a temp tree and compares bytes and URLs.

===========================================================================
*/

import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { buildAssetPacks } from "../../build/assetPacks.mjs";
import { baselinePacksOf, MAX_PACK_SLACK, packSlotOf, planPackLayout } from "../../build/assetPackLayout.mjs";

const TARGET = 100;
const OWN = "/assets/packs";

/*
================
file

One layout input: a path, a content tag standing in for its sha256, a size.
================
*/
function file( name, bytes = 30, tag = "v1" ) {
	return { publicPath: `/assets/${name}`, sha256: `${name}:${tag}`, bytes, mime: "application/octet-stream" };
}

/*
================
asBaseline

A plan as the index a build would have published, read back as a baseline.
================
*/
function asBaseline( plan, dir = OWN ) {
	return {
		targetBytes: TARGET,
		dir: OWN,
		packs: plan.map( ( pack ) => ({
			slot: pack.slot,
			dir: pack.dir ?? dir,
			sha256: `pack-${pack.slot}`,
			members: pack.files.map( ( f ) => ({
				path: f.publicPath,
				sha256: f.sha256,
				length: f.bytes,
				mime: f.mime
			}) )
		}) )
	};
}

const sorted = ( files ) => [ ...files ].sort( ( a, b ) => a.publicPath.localeCompare( b.publicPath ) );
const names = ( pack ) => pack.files.map( ( f ) => f.publicPath );

test("without a baseline the layout is path-ordered chunks in slots 1..n", () => {
	const files = sorted( [ "b", "c", "d", "e", "f", "g" ].map( ( n ) => file( n ) ) );
	const plan = planPackLayout( { files, baseline: null, targetBytes: TARGET } );
	assert.deepEqual( plan.map( ( p ) => p.slot ), [ 1, 2 ] );
	assert.deepEqual( plan.map( names ), [ [ "/assets/b", "/assets/c", "/assets/d" ], [
		"/assets/e",
		"/assets/f",
		"/assets/g"
	] ] );
	assert.ok( plan.every( ( p ) => !p.kept ) );
});

test("an insert ahead of every pack keeps all of them and adds one", () => {
	const before = sorted( [ "b", "c", "d", "e", "f", "g" ].map( ( n ) => file( n ) ) );
	const baseline = asBaseline( planPackLayout( { files: before, baseline: null, targetBytes: TARGET } ) );
	const after = sorted( [ ...before, file( "a" ) ] );
	const plan = planPackLayout( { files: after, baseline, targetBytes: TARGET } );
	assert.deepEqual( plan.filter( ( p ) => p.kept ).map( ( p ) => p.slot ), [ 1, 2 ] );
	const fresh = plan.filter( ( p ) => !p.kept );
	assert.deepEqual( fresh.map( ( p ) => [ p.slot, names( p ) ] ), [ [ 3, [ "/assets/a" ] ] ] );
});

test("a changed member rebuilds only its pack", () => {
	const before = sorted( [ "b", "c", "d", "e", "f", "g" ].map( ( n ) => file( n ) ) );
	const baseline = asBaseline( planPackLayout( { files: before, baseline: null, targetBytes: TARGET } ) );
	const after = before.map( ( f ) => f.publicPath === "/assets/f" ? file( "f", 30, "v2" ) : f );
	const plan = planPackLayout( { files: after, baseline, targetBytes: TARGET } );
	assert.deepEqual( plan.filter( ( p ) => p.kept ).map( ( p ) => p.slot ), [ 1 ] );
	const fresh = plan.filter( ( p ) => !p.kept );
	assert.deepEqual( fresh.map( ( p ) => [ p.slot, names( p ) ] ), [ [ 2, [
		"/assets/e",
		"/assets/f",
		"/assets/g"
	] ] ] );
	assert.equal( fresh[0].files[1].sha256, "f:v2" );
});

test("a removed member rebuilds only its pack, from the survivors", () => {
	const before = sorted( [ "b", "c", "d", "e", "f", "g" ].map( ( n ) => file( n ) ) );
	const baseline = asBaseline( planPackLayout( { files: before, baseline: null, targetBytes: TARGET } ) );
	const after = before.filter( ( f ) => f.publicPath !== "/assets/c" );
	const plan = planPackLayout( { files: after, baseline, targetBytes: TARGET } );
	assert.deepEqual( plan.filter( ( p ) => p.kept ).map( ( p ) => p.slot ), [ 2 ] );
	assert.deepEqual( plan.filter( ( p ) => !p.kept ).map( names ), [ [ "/assets/b", "/assets/d" ] ] );
});

test("drift past the slack, or a new target size, repacks the group whole", () => {
	// Every file alone in its baseline pack: far more packs than the ideal.
	const files = sorted( Array.from( { length: 10 }, ( _, i ) => file( "n" + i, 10 ) ) );
	const scattered = {
		targetBytes: TARGET,
		dir: OWN,
		packs: files.map( ( f, i ) => ({
			slot: i + 1,
			dir: OWN,
			sha256: "p" + i,
			members: [ { path: f.publicPath, sha256: f.sha256, length: f.bytes, mime: f.mime } ]
		}) )
	};
	assert.ok( files.length > 1 + MAX_PACK_SLACK );
	const plan = planPackLayout( { files, baseline: scattered, targetBytes: TARGET } );
	assert.deepEqual( plan.map( ( p ) => p.slot ), [ 1 ] );
	assert.ok( plan.every( ( p ) => !p.kept ) );
	const resized = planPackLayout( { files, baseline: { ...scattered, targetBytes: 50 }, targetBytes: TARGET } );
	assert.ok( resized.every( ( p ) => !p.kept ) );
});

test("a kept pack from a partial builder keeps its folder; fresh slots ignore it", () => {
	const files = sorted( [ file( "a" ), file( "b" ), file( "z" ) ] );
	const slotDir = "/assets/packs/incremental/x/slots/slot-1";
	const baseline = {
		targetBytes: TARGET,
		dir: OWN,
		packs: [ {
			slot: 1,
			dir: slotDir,
			sha256: "s",
			members: [ { path: "/assets/z", sha256: "z:v1", length: 30, mime: "application/octet-stream" } ]
		} ]
	};
	const plan = planPackLayout( { files, baseline, targetBytes: TARGET } );
	const kept = plan.find( ( p ) => p.kept );
	assert.equal( kept?.dir, slotDir );
	assert.deepEqual( plan.filter( ( p ) => !p.kept ).map( ( p ) => [ p.slot, p.dir ] ), [ [ 1, undefined ] ] );
});

test("slots are read from pack names", () => {
	assert.equal( packSlotOf( "/assets/packs/game-models-012-0123456789ab.bin" ), 12 );
	assert.equal( packSlotOf( "/assets/packs/manifest.json" ), null );
	const index = {
		groups: [ {
			name: "g",
			targetBytes: TARGET,
			packs: [ { path: "/assets/packs/g-002-0123456789ab.bin", sha256: "h" } ]
		} ],
		assets: [
			{
				path: "/assets/y",
				packPath: "/assets/packs/g-002-0123456789ab.bin",
				offset: 9,
				length: 1,
				sha256: "y",
				mime: "m"
			},
			{
				path: "/assets/x",
				packPath: "/assets/packs/g-002-0123456789ab.bin",
				offset: 0,
				length: 9,
				sha256: "x",
				mime: "m"
			}
		]
	};
	const baseline = baselinePacksOf( index, "g", OWN );
	assert.deepEqual( baseline?.packs.map( ( p ) => [ p.slot, p.dir, p.members.map( ( m ) => m.path ) ] ), [ [
		2,
		"/assets/packs",
		[ "/assets/x", "/assets/y" ]
	] ] );
	assert.equal( baselinePacksOf( index, "other", OWN ), null );
});

test("a real rebuild after an insert keeps every unchanged pack's bytes and URL", async ( t ) => {
	const tempRoot = await mkdtemp( path.join( os.tmpdir(), "sro-pack-layout-" ) );
	t.after( () => rm( tempRoot, { recursive: true, force: true } ) );
	const publicRoot = path.join( tempRoot, "public" );
	const write = async ( name, text ) => {
		const target = path.join( publicRoot, "assets", "models", name );
		await mkdir( path.dirname( target ), { recursive: true } );
		await writeFile( target, text );
		return `/assets/models/${name}`;
	};
	const paths = [];
	for ( const name of [ "b.glb", "c.glb", "d.glb", "e.glb", "f.glb", "g.glb" ] ) {
		paths.push( await write( name, name.repeat( 100 ) ) );
	}
	const options = ( files ) => ({
		publicRoot,
		outputRoot: path.join( publicRoot, "assets", "packs" ),
		hashCachePath: path.join( tempRoot, "hash-cache.json" ),
		targetBytes: 1500,
		groups: [ { name: "game-models", load: "lazy", files } ]
	});
	const first = await buildAssetPacks( options( paths ) );
	assert.equal( first.groups[0].packs.length, 2 );
	const inserted = await write( "a.glb", "a.glb".repeat( 100 ) );
	const second = await buildAssetPacks( options( [ inserted, ...paths ] ) );
	const before = first.groups[0].packs.map( ( p ) => [ p.path, p.sha256 ] );
	const after = new Map( second.groups[0].packs.map( ( p ) => [ p.path, p.sha256 ] ) );
	for ( const [url, sha] of before ) assert.equal( after.get( url ), sha, `${url} kept its bytes` );
	assert.equal( second.keptPackCount, 2 );
	assert.equal( second.freshPackCount, 1 );
	assert.equal( second.builtPackCount, 1, "only the new asset's pack is built" );
});
