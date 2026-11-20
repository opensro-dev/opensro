/*
===========================================================================

ui-image-lane.test.mjs - a window's art is never queued behind streaming

The UI image owner (ui/resources/resources.ts) requests on the asset
owner's interface lane, which always holds a slot of its own (#3977).
Runs the real asset owner and the real UI image owner against a recorded
worker: the streams fill every slot they may hold, and the window's icon
must still be requested on the same frame.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createAssets } = await import( "../../src/engine/runtime/assets/assets.ts" );
const { createUiAssets } = await import( "../../src/engine/runtime/ui/resources/resources.ts" );

const ORIGIN = "https://fixture.invalid/";

/*
================
fixture

The production asset owner with the worker boundary recorded.
================
*/
function fixture( t ) {
	const original = Object.getOwnPropertyDescriptor( globalThis, "Worker" );
	const messages = [];
	class Worker {
		/*
		================
		postMessage
		================
		*/
		postMessage( message ) {
			messages.push( message );
		}
		/*
		================
		terminate
		================
		*/
		terminate() {}
	}
	Object.defineProperty( globalThis, "Worker", { configurable: true, value: Worker } );
	t.after( () => {
		if ( original ) Object.defineProperty( globalThis, "Worker", original );
		else delete globalThis.Worker;
	} );
	const assets = createAssets();
	t.after( () => assets.dispose() );
	return { assets, messages };
}

test("a window's icon is requested while the streams hold every slot they may", t => {
	const { assets, messages } = fixture( t );
	for ( let i = 0; i < 4; i++ ) assets.request( ORIGIN + "assets/world/model" + i + ".glb", 1024, "glb" );
	assert.equal( assets.available(), 0, "the streams hold their four slots" );
	const images = createUiAssets( assets, () => {}, ORIGIN, () => {} );
	t.after( () => images.dispose() );
	const icon = "/assets/images/Media_extracted/icon/item/a.png";
	images.step( [ icon ], 0 );
	const loads = messages.filter( message => message.kind === "load" );
	assert.equal( loads.length, 5 );
	assert.equal( new URL( loads[4].url ).pathname, icon );
	assert.equal( loads[4].lane, "interface" );
});
