/*
===========================================================================
ui-qa-probe-support.test.mjs - guard decisions, catalogue identity and outputs

No browser is needed to pin the request-routing policy and fresh fetch contract.
===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import path from "node:path";
import { UI_QA_ORIGIN, UI_QA_CATALOG_PATH } from "../../tools/ui-qa/settings.mjs";
import { artifactDirectory, guardReviewRequests, UI_QA_REPOSITORY_ROOT } from "../../tools/ui-qa/probe-support.mjs";
import { fetchCatalog, fetchCatalogIdentity } from "../../tools/ui-qa/catalog-client.mjs";

test("the context guard admits local assets and aborts other origins before sending", async () => {
	const decisions = [], blocked = [];
	const urls = [
		UI_QA_ORIGIN + "/assets/data/skillUi.json",
		"https://example.invalid/report",
		UI_QA_ORIGIN + "0/",
		"http://127.0.0.1:5190/",
		"http://127.0.0.1:5191.evil.invalid/"
	];
	await guardReviewRequests( {
		async route( pattern, handler ) {
			assert.equal( pattern, "**/*" );
			for ( const url of urls ) {
				await handler( {
					request: () => ({ url: () => url }),
					async continue() {
						decisions.push( [ url, "continue" ] );
					},
					async abort( reason ) {
						decisions.push( [ url, reason ] );
					}
				} );
			}
		}
	}, blocked );
	assert.deepEqual( decisions, urls.map( ( url, index ) => [ url, index ? "blockedbyclient" : "continue" ] ) );
	assert.deepEqual( blocked, urls.slice( 1 ) );
});

test("catalogue clients fetch a fresh checked identity and reject failed or malformed responses", async () => {
	const catalog = {
		identity: {
			codeSha: "head",
			sourceHash: "source",
			assetHash: "assets",
			defineHash: null,
			fontHash: null,
			fingerprint: "first",
			dirty: false
		},
		resources: [ {
			name: "ifstore",
			nativeHash: null,
			publishedHash: "published",
			url: "/assets/cif/layouts/ifstore.json",
			references: [ "runtime/ui/ui.ts" ],
			sections: [ {
				name: "Create",
				nodes: [ {
					id: 15,
					name: "GDR_STORE",
					type: "CIFStore",
					occurrence: 0,
					rect: { x: 0, y: 0, width: 254, height: 370 }
				} ]
			} ]
		} ],
		roots: {},
		limitations: "No parity claim"
	};
	const calls = [];
	/** @type {typeof fetch} */
	const request = async ( url, options ) => {
		calls.push( { url, options } );
		return new Response( JSON.stringify( catalog ) );
	};
	assert.deepEqual( await fetchCatalog( request ), catalog );
	catalog.identity.fingerprint = "second";
	assert.equal( (await fetchCatalogIdentity( request )).fingerprint, "second" );
	assert.deepEqual(
		calls,
		[ 0, 1 ].map( () => ({ url: UI_QA_ORIGIN + UI_QA_CATALOG_PATH, options: { cache: "no-store" } }) )
	);
	await assert.rejects( fetchCatalog( async () => new Response( "unavailable", { status: 503 } ) ), /503/ );
	await assert.rejects(
		fetchCatalogIdentity( async () => new Response( JSON.stringify( { identity: {} } ) ) ),
		/Invalid UI QA/
	);
	await assert.rejects( fetchCatalog( async () => new Response( "not json" ) ), SyntaxError );
	const invalid = {
		...catalog,
		resources: [ { ...catalog.resources[0], sections: [ { name: "Create", nodes: [ { rect: [] } ] } ] } ]
	};
	await assert.rejects( fetchCatalog( async () => new Response( JSON.stringify( invalid ) ) ), /Invalid UI QA/ );
});

test("artifact directories stay under the checkout root independent of cwd", () => {
	const previous = process.cwd();
	try {
		process.chdir( path.join( UI_QA_REPOSITORY_ROOT, "apps/client-next" ) );
		assert.equal(
			artifactDirectory( "registry" ),
			path.join( UI_QA_REPOSITORY_ROOT, "temp/artifacts/ui-qa/registry" )
		);
		assert.throws( () => artifactDirectory( "../../../outside" ), /leaves UI QA root/ );
	} finally {
		process.chdir( previous );
	}
});
