/*
===========================================================================

character-creation.test.mjs - tests for dialog.ts, creation.ts,
character-create.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { defined } from "../helpers/defined.mjs";
const { createCharacterDialog } = await import( "../../src/engine/runtime/frontend/dialog/dialog.ts" );
const { createCreation } = await import( "../../src/engine/runtime/frontend/creation/creation.ts" );
const { creationLoadout, creationStarterCodenames, initialCreation, creationRange } = await import(
	"../../src/engine/foundation/ui/character-create.ts"
);
const { createItemCodenameIndex } = await import( "../../src/engine/foundation/animation/equipment-appearance.ts" );
const { readPublishedAssetJsonSync } = await import( "../../../../scripts/lib/publishedAsset.mjs" );
test("creation status sounds follow native operations, including repeated validation and silent frame holds", () => {
	const { owner, commands, sounds } = creation();
	owner.action( "create:ok" );
	assert.equal( sounds.length, 2, "0x72FC90 calls the status helper and the sound vfunc" );
	for ( let i = 0; i < 10; i++ ) owner.step( .016 );
	assert.equal( sounds.length, 2 );
	owner.action( "create:ok" );
	assert.equal( sounds.length, 4, "a repeated action is a fresh notification" );
	owner.action( "create:name", "ProbeChar" );
	owner.action( "create:check" );
	const result = { ...commands.at( -1 ), status: "succeeded" };
	owner.step( 0, result );
	assert.equal( sounds.length, 5, "available-name confirmation uses the same native status bell" );
	owner.step( 0, result );
	assert.equal( sounds.length, 5, "a consumed reply never plays twice" );
	owner.dispose();
});
test("warning captures identity, rejects duplicate submits and stale results, and fades closed", () => {
	const sent = [],
		dialog = createCharacterDialog( c => sent.push( c ) ),
		a = { id: 12, name: "Scratch", deletePending: false },
		b = { id: 13, name: "Other", deletePending: false };
	dialog.open( a );
	dialog.accept();
	assert.equal( sent.length, 0 );
	dialog.step( .5, undefined, [ a, b ] );
	dialog.open( b );
	dialog.accept();
	dialog.accept();
	dialog.cancel();
	assert.equal( sent.length, 1 );
	assert.equal( sent[0].characterName, "Scratch" );
	assert.equal( dialog.step( 0, { ...sent[0], operationId: 999, status: "succeeded" }, [ a, b ] ), null );
	assert.equal( defined( dialog.snapshot() ).phase, "pending" );
	assert.equal( defined( dialog.step( 0, { ...sent[0], status: "succeeded" }, [ a, b ] ) ).status, "succeeded" );
	dialog.step( .5, undefined, [ a, b ] );
	assert.equal( dialog.snapshot(), null );
	dialog.open( { ...a, deletePending: true } );
	dialog.step( .5, undefined, [ a, b ] );
	dialog.accept();
	assert.equal( sent.at( -1 ).kind, "restore-character" );
	assert.notEqual( sent[0].operationId, sent.at( -1 ).operationId );
});
function creation() {
	const sounds = [],
		commands = [],
		bytes = readFileSync( "../../.generated/client-public/assets/textdata/abusefilter.txt" );
	let requested = false;
	const assets = {
		available: () => 4,
		request: () => {
			requested = true;
			return 1;
		},
		take: () =>
			requested ?
				{ kind: "bytes", buffer: bytes.buffer.slice( bytes.byteOffset, bytes.byteOffset + bytes.byteLength ) } :
				null,
		cancel() {}
	};
	const owner = createCreation( assets, "http://localhost", c => commands.push( c ), () => sounds.push( "message" ) );
	owner.open( 0 );
	owner.step( 0 );
	owner.step( 0 );
	return { owner, commands, sounds };
}

test("a replaced roster invalidates and cancels the captured dialog operation", () => {
	const commands = [],
		owner = createCharacterDialog( c => commands.push( c ) ),
		row = { id: 1, name: "fixture", deletePending: false };
	owner.open( row );
	owner.step( .5, undefined, [ row ] );
	owner.accept();
	owner.step( 0, undefined, [ { ...row, name: "replacement" } ] );
	assert.equal( owner.snapshot(), null );
	assert.equal( commands.at( -1 ).kind, "cancel-character-operation" );
});

test("native guild and academy blockers produce status instead of admitting a dialog", () => {
	const commands = [], owner = createCharacterDialog( c => commands.push( c ) );
	for (
		const [deletionBlocker, suffix] of [ [ "guild-master", "MASTER" ], [ "guild-member", "MEMBER" ], [
			"academy-guardian",
			"GUARDIAN"
		], [ "academy-student", "STUDENT" ] ]
	) {
		assert.equal(
			owner.open( { id: 1, name: "fixture", deletePending: false, deletionBlocker } ),
			"UIO_MSG_CHAR_DEL_WANNING_CONFIRM_" + suffix
		);
		assert.equal( owner.snapshot(), null );
		owner.accept();
	}
	assert.deepEqual( commands, [] );
});

test("leaving creation releases a pending name-filter reservation and reentry requests afresh", () => {
	const cancelled = [];
	let serial = 0;
	const owner = createCreation(
		{
			available: () => 1,
			request: () => ++serial,
			take: () => null,
			cancel: id => cancelled.push( id )
		},
		"http://localhost",
		() => {}
	);
	owner.open( 0 );
	owner.step( 0 );
	owner.reset();
	assert.deepEqual( cancelled, [ 1 ] );
	owner.open( 1 );
	owner.step( 0 );
	assert.equal( serial, 2 );
	owner.dispose();
	assert.deepEqual( cancelled, [ 1, 2 ] );
});

test("confirmation cancellation fades, blocks reentry and returns to the unchanged draft", () => {
	const { owner, commands } = creation();
	owner.action( "create:name", "ProbeChar" );
	owner.action( "create:weapon:next" );
	owner.action( "create:protector:next" );
	owner.action( "create:ok" );
	owner.step( .5 );
	owner.action( "create:confirm-cancel" );
	owner.action( "create:confirm" );
	owner.step( .25 );
	assert.equal( defined( owner.snapshot() ).phase, "dismissing" );
	assert.equal( defined( owner.snapshot() ).alpha, .5 );
	owner.step( .25 );
	assert.equal( defined( owner.snapshot() ).phase, "editing" );
	assert.equal( defined( owner.snapshot() ).selection.name, "ProbeChar" );
	assert.deepEqual( commands, [] );
});
test("creation validation, immutable confirmation, single submission, rejection and retry", () => {
	const { owner, commands } = creation();
	owner.action( "create:ok" );
	assert.equal( defined( defined( owner.snapshot() ).status ).key, "UIO_MSG_ERROR_CHARACTER_SELECTARMOR" );
	owner.action( "create:weapon:next" );
	owner.action( "create:protector:next" );
	owner.action( "create:ok" );
	assert.equal( defined( defined( owner.snapshot() ).status ).key, "UIO_MSG_ERROR_CHARACTER_NAME_STRING" );
	owner.action( "create:name", "ProbeChar" );
	owner.action( "create:check" );
	assert.equal( commands.at( -1 ).kind, "check-name" );
	owner.action( "create:name", "Changed" );
	assert.equal( defined( owner.snapshot() ).selection.name, "ProbeChar" );
	owner.step( 0, { ...commands.at( -1 ), status: "succeeded" } );
	assert.equal( defined( defined( owner.snapshot() ).status ).key, "UIO_MSG_ERROR_ADMISSON" );
	owner.action( "create:ok" );
	owner.step( .5 );
	owner.action( "create:confirm" );
	owner.action( "create:confirm" );
	owner.step( .5 );
	assert.equal( commands.filter( c => c.kind === "create-character" ).length, 1 );
	assert.equal( commands.at( -1 ).draft.characterName, "ProbeChar" );
	owner.step( 0, { ...commands.at( -1 ), status: "failed", nativeErrorCode: 17 } );
	assert.equal( defined( owner.snapshot() ).phase, "editing" );
	assert.equal( defined( defined( owner.snapshot() ).status ).key, "UIO_MSG_ERROR_OVERLAP" );
	owner.action( "create:ok" );
	owner.step( .5 );
	owner.action( "create:confirm" );
	owner.step( .5 );
	assert.equal( owner.step( .5, { ...commands.at( -1 ), status: "succeeded" } ), true );
});
test("cancelled checks cannot affect a new creation session", () => {
	const { owner, commands } = creation();
	owner.action( "create:name", "ProbeChar" );
	owner.action( "create:check" );
	const old = commands.at( -1 );
	owner.reset();
	assert.equal( commands.at( -1 ).kind, "cancel-character-operation" );
	owner.open( 1 );
	owner.action( "create:name", "NewProbe" );
	owner.action( "create:check" );
	owner.step( 0, { ...old, status: "failed", nativeErrorCode: 17 } );
	assert.equal( defined( owner.snapshot() ).phase, "checking" );
});
/*
================
creation starter items follow the native weapon and protector rules

CPSCharacterCreateEurope_OnProtectorChanged (0x730690) and the creation
weapon list: Europe choice 6 is the darkstaff, robe only; no protector
grants no garments.
================
*/
test("creation starter items follow the native weapon and protector rules", () => {
	const europe = { ...initialCreation( 0 ), gender: 1, figure: 1 };
	assert.deepEqual( creationStarterCodenames( { ...europe, weapon: 6, protector: 1 } ), [
		"ITEM_EU_W_CLOTHES_01_BA_A_DEF",
		"ITEM_EU_W_CLOTHES_01_LA_A_DEF",
		"ITEM_EU_W_CLOTHES_01_FA_A_DEF",
		"ITEM_EU_DARKSTAFF_01_A_DEF"
	] );
	assert.equal( creationRange( { ...europe, weapon: 6 }, "protector" )[1], 1, "darkstaff takes the robe only" );
	assert.equal( creationRange( { ...europe, weapon: 9 }, "protector" )[1], 2, "one-hand staff: light or robe" );
	assert.deepEqual( creationStarterCodenames( { ...europe, weapon: 9, protector: 2 } ).slice( -2 ), [
		"ITEM_EU_STAFF_01_A_DEF",
		"ITEM_EU_SHIELD_01_A_DEF"
	] );
	const china = { ...initialCreation( 1 ), gender: 0, figure: 1 };
	assert.deepEqual( creationStarterCodenames( { ...china, weapon: 2, protector: 0 } ), [
		"ITEM_CH_BLADE_01_A_DEF",
		"ITEM_CH_SHIELD_01_A_DEF"
	], "no protector: no garments" );
});

