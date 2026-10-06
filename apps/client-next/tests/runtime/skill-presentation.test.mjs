/*
===========================================================================

skill-presentation.test.mjs - tests for localization.ts, icon.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
const { createLocalization } = await import( "../../src/engine/runtime/ui/localization/localization.ts" );
const { iconPath } = await import( "../../src/engine/foundation/ui/icon.ts" );
test("shipped skill name and icon resolve to published assets", () => {
	const names = JSON.parse( fs.readFileSync( CLIENT_PUBLIC_ROOT + "/assets/text/textdataname.en.json", "utf8" ) )
		.entries;
	assert.equal( names.SN_SKILL_CH_SWORD_SMASH_A, "Strike n' smash" );
	assert.ok( fs.existsSync( CLIENT_PUBLIC_ROOT + iconPath( "skill\\china\\sword_smash_a.ddj" ) ) );
});
test("skill icons resolve inside the native icon root and reject traversal or remote URLs", () => {
	assert.equal( iconPath( "skill\\china\\SWORD.ddj" ), "/assets/images/Media_extracted/icon/skill/china/sword.png" );
	for ( const path of [ "../../x.ddj", "/x.ddj", "https://example/x.ddj", "xxx", "a//b.ddj" ] ) {
		assert.equal( iconPath( path ), null );
	}
});
test("localized skill names arrive asynchronously; failures retry with bounded backoff", () => {
	let id = 0;
	const results = new Map(), cancelled = [], requests = [];
	const owner = createLocalization( {
		available: () => 1,
		request: url => {
			requests.push( url );
			return ++id;
		},
		take: id => {
			const value = results.get( id );
			results.delete( id );
			return value;
		},
		cancel: id => cancelled.push( id )
	}, "https://fixture.invalid/" );
	owner.step( 0 );
	assert.equal( requests.length, 1 );
	assert.equal( owner.text( "SN_SKILL", "fallback" ), "fallback" );
	results.set( 1, { kind: "error" } );
	owner.step( 1 );
	owner.step( 2000 );
	assert.equal( requests.length, 1 );
	owner.step( 2001 );
	assert.equal( requests.length, 2 );
	results.set( 2, {
		kind: "bytes",
		buffer: new TextEncoder().encode( JSON.stringify( { entries: { SN_SKILL: "Wolf’s Thunderbolt" } } ) ).buffer
	} );
	assert.equal( owner.step( 2002 ), true );
	assert.equal( owner.text( "SN_SKILL", "fallback" ), "Wolf’s Thunderbolt" );
	assert.equal( owner.text( "constructor", "fallback" ), "fallback" );
	owner.step( 90000 );
	assert.equal( requests.length, 2 );
	owner.dispose();
	assert.equal( owner.text( "SN_SKILL", "fallback" ), "fallback" );
});
