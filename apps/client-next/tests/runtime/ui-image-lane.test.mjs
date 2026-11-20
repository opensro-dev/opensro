/*
===========================================================================

ui-image-lane.test.mjs - an open window's loads are never queued behind streaming

Every UI owner whose loads a player waits on in an open window - its art
(ui/resources/resources.ts), the M window's region and guide catalogues,
its hunting guide and unique spawns, the stall network's categories -
requests on the asset owner's interface lane, which always holds a slot of
its own (foundation/assets/asset-slots.ts, #3977). Runs the real asset
owner and the real UI owners against a recorded worker: the streams fill
every slot they may hold, and each window's load must still be requested
on the same step.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createAssets } = await import( "../../src/engine/runtime/assets/assets.ts" );
const { createUiAssets } = await import( "../../src/engine/runtime/ui/resources/resources.ts" );
const { createHuntingGuideHud } = await import( "../../src/engine/runtime/ui/hud/hunting-guide.ts" );
const { createMapToolsHud } = await import( "../../src/engine/runtime/ui/hud/map-tools.ts" );
const { createUniqueNestsHud } = await import( "../../src/engine/runtime/ui/hud/unique-nests.ts" );
const { createStallNetworkCategories } = await import(
	"../../src/engine/runtime/ui/hud/stall-network-categories.ts"
);

const ORIGIN = "https://fixture.invalid/";

/*
================
fixture

The production asset owner with the worker boundary recorded, and every
stream slot held by a world load.
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
		else Reflect.deleteProperty( globalThis, "Worker" );
	} );
	const assets = createAssets();
	t.after( () => assets.dispose() );
	for ( let i = 0; i < 4; i++ ) assets.request( ORIGIN + "assets/world/model" + i + ".glb", 1024, "glb" );
	assert.equal( assets.available(), 0, "the streams hold their four slots" );
	return { assets, loads: () => messages.filter( message => message.kind === "load" ) };
}

// Each owner, made over the asset owner, and the step that opens its window.
const OWNERS = [
	{
		name: "a window's art",
		open: assets => {
			const images = createUiAssets( assets, () => {}, ORIGIN, () => {} );
			images.step( [ "/assets/images/Media_extracted/icon/item/a.png" ], 0 );
			return images;
		}
	},
	{
		name: "the M window's region catalogue",
		open: assets => {
			const tools = createMapToolsHud( { assets, base: ORIGIN } );
			tools.step( "Map" );
			return tools;
		}
	},
	{
		name: "the M window's hunting guide",
		open: assets => {
			const guide = createHuntingGuideHud( assets, ORIGIN );
			guide.step( { url: ORIGIN + "assets/data/hunting-guide.json", bytes: 1024 }, true, false );
			return guide;
		}
	},
	{
		name: "the M window's unique spawns",
		open: assets => {
			const nests = createUniqueNestsHud( assets, ORIGIN );
			nests.step( true );
			return nests;
		}
	},
	{
		name: "the stall network's categories",
		open: assets => {
			const categories = createStallNetworkCategories( assets, ORIGIN );
			categories.step( true );
			return categories;
		}
	}
];

for ( const owner of OWNERS ) {
	test(`${owner.name} is requested while the streams hold every slot they may`, t => {
		const { assets, loads } = fixture( t );
		const made = owner.open( assets );
		t.after( () => made.dispose?.() );
		assert.equal( loads().length, 5, "the window's load took the interface lane's own slot" );
		assert.equal( loads()[4].lane, "interface" );
	});
}