test("all creation figures and equipment resolve to published native preview resources", () => {
	const catalog = readPublishedAssetJsonSync(
		"/assets/char/roster.json",
		path.join( root, "../../.generated/client-public" )
	);
	const models = Object.values( catalog.models ), itemIds = createItemCodenameIndex( catalog.dress );
	for ( const race of [ 0, 1 ] ) {
		for ( const gender of [ 0, 1 ] ) {
			const body = (race === 0 ? "EU" : "CH") + "_" + (gender === 0 ? "M" : "W");
			for ( let figure = 1; figure <= 13; figure++ ) {
				for ( let weapon = 0; weapon <= (race === 0 ? 9 : 5); weapon++ ) {
					const base = { ...initialCreation( race ), gender, figure, weapon };
					for ( let protector = 0; protector <= creationRange( base, "protector" )[1]; protector++ ) {
						const s = { ...base, protector },
							loadout = creationLoadout( s, itemIds ),
							model = models.find( m => m.codename === loadout.modelCodename );
						assert.ok( model?.previewGlb, loadout.modelCodename );
						for ( const item of loadout.items ) {
							assert.ok(
								catalog.dress.equipment[item.refObjId]?.bodies[body],
								item.refObjId + " " + body
							);
						}
					}
				}
			}
		}
	}
});
