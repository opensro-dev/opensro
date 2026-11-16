/*
===========================================================================

perf-crowd-held.test.mjs - the held crowd's layout and resumable accounts

The two places a reader would not see a fault: a layout whose last row or
outer column puts a walker's reach past the observer's interest area, and
a provisioning pass that, interrupted between the server's answer and
its own save, creates an account or a character twice on the next run.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const { crowdLayout } = await import( "../../tools/perf/core/crowd.mjs" );
const { newAccountsFile, provisionAccounts } = await import( "../../tools/perf/core/crowd-accounts.mjs" );

// Jangan's square (region 25000) and the Europe field near its east edge.
const JANGAN = Object.freeze( { id: "jangan", movementMode: 3, start: { regionId: 25000, x: 1404, y: -6, z: 1374 } } );
const EAST_EDGE = Object.freeze( { id: "east", movementMode: 3, start: { regionId: 0x6e4a, x: 1756, y: 7, z: 525 } } );
// Server world/interest.go block edge and crowd-activity.mjs WALK_RADIUS.
const BLOCK = 320, WALK = 25;

/*
================
fakeAgent

An Agent that keeps its accounts and characters and answers 409 for one
it already has, as provisioning.go and character create do.
================
*/
function fakeAgent( server ) {
	const conflict = () => Object.assign( Error( "conflict" ), { status: 409 } );
	return {
		createAccount: async ( id, password ) => {
			if ( server.accounts.has( id ) ) throw conflict();
			server.accounts.set( id, password );
			server.creates.push( id );
		},
		verifyAccount: async ( id, password ) => server.accounts.get( id ) === password,
		login: async ( id, password ) => {
			assert.equal( server.accounts.get( id ), password );
			server.logins.push( id );
			return { token: `${id}-${server.logins.length}` };
		},
		createCharacter: async ( session, name ) => {
			if ( server.characters.has( name ) ) throw conflict();
			server.characters.add( name );
		},
		listCharacters: async () => [ ...server.characters ]
	};
}

test("every layout keeps each spot and a walker's reach inside the observer's interest area", () => {
	for ( const count of [ 1, 6, 32, 72, 73, 200, 600, 720 ] ) {
		const spots = crowdLayout( JANGAN, count ), start = JANGAN.start;
		assert.equal( spots.length, count );
		assert.equal( new Set( spots.map( spot => `${spot.x},${spot.z}` ) ).size, count, `${count}: spots overlap` );
		for ( const spot of spots ) {
			assert.ok( Math.abs( spot.x - start.x ) + WALK < BLOCK, `${count}: x ${spot.x}` );
			assert.ok( spot.z - start.z + WALK < BLOCK && spot.z - start.z - WALK > 0, `${count}: z ${spot.z}` );
			assert.equal( spot.regionId, start.regionId );
		}
	}
	// A small crowd keeps the four-wide block it always had.
	assert.equal( new Set( crowdLayout( JANGAN, 32 ).map( spot => spot.x ) ).size, 4 );
	assert.throws( () => crowdLayout( JANGAN, 721 ), /does not fit/ );
	assert.doesNotThrow( () => crowdLayout( EAST_EDGE, 32 ) );
	assert.throws( () => crowdLayout( EAST_EDGE, 600 ), /leaves region/ );
});

test("an interrupted provisioning pass resumes without creating an account or character twice", async () => {
	const server = { accounts: new Map(), characters: new Set(), creates: [], logins: [] };
	// The file on disk; a save matching crash() dies before it writes, as a
	// process killed between the server's answer and its own save.
	let disk = /** @type {any} */ (null), crash = ( /** @type {any} */ _file ) => false;
	const save = async file => {
		if ( crash( file ) ) {
			crash = _file => false;
			throw Error( "interrupted" );
		}
		disk = structuredClone( file );
	};
	const quiet = () => {};
	const file = newAccountsFile( JANGAN );
	const ids = () => disk.accounts.map( account => account.id );
	const pass = ( count, from = structuredClone( disk ) ) =>
		provisionAccounts( { file: from, count, agent: fakeAgent( server ), save, log: quiet } );
	crash = file => file.accounts[2].provisioned;
	await assert.rejects( pass( 6, file ), /interrupted/ );
	const planned = ids();
	crash = file => file.accounts[4].created;
	await assert.rejects( pass( 6 ), /interrupted/ );
	const done = await pass( 6 );
	assert.deepEqual( ids(), planned );
	assert.deepEqual( server.creates, planned );
	assert.equal( server.characters.size, 6 );
	assert.ok( disk.accounts.every( account => account.provisioned && account.created && account.session ) );
	// One login per account: the resumed pass reused account 4's saved session.
	assert.deepEqual( [ ...server.logins ].sort(), planned );
	assert.equal( done.created, 2 );
	// Growing the file plans only the new indices.
	await pass( 8 );
	assert.deepEqual( ids().slice( 0, 6 ), planned );
	assert.equal( server.creates.length, 8 );
	// An existing account whose password is not the file's is never taken over.
	const stranger = structuredClone( disk );
	stranger.accounts.length = 8;
	server.accounts.set( `held${file.prefix}008`, "someone-else" );
	await assert.rejects( pass( 9, stranger ), /another password/ );
});
