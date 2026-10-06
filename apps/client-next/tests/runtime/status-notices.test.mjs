/*
===========================================================================

status-notices.test.mjs - tests for inventory-notices.ts, party-notices.ts,
target-notices.ts, targeting.ts, ...

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
const { inventoryNotice } = await import( "../../src/engine/foundation/gameplay/inventory-notices.ts" );
const { partyNotice } = await import( "../../src/engine/foundation/gameplay/party-notices.ts" );
const { targetNotice } = await import( "../../src/engine/foundation/gameplay/target-notices.ts" );
const { createTargeting } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/targeting/targeting.ts"
);
const { textBoxLines } = await import( "../../src/engine/foundation/ui/text-lines.ts" );
const { systemMessageLayout } = await import( "../../src/engine/foundation/ui/system-message-layout.ts" );
const { decodeAuthoredLayout } = await import( "../../src/engine/foundation/ui/authored-layout.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { createHudMessages } = await import( "../../src/engine/runtime/ui/hud/messages.ts" );
const copy = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/text/textuisystem.en.json", "utf8" ) ).entries;

test("invitee acknowledgement reports refusals and timeout retires only a party prompt", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( {} );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0, countryByte9c: 0 } );
	g.take();
	for ( const code of [ 12, 23 ] ) {
		g.receive( { opcode: 0xb452, payload: Uint8Array.of( 2, code ) }, 0 );
		const state = g.take();
		assert.deepEqual( { ...defined( defined( state ).notices ).at( -1 ), sequence: undefined }, {
			...partyNotice( code ),
			sequence: undefined
		} );
		assert.deepEqual( defined( defined( state ).social ).members, [] );
	}
	for ( const type of [ 2, 3, 1, 5 ] ) {
		g.receive( {
			opcode: 0x3393,
			payload: Uint8Array.from( [ type, 7, 0, 0, 0, ...(type === 2 || type === 3 ? [ 1 ] : []) ] )
		}, 0 );
		const invitation = defined( defined( g.take() ).social ).invitation;
		g.receive( { opcode: 0xb452, payload: Uint8Array.of( 2, 16 ) }, 0 );
		const timeout = g.take();
		assert.equal( defined( defined( timeout ).notices ).length, 2 );
		assert.equal( defined( defined( timeout ).social ).unresolvedNotice, undefined );
		assert.deepEqual(
			defined( defined( timeout ).social ).invitation,
			type === 2 || type === 3 ? null : invitation,
			"timeout must not dismiss a replacement exchange/guild prompt"
		);
	}
	g.receive( { opcode: 0xb452, payload: Uint8Array.of( 1, 7, 0, 0, 0 ) }, 0 );
	assert.equal( defined( defined( g.take() ).social ).self, 7 );
	for ( const p of [ [], [ 2 ], [ 2, 12, 0 ], [ 1, 7 ] ] ) {
		assert.throws( () => g.receive( { opcode: 0xb452, payload: Uint8Array.from( p ) }, 0 ) );
	}
	assert.equal( g.take(), null );
});

test("party kick, leave and disband failures preserve native category-one routing", () => {
	for ( const opcode of [ 0xb095, 0xb34a, 0xb2db ] ) {
		for ( const country of [ 0, 1 ] ) {
			const g = createGameplay( () => {} );
			g.bootstrap( {} );
			g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0, countryByte9c: country } );
			g.take();
			let count = 0;
			for ( let code = 0; code < 256; code++ ) {
				const payload = Uint8Array.of( 2, code ), expected = inventoryNotice( 0xb06d, payload, country );
				assert.equal( g.receive( { opcode, payload }, 0 ), true );
				const state = g.take();
				assert.deepEqual( defined( defined( state ).social ).members, [] );
				if ( expected ) {
					count++;
					assert.deepEqual( { ...defined( defined( state ).notices ).at( -1 ), sequence: undefined }, {
						...expected,
						sequence: undefined
					} );
				}
				assert.equal( defined( defined( state ).notices ).at( -1 )?.sequence ?? 0, count );
			}
			for ( const flag of [ 0, 1, 3, 255 ] ) {
				g.receive( { opcode, payload: Uint8Array.of( flag ) }, 0 );
				assert.equal( defined( defined( g.take() ).notices ).at( -1 )?.sequence ?? 0, count );
			}
			for ( const payload of [ [], [ 2 ], [ 2, 7, 0 ], [ 1, 0 ] ] ) {
				assert.throws( () => g.receive( { opcode, payload: Uint8Array.from( payload ) }, 0 ) );
			}
			assert.equal( g.take(), null );
		}
	}
});

test("guild notice admission reports native empty-field errors before sending", () => {
	const sent = [], g = createGameplay( frame => sent.push( frame ) );
	g.bootstrap( {} );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0, countryByte9c: 0 } );
	g.take();
	// Minimal guild baseline: ID, empty name, level, GP, empty subject/body,
	// crest, flag, zero members and no vote. Authority arrives on the wire.
	g.receive( {
		opcode: 0x32c4,
		payload: Uint8Array.from( [ 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0 ] )
	}, 0 );
	g.take();
	for ( const [subject, contents, code] of [ [ "", "", 0x22 ], [ "", "body", 0x22 ], [ "title", "", 0x23 ] ] ) {
		assert.equal( g.command( { kind: "guild-notice", subject, contents }, 0 ), null );
		const state = g.take(), notice = defined( defined( state ).notices ).at( -1 );
		assert.equal(
			defined( notice ).key,
			code === 0x22 ?
				"UIIT_MSG_GUILDERR_INVALID_MASTER_COMMENT_TITLE" :
				"UIIT_MSG_GUILDERR_INVALID_MASTER_COMMENT"
		);
		assert.equal( sent.length, 0 );
	}
	g.command( { kind: "guild-notice", subject: "title", contents: "body" }, 0 );
	assert.equal( sent.length, 1 );
	assert.equal( sent[0].opcode, 0x777a );
});

test("party invitation acknowledgements route failures without publishing membership", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( {} );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0, countryByte9c: 0 } );
	g.take();
	let count = 0;
	for ( let code = 0; code < 256; code++ ) {
		const expected = partyNotice( code );
		assert.equal( g.receive( { opcode: 0xb51a, payload: Uint8Array.of( 2, code ) }, 0 ), true );
		const state = g.take();
		assert.deepEqual( defined( defined( state ).social ).members, [] );
		if ( expected ) {
			count++;
			assert.deepEqual( { ...defined( defined( state ).notices ).at( -1 ), sequence: undefined }, {
				...expected,
				sequence: undefined
			} );
		}
		assert.equal( defined( defined( state ).notices ).at( -1 )?.sequence ?? 0, count );
	}
	for ( const flag of [ 0, 1, 3, 255 ] ) {
		g.receive( { opcode: 0xb51a, payload: Uint8Array.of( flag ) }, 0 );
		assert.equal( defined( defined( g.take() ).notices ).at( -1 )?.sequence ?? 0, count );
	}
	for ( const p of [ [], [ 2 ], [ 2, 0x12, 0 ], [ 1, 0 ] ] ) {
		assert.throws( () => g.receive( { opcode: 0xb51a, payload: Uint8Array.from( p ) }, 0 ) );
	}
	assert.equal( g.take(), null );
});

test("academy membership refusals retain authority and route every native error byte", async () => {
	const { constantNativeNotice } = await import( "../../src/engine/foundation/gameplay/native-notice.ts" );
	const g = createGameplay( () => {} );
	g.bootstrap( { academyMember: true } );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0, countryByte9c: 0 } );
	const original = defined( g.take() ).academy;
	let count = 0;
	for ( let code = 0; code < 256; code++ ) {
		const expected = constantNativeNotice( 0x1d, code );
		assert.equal( g.receive( { opcode: 0x3ac5, payload: Uint8Array.of( 10, 2, code ) }, 0 ), true );
		const state = g.take();
		assert.deepEqual( defined( state ).academy, original );
		if ( expected ) {
			count++;
			assert.deepEqual( { ...defined( defined( state ).notices ).at( -1 ), sequence: undefined }, {
				...expected,
				sequence: undefined
			} );
		}
		assert.equal(
			defined( defined( state ).notices ).at( -1 )?.sequence ?? 0,
			count,
			"silent native codes must not publish another notice"
		);
	}
	for ( const payload of [ Uint8Array.of( 10, 2 ), Uint8Array.of( 10, 2, 1, 0 ) ] ) {
		assert.throws( () => g.receive( { opcode: 0x3ac5, payload }, 0 ) );
	}
	assert.equal( g.take(), null, "malformed packets must not dirty state" );
});

test("Magic Pop routes every non-success result through category 1 without mutating inventory", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( {} );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0, countryByte9c: 0 } );
	g.take();
	for ( const flag of [ 0, 2, 255 ] ) {
		for ( let code = 0; code < 256; code++ ) {
			const expected = inventoryNotice( 0xb06d, Uint8Array.of( 2, code ), 0 );
			assert.deepEqual( inventoryNotice( 0xb053, Uint8Array.of( flag, code ), 0 ), expected );
			g.receive( { opcode: 0xb053, payload: Uint8Array.of( flag, code ) }, 0 );
			const state = g.take();
			assert.deepEqual( defined( state ).inventory, [] );
			if ( expected ) {
				assert.deepEqual( { ...defined( defined( state ).notices ).at( -1 ), sequence: undefined }, {
					...expected,
					sequence: undefined
				} );
			}
		}
	}
	for ( const p of [ [], [ 2 ], [ 2, 7, 0 ] ] ) {
		assert.throws( () => g.receive( { opcode: 0xb053, payload: Uint8Array.from( p ) }, 0 ) );
	}
	assert.equal( g.take(), null );
	assert.equal( inventoryNotice( 0xb053, Uint8Array.of( 1, 1 ), 0 ), null );
});

test("empty native guides cannot evict history or reappear after localization changes", () => {
	const hud = createHudMessages( () => 0 );
	for ( let i = 0; i < 100; i++ ) hud.append( "retained " + i );
	const notices = Array.from(
		{ length: 120 },
		( _, i ) => ({ sequence: i + 1, key: "missing", value: 0, nativeType: 5 })
	);
	const step = lookup => hud.step( 0, [], 1, 0, notices, lookup );
	const before = step( () => "" );
	assert.equal( before.length, 100 );
	assert.equal( before[0].value, "retained 0" );
	assert.deepEqual( step( () => "late localization" ), before, "consumed empty sequences must not replay" );
	hud.append( "\0discard" );
	assert.deepEqual( step( () => "" ), before );
	hud.append( "visible\0hidden" );
	const after = step( () => "" );
	assert.equal( after.length, 100 );
	assert.equal( defined( after.at( -1 ) ).value, "visible" );
	assert.equal( after[0].value, "retained 1" );
});

test("unsequenced guide projection shares native filters, colors and empty-string admission", () => {
	const hud = createHudMessages( () => 0 ),
		notices = [
			{ key: "blocked", value: 0, nativeType: 5 },
			{ key: "allowed", value: 0, nativeType: 0 },
			{ key: "tip", value: 0, nativeType: 6 },
			{ key: "missing", value: 0, nativeType: 0 },
			{ key: "banner", value: 0, bannerOnly: true },
			{ key: "terminated", value: 0, nativeType: 0 }
		];
	const lines = hud.step(
		0,
		[],
		1,
		0,
		notices,
		k => k === "missing" ? "" : k === "terminated" ? "\0discard" : k,
		true,
		new Set()
	);
	assert.deepEqual( lines.map( r => [ r.value, r.colorArgb ] ), [ [ "allowed", 0xffdbc99b ], [
		"tip",
		0xffbacff2
	] ] );
});

test("empty scheduled tips consume their timer without inserting a history entry", () => {
	const hud = createHudMessages( () => 0 ),
		tips = [ { id: 1, type: 3, minLevel: 1, maxLevel: 99, text: "\0discard" } ];
	hud.append( "keep" );
	hud.step( 0, tips, 1, 0, [], () => "" );
	assert.deepEqual( hud.step( 60000, tips, 1, 0, [], () => "" ).map( r => r.value ), [ "keep" ] );
	assert.equal( hud.deadline(), 120000 );
});

test("party refusal routes mapped bytes through production gameplay without placeholder errors", () => {
	const mapped = [
		2,
		3,
		4,
		5,
		6,
		7,
		8,
		10,
		12,
		13,
		14,
		15,
		16,
		17,
		18,
		19,
		20,
		23,
		24,
		27,
		28,
		29,
		30,
		31,
		35,
		36,
		37,
		38
	];
	const g = createGameplay( () => {} );
	g.bootstrap( {} );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0 } );
	for ( let code = 0; code < 256; code++ ) {
		const expected = partyNotice( code );
		assert.equal( !!expected, mapped.includes( code ) );
		assert.equal( g.receive( { opcode: 0xb0d5, payload: Uint8Array.of( 2, code ) }, 0 ), true );
		const state = g.take();
		assert.equal( defined( state ).error, null );
		if ( expected ) {
			assert.deepEqual( { ...defined( defined( state ).notices ).at( -1 ), sequence: undefined }, {
				...expected,
				sequence: undefined
			} );
			assert.equal( expected.nativeType, 4 );
			assert.equal( expected.banner, code === 7 ? true : undefined );
		}
	}
	assert.equal( defined( partyNotice( 13 ) ).key, "UIIT_MSG_PARTYERR_CANT_FIND_PARTY" );
	assert.equal( defined( partyNotice( 28 ) ).key, defined( partyNotice( 13 ) ).key );
});

test("horse use-level refusal reaches both native banner and guide", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( {} );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0 } );
	g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 0x6c ) }, 0 );
	const notice = defined( defined( g.take() ).notices ).at( -1 );
	assert.equal( defined( notice ).key, "UIIT_MSG_STRGERR_HIGHER_LEVEL_REQUIRED_TO_USE_THISITEM" );
	assert.equal( defined( notice ).banner, true );
	assert.equal( defined( notice ).nativeType, 5 );
	assert.ok( copy[defined( notice ).key] );
});

test("native guide filters affect admission, not retained history or banners", () => {
	const hud = createHudMessages( () => 0 ), filters = new Set( [ "game" ] );
	const notice = ( sequence, nativeType = 5 ) => ({ sequence, nativeType, key: "test", value: 0, banner: true });
	const step = notices => hud.step( 0, [], 1, 0, notices, () => "message", true, filters );
	assert.equal( step( [ notice( 1 ) ] ).length, 1 );
	filters.clear();
	assert.equal(
		step( [ notice( 1 ), notice( 2 ) ] ).length,
		1,
		"existing history stays; new filtered notice is discarded"
	);
	filters.add( "game" );
	assert.equal( step( [ notice( 1 ), notice( 2 ) ] ).length, 1, "discarded sequence never reappears" );
	filters.clear();
	assert.equal( step( [ notice( 3, 0 ), notice( 4, 6 ) ] ).length, 3, "types 0 and 6 bypass filters" );
});

test("all target refusal bytes terminate selection; only native error 7 emits a guide", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( {} );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0 } );
	const targeting = createTargeting( () => {} );
	for ( let code = 0; code < 256; code++ ) {
		const payload = Uint8Array.of( 2, code );
		targeting.select( 9, 0 );
		assert.equal( targeting.receive( 0xb45a, payload ), true );
		assert.equal( targeting.state().targetPending, 0 );
		assert.equal( targeting.state().target, 0 );
		assert.equal( g.receive( { opcode: 0xb45a, payload }, 0 ), true );
		assert.deepEqual(
			targetNotice( 0xb45a, payload ),
			code === 7 ? { key: "UIIT_MSG_STRGERR_CANT_SWAP_JOBITEM", value: 0, nativeType: 5 } : null
		);
	}
	assert.deepEqual( defined( defined( g.take() ).notices ).map( n => n.key ), [
		"UIIT_MSG_STRGERR_CANT_SWAP_JOBITEM"
	] );
	targeting.select( 9, 0 );
	for ( const payload of [ Uint8Array.of( 2 ), Uint8Array.of( 2, 7, 0 ) ] ) {
		assert.throws( () => targeting.receive( 0xb45a, payload ), /Invalid target rejection/ );
		assert.throws( () => targetNotice( 0xb45a, payload ), /Invalid target rejection/ );
		assert.equal( targeting.state().targetPending, 9 );
	}
});

test("target rejection preserves previous selection and cannot consume a release barrier", () => {
	const targeting = createTargeting( () => {} );
	targeting.select( 8, 0, "player" );
	targeting.select( 9, 0 );
	targeting.receive( 0xb45a, Uint8Array.of( 2, 7 ) );
	assert.equal( targeting.state().target, 8 );
	targeting.release( 0 );
	targeting.receive( 0xb45a, Uint8Array.of( 2, 7 ) );
	assert.equal( targeting.state().targetPending, 8 );
	targeting.receive( 0xb4b3, Uint8Array.of( 1 ) );
	assert.equal( targeting.state().target, 0 );
});

test("inventory refusal family routes native text, country split and conditional banner", () => {
	const notice = ( code, country = 0, mall = false ) =>
		inventoryNotice( 0xb06d, Uint8Array.of( 2, code ), country, mall );
	assert.equal( defined( notice( 0x32 ) ).key, "UIIT_MSG_STRGERR_CANT_MIX_EXCLUSIVE_ARMOR_TYPE" );
	assert.equal( defined( notice( 0x32, 1 ) ).key, "UIIT_MSG_STRGERR_EU_CANT_MIX_EXCLUSIVE_ARMOR_TYPE" );
	assert.equal( notice( 0x32, 2 ), null );
	assert.equal( defined( notice( 0x30 ) ).key, "UIIT_MSG_STRGERR_HIGHER_STRENGTH_REQUIRED" );
	assert.equal( defined( notice( 0x31 ) ).key, "UIIT_MSG_STRGERR_HIGHER_INTELLECT_REQUIRED" );
	assert.equal( defined( notice( 7 ) ).banner, undefined );
	assert.equal( defined( notice( 7, 0, true ) ).banner, true );
	let count = 0;
	for ( let code = 0; code < 256; code++ ) {
		const n = notice( code );
		if ( n ) {
			count++;
			assert.equal( n.nativeType, code === 0x71 ? 0 : 5 );
			assert.equal( n.bannerOnly, code === 0x71 ? true : undefined );
			assert.ok( n.key in copy, n.key );
			assert.ok( !/%[sd]/.test( copy[n.key] ), "unexpected formatting arguments: " + n.key );
		}
	}
	assert.equal( count, 159 );
	assert.equal( notice( 0xff ), null );
	assert.equal( inventoryNotice( 0xb06d, Uint8Array.of( 1, 0 ) ), null );
	assert.throws( () => inventoryNotice( 0xb06d, Uint8Array.of( 2 ) ) );
	assert.throws( () => inventoryNotice( 0xb06d, Uint8Array.of( 2, 50, 0 ) ) );
	for ( let code = 0; code < 256; code++ ) {
		assert.deepEqual(
			inventoryNotice( 0xb5bd, Uint8Array.of( 2, code ), 0 ),
			notice( code ),
			"item-use and inventory call the same native error mapper"
		);
	}
});

test("item-use rejection publishes through production gameplay", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( {} );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0, countryByte9c: 0 } );
	assert.equal( g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 0x30 ) }, 0 ), true );
	assert.equal(
		defined( defined( defined( g.take() ).notices ).at( -1 ) ).key,
		"UIIT_MSG_STRGERR_HIGHER_STRENGTH_REQUIRED"
	);
	g.dispose();
});

test("repeated server refusals publish independently through production gameplay", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( {} );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0, countryByte9c: 1 } );
	for ( let i = 0; i < 2; i++ ) {
		assert.equal( g.receive( { opcode: 0xb06d, payload: Uint8Array.of( 2, 0x32 ) }, 0 ), true );
	}
	const notices = defined( defined( g.take() ).notices ).filter( n => n.nativeType === 5 );
	assert.equal( notices.length, 2 );
	assert.ok( defined( notices[1].sequence ) > defined( notices[0].sequence ) );
	assert.match( notices[0].key, /_EU_/ );
	const hud = createHudMessages( () => 0 );
	const rows = hud.step( 0, [], 1, 0, notices, () => "Armor and robe cannot be equipped at the same time." );
	assert.equal( rows.length, 2 );
	assert.ok( rows.every( r => r.category === "game" && r.colorArgb === 0xffdbc99b ) );
	g.bootstrap( {} );
	g.receive( { opcode: 0xb06d, payload: Uint8Array.of( 2, 0x32 ) }, 0 );
	assert.equal(
		defined( defined( g.take() ).notices ).filter( n => n.nativeType === 5 ).length,
		0,
		"country cannot leak across bootstrap"
	);
});

test("native textbox keeps spaces, escape grammar and post-wrap continuation indentation", () => {
	const measure = s => s.length * 5;
	assert.deepEqual( textBoxLines( "aa bb cc", 25, measure, true ), [ "aa ", "    bb cc" ] );
	assert.deepEqual( textBoxLines( "a\\nb\nc\\\nd", 100, measure, true ), [ "a", "b", "cd" ] );
	assert.deepEqual( textBoxLines( "  aa ", 100, measure, true ), [ "  aa " ] );
	assert.deepEqual( textBoxLines( "abc", 1, measure, true ), [ "a", "    b", "    c" ] );
});

test("short status histories align to bottom and resizing preserves last row", () => {
	const layout = decodeAuthoredLayout(
		JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifsystemmessage.json", "utf8" ) )
	);
	const draw = ( groups, lines, offset = 0 ) => {
		const painted = [];
		systemMessageLayout(
			layout,
			1024,
			768,
			groups,
			lines,
			() => [ 16, 16 ],
			( value, rect, clip, color ) => {
				painted.push( { value, rect, clip, color } );
				return [];
			},
			null,
			null,
			offset,
			s => s.length * 5
		);
		return painted;
	};
	const one = draw( 2, [ "Cerberus has appeared." ] );
	assert.equal( one[0].rect[1], 646 );
	assert.equal( draw( 4, [ "Cerberus has appeared." ] )[0].rect[1], 646 );
	assert.equal( draw( 2, [ "", "Cerberus has appeared." ] ).length, 1 );
	const full = draw( 2, Array.from( { length: 8 }, ( _, i ) => String( i ) ), 1 );
	assert.deepEqual( full.map( r => r.value ), [ "1", "2", "3", "4", "5", "6" ] );
	assert.equal( full.at( -1 ).rect[1], 646 );
});

test("every item-use and inventory refusal byte reaches its native route without fabricating silent feedback", () => {
	for ( const opcode of [ 0xb5bd, 0xb06d ] ) {
		for ( const country of [ 0, 1 ] ) {
			const g = createGameplay( () => {} );
			g.bootstrap( {} );
			g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0, countryByte9c: country } );
			g.take();
			let sequence = 0;
			for ( let code = 0; code < 256; code++ ) {
				const payload = Uint8Array.of( 2, code ), expected = inventoryNotice( opcode, payload, country );
				assert.equal( g.receive( { opcode, payload }, code ), true );
				const state = g.take(), last = defined( defined( state ).notices ).at( -1 );
				assert.deepEqual( defined( state ).inventory, [] );
				assert.deepEqual( defined( state ).itemCooldowns, [] );
				assert.equal( defined( state ).inventoryPending, false );
				if ( expected ) {
					assert.ok( defined( defined( last ).sequence ) > sequence );
					sequence = defined( last ).sequence;
					assert.deepEqual( { ...last, sequence: undefined }, { ...expected, sequence: undefined } );
				} else assert.equal( last?.sequence ?? 0, sequence, "native silent code must not add a notice" );
			}
			g.dispose();
		}
	}
	const generic = inventoryNotice( 0xb5bd, Uint8Array.of( 2, 2 ), 0 );
	assert.equal( generic, null, "full-pool server response remains native silent" );
	const reuse = inventoryNotice( 0xb5bd, Uint8Array.of( 2, 0x5b ), 0 );
	assert.equal( defined( reuse ).key, "UIIT_MSG_STRGERR_WAIT_FOR_REUSE_DELAY" );
	assert.equal( defined( reuse ).banner, undefined );
	assert.equal( defined( reuse ).nativeType, 5 );
	const hud = createHudMessages( () => 0 ),
		lines = hud.step( 0, [], 1, 0, [ { ...reuse, sequence: 1 } ], key => copy[key] );
	assert.equal( defined( lines.at( -1 ) ).category, "game" );
	assert.equal( defined( lines.at( -1 ) ).value, copy[defined( reuse ).key] );
});
