/*
===========================================================================

persistent-storage.test.mjs - the client asks for persistence without prompting

Chromium and WebKit decide silently, so the request is made there and only
once it is not already granted; Firefox would prompt mid-play, so it is not
asked; a missing API or a rejected call reads as "not persistent".

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";

const { requestPersistentStorage } = await import( "../../src/engine/foundation/assets/persistent-storage.ts" );
const CHROME =
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36";
const FIREFOX = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:147.0) Gecko/20100101 Firefox/147.0";

/*
================
storageNavigator
================
*/
function storageNavigator( userAgent, { persisted = false, grant = true, fail = false } = {} ) {
	const calls = [];
	return {
		calls,
		userAgent,
		storage: {
			persisted: async () => {
				calls.push( "persisted" );
				if ( fail ) throw new Error( "storage disabled" );
				return persisted;
			},
			persist: async () => {
				calls.push( "persist" );
				return grant;
			}
		}
	};
}

test("Chromium asks once and reports the browser's answer", async () => {
	const nav = storageNavigator( CHROME );
	assert.equal( await requestPersistentStorage( nav ), true );
	assert.deepEqual( nav.calls, [ "persisted", "persist" ] );
});

test("storage that is already persistent is not asked again", async () => {
	const nav = storageNavigator( CHROME, { persisted: true } );
	assert.equal( await requestPersistentStorage( nav ), true );
	assert.deepEqual( nav.calls, [ "persisted" ] );
});

test("Firefox is never asked, because it would prompt the player", async () => {
	const nav = storageNavigator( FIREFOX );
	assert.equal( await requestPersistentStorage( nav ), false );
	assert.deepEqual( nav.calls, [] );
});

test("a missing API or a failing call reads as not persistent", async () => {
	assert.equal( await requestPersistentStorage( { userAgent: CHROME } ), false );
	assert.equal( await requestPersistentStorage( storageNavigator( CHROME, { fail: true } ) ), false );
});
