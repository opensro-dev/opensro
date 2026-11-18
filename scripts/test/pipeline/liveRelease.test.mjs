/*
===========================================================================

liveRelease.test.mjs - tests for scripts/github/liveRelease.mjs

The pure decisions over the public production journal, and run() itself
against a fake GitHub client that keeps releases, so each run's effect on
the repository's Latest release is what is asserted.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import {
	decideLiveRelease,
	PRODUCTION_FORMAT,
	readLivePair,
	releaseNotes,
	run,
	tagName
} from "../../github/liveRelease.mjs";

// r202: b4b8e07d2, live since 2026-11-18 07:42:26 UTC.
const LIVE = "b4b8e07d25ce2080f21c4d0e1ec8fdecc54433a9";
const OLDER = "9ae50809c" + "0".repeat( 31 );
const DEPLOYED = 1794987746.98;

function journal( changes = {} ) {
	return {
		format: PRODUCTION_FORMAT,
		operation: null,
		server: { commit: LIVE, release: LIVE, deployedAt: DEPLOYED },
		client: { commit: LIVE, release: "1a145d95", deployedAt: DEPLOYED },
		...changes
	};
}

/*
================
fakeGithub

Releases by tag; git refs derived from them. getLatestRelease answers
the release flagged latest, or 404.
================
*/
function fakeGithub( releases ) {
	let nextId = releases.length + 1;
	const calls = [];
	const notFound = () => Object.assign( new Error( "Not Found" ), { status: 404 } );
	const rest = {
		git: {
			listMatchingRefs: async ( { ref } ) => ({
				data: releases.filter( ( release ) => ("tags/" + release.tag_name).startsWith( ref ) )
					.map( ( release ) => ({ ref: "refs/tags/" + release.tag_name }) )
			})
		},
		repos: {
			getLatestRelease: async () => {
				const latest = releases.find( ( release ) => release.latest );
				if ( !latest ) {
					throw notFound();
				}
				return { data: latest };
			},
			getReleaseByTag: async ( { tag } ) => ({ data: releases.find( ( release ) => release.tag_name === tag ) }),
			createRelease: async ( fields ) => {
				calls.push( [ "create", fields ] );
				releases.forEach( ( release ) => release.latest = false );
				releases.push( {
					id: nextId++,
					tag_name: fields.tag_name,
					target: fields.target_commitish,
					latest: true
				} );
			},
			updateRelease: async ( fields ) => {
				calls.push( [ "update", fields ] );
				releases.forEach( ( release ) => release.latest = release.id === fields.release_id );
			}
		}
	};
	const github = { rest, paginate: async ( method, options ) => (await method( options )).data };
	return { github, calls, releases };
}

async function runWith( production, releases ) {
	const fake = fakeGithub( releases );
	const lines = [];
	const decision = await run( {
		github: fake.github,
		context: { repo: { owner: "opensro-dev", repo: "opensro" } },
		core: { info: ( line ) => lines.push( line ) },
		fetchJson: async () => production
	} );
	return { decision, ...fake, lines };
}

test("the tag name is the deploy's UTC date and the commit's first nine characters", () => {
	assert.equal( tagName( LIVE, DEPLOYED ), "release-2026-11-18-b4b8e07d2" );
});

test("a journal in motion or out of shape decides nothing", () => {
	assert.match( readLivePair( journal( { operation: { phase: "verifying" } } ) ).skip, /operation is open/ );
	assert.match( readLivePair( journal( { format: "other" } ) ).skip, /unknown format/ );
	assert.match(
		readLivePair( journal( { server: { commit: "main", deployedAt: DEPLOYED } } ) ).skip,
		/no live commit/
	);
	assert.match( readLivePair( journal( { server: { commit: LIVE } } ) ).skip, /no live commit/ );
	assert.equal( decideLiveRelease( readLivePair( null ), [], null ).action, "none" );
});

test("a live commit without a release gets one, marked Latest", async () => {
	const { decision, calls, releases } = await runWith( journal(), [
		{ id: 1, tag_name: "revtool-seed-2026-11-13", latest: true }
	] );
	assert.equal( decision.action, "create" );
	const [kind, fields] = calls[0];
	assert.equal( kind, "create" );
	assert.equal( fields.tag_name, "release-2026-11-18-b4b8e07d2" );
	assert.equal( fields.target_commitish, LIVE );
	assert.equal( fields.make_latest, "true" );
	assert.equal( fields.name, "Live: 2026-11-18 (b4b8e07d2)" );
	assert.match( fields.body, /since \*\*2026-11-18 07:42 UTC\*\*/ );
	assert.equal( releases.find( ( release ) => release.latest ).target, LIVE );
});

test("the publisher's own tag of the live commit is the same release", async () => {
	// Confirmed a day after its deploy: another date, the same commit.
	const { decision, calls } = await runWith( journal(), [
		{ id: 1, tag_name: "release-2026-11-19-b4b8e07d2", latest: true }
	] );
	assert.equal( decision.action, "none" );
	assert.deepEqual( calls, [] );
});

test("after a revert to an earlier release, that release is marked Latest again", async () => {
	const older = journal( {
		server: { commit: OLDER, deployedAt: DEPLOYED - 86400 },
		client: { commit: OLDER, deployedAt: DEPLOYED - 86400 }
	} );
	const { decision, calls, releases } = await runWith( older, [
		{ id: 1, tag_name: "release-2026-11-17-9ae50809c", latest: false },
		{ id: 2, tag_name: "release-2026-11-18-b4b8e07d2", latest: true }
	] );
	assert.equal( decision.action, "mark-latest" );
	assert.deepEqual( calls, [ [ "update", {
		owner: "opensro-dev",
		repo: "opensro",
		release_id: 1,
		make_latest: "true"
	} ] ] );
	assert.equal( releases.find( ( release ) => release.latest ).tag_name, "release-2026-11-17-9ae50809c" );
});

test("an open operation writes nothing", async () => {
	const { decision, calls, lines } = await runWith( journal( { operation: { phase: "verifying" } } ), [] );
	assert.equal( decision.action, "none" );
	assert.deepEqual( calls, [] );
	assert.match( lines[0], /operation is open/ );
});

test("a pair from two commits names both in the notes", () => {
	const notes = releaseNotes( { server: LIVE, client: OLDER, deployedAt: DEPLOYED } );
	assert.match( notes, new RegExp( "Game commit: `" + LIVE ) );
	assert.match( notes, new RegExp( "Browser client commit: `" + OLDER ) );
});
