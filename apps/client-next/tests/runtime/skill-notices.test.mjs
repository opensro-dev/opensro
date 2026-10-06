/*
===========================================================================

skill-notices.test.mjs - tests for skill-notices.ts, gameplay.ts,
messages.ts, unique-banner.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { skillNotice } = await import( "../../src/engine/foundation/gameplay/skill-notices.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { createHudMessages } = await import( "../../src/engine/runtime/ui/hud/messages.ts" );
const { createUniqueBanner } = await import( "../../src/engine/runtime/ui/hud/unique-banner.ts" );
const copy = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/text/textuisystem.en.json", "utf8" ) ).entries;
test("skill failures preserve all native channels and silent bytes", () => {
	const notice = ( c, country = 0, war = false, pk = false ) =>
		skillNotice( 0xb245, Uint8Array.of( 2, c ), country, war, pk );
	assert.deepEqual( notice( 4 ), {
		key: "UIIT_SKILL_USE_FAIL_NOTENOUGHMP",
		value: 0,
		nativeType: 0,
		banner: true,
		bannerOnly: true
	} );
	assert.equal( notice( 5 ), null );
	assert.equal( notice( 9 ), null );
	assert.equal( notice( 255 ), null );
	assert.equal( defined( notice( 6 ) ).banner, undefined );
	assert.equal( defined( notice( 33 ) ).bannerOnly, undefined );
	assert.equal( defined( notice( 33 ) ).banner, true );
	assert.match( defined( notice( 14, 1 ) ).key, /VOLT$/ );
	assert.equal( notice( 14, 2 ), null, "an empty localized key has no visible message" );
	assert.match( defined( notice( 32, 0, true ) ).key, /FORTRESS$/ );
	assert.match( defined( notice( 22, 0, false, true ) ).key, /PROHIBITED_IN_THIS_SERVER$/ );
	let count = 0;
	for ( let c = 0; c < 256; c++ ) {
		const n = notice( c );
		if ( n ) {
			count++;
			assert.ok( n.key in copy, n.key );
		}
	}
	assert.equal( count, 50 );
	assert.throws( () => skillNotice( 0xb245, Uint8Array.of( 2 ) ) );
});
test("production MP refusals restart warning without status history pollution", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( {} );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0 } );
	const hud = createHudMessages( () => 0 ), banner = createUniqueBanner();
	g.receive( { opcode: 0xb245, payload: Uint8Array.of( 2, 4 ) }, 100 );
	let notices = defined( g.take() ).notices;
	banner.step( notices, 100, true );
	assert.equal( banner.value( k => copy[k] ), copy.UIIT_SKILL_USE_FAIL_NOTENOUGHMP );
	assert.equal( hud.step( 100, [], 1, 0, notices, k => copy[k] ).length, 0 );
	banner.step( notices, 6100, true );
	assert.equal( banner.alpha(), 127 / 255 );
	g.receive( { opcode: 0xb245, payload: Uint8Array.of( 2, 4 ) }, 6200 );
	notices = defined( g.take() ).notices;
	banner.step( notices, 6200, true );
	assert.equal( banner.alpha(), 1 );
	assert.equal( hud.step( 6200, [], 1, 0, notices, k => copy[k] ).length, 0 );
	g.receive( { opcode: 0xb245, payload: Uint8Array.of( 2, 6 ) }, 6300 );
	assert.equal(
		defined( hud.step( 6300, [], 1, 0, defined( g.take() ).notices, k => copy[k] ).at( -1 ) ).value,
		copy.UIIT_SKILL_USE_FAIL_WRONGTARGET
	);
});
