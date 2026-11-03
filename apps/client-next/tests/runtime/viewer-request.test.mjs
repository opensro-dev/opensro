/*
===========================================================================

viewer-request.test.mjs - the 3D viewer's query and failure reasons

viewer.html takes exactly one of ?monster= and ?look=, an optional still
flag and size, and reports only the fixed reasons (docs/guides/VIEWER.md).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { parseViewerRequest, viewerReason, viewerError, viewerReasons, VIEWER_STILL_SIZE } = await import(
	"../../src/engine/foundation/ui/viewer-request.ts"
);

/*
================
lookParam
================
*/
function lookParam( look ) {
	return Buffer.from( JSON.stringify( look ) ).toString( "base64url" );
}

test("a monster request names its id; still and size have their defaults", () => {
	assert.deepEqual( parseViewerRequest( "?monster=1933" ), {
		kind: "monster",
		refObjId: 1933,
		still: false,
		size: VIEWER_STILL_SIZE,
		batch: false
	} );
	assert.deepEqual( parseViewerRequest( "?monster=1933&still=1&size=256" ), {
		kind: "monster",
		refObjId: 1933,
		still: true,
		size: 256,
		batch: false
	} );
});

test("batch=1 keeps a monster still's page for the next monster, and nothing else", () => {
	assert.equal( parseViewerRequest( "?monster=1933&still=1&batch=1" ).batch, true );
	for (
		const query of [ "?monster=1933&batch=1", "?monster=1933&still=0&batch=1", "?monster=1933&still=1&batch=0" ]
	) {
		assert.throws( () => parseViewerRequest( query ), /bad-request/, query );
	}
	const look = lookParam( { bodyRefObjId: 1907, worn: [] } );
	assert.throws( () => parseViewerRequest( "?look=" + look + "&still=1&batch=1" ), /bad-request/ );
});

test("a look request decodes the public API's look object", () => {
	const look = {
		bodyRefObjId: 1907,
		worn: [ { slot: 6, refItemId: 3801, plus: 7 } ],
		avatar: [ { slot: 0, refItemId: 23450 } ]
	};
	assert.deepEqual( parseViewerRequest( "?look=" + lookParam( look ) ), {
		kind: "look",
		look,
		still: false,
		size: VIEWER_STILL_SIZE,
		batch: false
	} );
	// worn plus defaults to zero; empty lists are allowed.
	assert.deepEqual(
		/** @type {any} */ (parseViewerRequest(
			"?look=" + lookParam( { bodyRefObjId: 1907, worn: [ { slot: 1, refItemId: 9 } ] } )
		)).look,
		{
			bodyRefObjId: 1907,
			worn: [ { slot: 1, refItemId: 9, plus: 0 } ],
			avatar: []
		}
	);
});

test("anything malformed is a bad request, never a guess", () => {
	for (
		const search of [
			"",
			"?monster=1&look=" + lookParam( { bodyRefObjId: 1 } ),
			"?monster=0",
			"?monster=-5",
			"?monster=12abc",
			"?monster=99999999999",
			"?monster=1933&size=63",
			"?monster=1933&size=1025",
			"?monster=1933&size=abc",
			"?monster=1933&still=yes",
			"?look=not base64!",
			"?look=" + Buffer.from( "{not json" ).toString( "base64url" ),
			"?look=" + lookParam( { worn: [] } ),
			"?look=" + lookParam( { bodyRefObjId: 1907, worn: [ { slot: 13, refItemId: 1 } ] } ),
			"?look=" + lookParam( { bodyRefObjId: 1907, avatar: [ { slot: 4, refItemId: 1 } ] } ),
			"?look=" +
			lookParam( {
				bodyRefObjId: 1907,
				worn: Array.from( { length: 33 }, ( _, i ) => ({ slot: 0, refItemId: i + 1 }) )
			} )
		]
	) {
		assert.throws(
			() => parseViewerRequest( search ),
			error =>
				error instanceof Error && error.message === "bad-request" && /** @type {any} */
				(error).reason === "bad-request",
			search
		);
	}
});

test("failures report only the fixed reasons", () => {
	assert.deepEqual( [ ...viewerReasons() ], [
		"no-webgpu",
		"bad-request",
		"unknown-monster",
		"asset-load",
		"render",
		"timeout"
	] );
	for ( const reason of viewerReasons() ) assert.equal( viewerReason( viewerError( reason ) ), reason );
	assert.equal( viewerReason( new Error( "anything else" ) ), "render" );
	assert.equal( viewerReason( "a string" ), "render" );
});
