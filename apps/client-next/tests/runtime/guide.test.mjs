/*
===========================================================================

guide.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { isPlaceholderText, loadEnglishCompletions } from "../../../../scripts/build/shared/englishCompletions.mjs";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { collectGuideImageReferences } from "../../../../scripts/build/shared/guideImageReferences.mjs";
import {
	readText,
	textDataDir,
	imageSourceRoot,
	imagePublicRoot
} from "../../../../scripts/build/shared/resourceIo.mjs";
import path from "node:path";
import { readLocalizedTextDataRowsSync } from "../../../../scripts/build/shared/textDataIo.mjs";
/*
================
load
================
*/
async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
const { guideBootstrap, revealGuide, automaticGuide } = await load( "src/engine/foundation/gameplay/guide.ts" );
const { guideTokens } = await load( "src/engine/foundation/ui/guide-content.ts" );
const { createGuideResources } = await load( "src/engine/runtime/ui/guide/resources.ts" );
const { createGameplay } = await load( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" );
const { createEntities } = await load( "src/engine/runtime/simulation/worker/session/world/entities/entities.ts" );
const { generalGuideArticles } = await load( "src/engine/foundation/ui/guide-catalog.ts" );

test("guide publication discovers inline images and preserves converted retail pixels", async () => {
	assert.deepEqual(
		collectGuideImageReferences(
			'<IMG src="interface\\guide\\gd_start.ddj"><img src="interface/guide/gd_start.ddj">'
		),
		[ "interface/guide/gd_start.ddj" ]
	);
	assert.throws( () => collectGuideImageReferences( '<img src="interface/../secret.ddj">' ) );
	const references = collectGuideImageReferences( await readText( path.join( textDataDir, "texthelp.txt" ) ) );
	assert.ok( references.includes( "interface/guide/gd_start.ddj" ) );
	assert.ok( references.includes( "interface/image/eguide_minimap.ddj" ) );
	for ( const reference of references ) {
		const relative = path.join( "Media_extracted", reference.replace( /\.ddj$/, ".png" ) );
		assert.deepEqual(
			await readFile( path.join( imagePublicRoot, relative ) ),
			await readFile( path.join( imageSourceRoot, relative ) ),
			reference
		);
	}
});
test("gameplay retains guide state only after successful native enqueue and resets at bootstrap", () => {
	let refuse = true;
	const sent = [],
		owner = createGameplay( f => {
			if ( refuse ) throw Error( "closed transport" );
			sent.push( f );
		} );
	owner.bootstrap( { localPlayerEntry: { countryByte9c: 0 }, eventGuideStateMask: 0 } );
	owner.seed( { gid: 1, regionId: 0x62a7, x: 0, y: 0, z: 0, heading: 0 } );
	assert.throws( () => owner.command( { kind: "guide-event", event: 1 }, 0, undefined ), /closed transport/ );
	assert.equal( owner.take().guide.seenMask, 0 );
	refuse = false;
	owner.command( { kind: "guide-event", event: 1 }, 0, undefined );
	assert.equal( owner.take().guide.seenMask, 1 );
	owner.command( { kind: "guide-event", event: 1 }, 0, undefined );
	assert.equal( sent.length, 1 );
	owner.bootstrap( { localPlayerEntry: { countryByte9c: 0 }, eventGuideStateMask: 0 } );
	assert.equal( owner.take().guide.seenMask, 0 );
});
test("local spawn retains server visual flags instead of synthesizing a beginner mark", () => {
	const owner = createEntities();
	owner.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		character: { name: "Fixture" },
		localPlayerEntry: {
			modelRef: 1933,
			visualFlags: 2,
			startProfile: { regionId: 0x62a7, x: 0, y: 0, z: 0, angle: 0 }
		}
	} );
	const payload = new Uint8Array( 8 );
	new DataView( payload.buffer ).setUint32( 0, 7, true );
	owner.receive( { opcode: 0x32a6, payload } );
	assert.equal( owner.take().events.find( e => e.kind === "spawn" ).entity.visualFlags, 2 );
});
test("first entry acknowledges exact native mask once; durable re-entry preserves all bits", () => {
	const original = guideBootstrap( { localPlayerEntry: { countryByte9c: 0 }, eventGuideStateMask: 0x80000000 } ),
		next = revealGuide( original, 1 );
	assert.deepEqual( [ ...next.frame.payload ], [ 1, 0, 0, 128 ] );
	assert.equal( next.frame.opcode, 0x707b );
	assert.equal( original.seenMask, 0x80000000 );
	assert.equal( revealGuide( next.state, 1 ), null );
	assert.equal(
		revealGuide(
			guideBootstrap( { localPlayerEntry: { countryByte9c: 0 }, eventGuideStateMask: next.state.seenMask } ),
			1
		),
		null
	);
	assert.throws( () => guideBootstrap( { localPlayerEntry: { countryByte9c: 0 }, eventGuideStateMask: -1 } ) );
	assert.throws( () => revealGuide( original, 22 ) );
});
test("automatic guide scan preserves native precedence and novice-region edges", () => {
	const facts = { moved: true, regionId: 0x62a7, monster: true, hp: 50, maxHp: 100 };
	assert.equal( automaticGuide( 0, facts ), 7 );
	assert.equal( automaticGuide( 64, facts ), 5 );
	assert.equal( automaticGuide( 80, facts ), 2 );
	assert.equal( automaticGuide( 82, { ...facts, moved: false, regionId: 0x62a9 } ), null );
	assert.equal( automaticGuide( 82, { ...facts, moved: false, regionId: 0x62aa } ), 4 );
});
test("published event articles parse without dropping native images, colors or emphasis", async () => {
	const data = JSON.parse(
		await readFile( "../../.generated/client-public/assets/data/event-guide-catalog.json", "utf8" )
	);
	const nativeQuest = readLocalizedTextDataRowsSync( path.join( textDataDir, "texthelp.txt" ) ).find( row =>
		row[1] === "SRO_GGW_EVE_QUEST"
	);
	assert.equal( nativeQuest?.[8], "", "the extracted English quest article is authored empty" );
	// Retail ships this article without English; the product localization
	// layer (englishCompletions/texthelp.json) supplies it.
	assert.equal(
		data.eventRowsByState[21].englishContent,
		loadEnglishCompletions( "texthelp.txt" ).SRO_GGW_EVE_QUEST?.english
	);
	for ( const [id, row] of Object.entries( data.eventRowsByState ) ) {
		assert.ok( guideTokens( row.englishContent ).length > 0, `article ${id}` );
	}
	const first = guideTokens( data.eventRowsByState[1].englishContent );
	assert.ok( first.some( t => t.kind === "image" && t.path.endsWith( "gd_start.png" ) ) );
	assert.ok( first.some( t => t.kind === "text" && t.strong ) );
	assert.ok( first.some( t => t.kind === "text" && t.color?.[0] === 1 ) );
	assert.throws( () => guideTokens( '<img src="interface\\..\\secret.ddj">' ) );
});
test("guide resource lifetime cancels owned work and ignores late completions", () => {
	let serial = 0, takes = 0;
	const canceled = [];
	const owner = createGuideResources( {
		available: () => 4,
		request: () => ++serial,
		take: () => {
			takes++;
			return null;
		},
		cancel: id => canceled.push( id )
	}, "http://fixture.invalid" );
	owner.step();
	assert.equal( serial, 8 );
	owner.dispose();
	owner.step();
	assert.deepEqual( canceled, [ 1, 2, 3, 4, 5, 6, 7, 8 ] );
	assert.equal( takes, 0 );
	assert.equal( owner.data(), null );
});
test("published guide resource batch admits completely", async () => {
	const paths = [
		"cif/layouts/ifgameguide.json",
		"data/event-guide-catalog.json",
		"text/texthelp.en.json",
		"text/textuisystem.en.json",
		"cif/layouts/ifmentormatch.json",
		"cif/layouts/ifmentormatchslot.json",
		"data/questData.json",
		"cif/layouts/ifggmenu.json"
	];
	const buffers = await Promise.all( paths.map( async p => {
		const b = await readFile( "../../.generated/client-public/assets/" + p );
		return b.buffer.slice( b.byteOffset, b.byteOffset + b.byteLength );
	} ) );
	let id = 0;
	const owner = createGuideResources( {
		available: () => 4,
		request: () => ++id,
		take: i => ({ kind: "bytes", buffer: buffers[i - 1] }),
		/*
================
cancel
================
		*/
		cancel() {}
	}, "http://fixture.invalid" );
	owner.step();
	owner.step();
	assert.equal( owner.error(), null );
	assert.equal( owner.data().articles.length, 21 );
	const data = owner.data();
	const pictures = [
		...data.general.map( row => row.tokens ),
		...data.articles.flatMap( row => [ row.tokens, row.european ?? [] ] )
	].flat().filter( token => token.kind === "image" );
	assert.ok( pictures.length > 0 );
	for ( const image of pictures ) assert.ok( data.warmPaths.includes( image.path ), image.path );
	for ( const layout of [ data.layout, data.menu, data.mentor, data.mentorSlot ] ) {
		for ( const node of Object.values( layout ) ) {
			if ( node.texture?.endsWith( ".png" ) ) {
				assert.ok( data.warmPaths.includes( node.texture ), node.texture );
			}
		}
	}
	assert.equal( new Set( data.warmPaths ).size, data.warmPaths.length );
	for ( const article of owner.data().articles ) {
		// Article 21 has English from the localization layer now, so it shares
		// the non-European shape: its European branch is its own content.
		if ( [ 1, 3, 13 ].includes( article.id ) ) assert.notDeepEqual( article.tokens, article.european );
		else {
			assert.ok( article.tokens.length > 0, `article ${article.id}` );
			assert.deepEqual( article.tokens, article.european );
		}
	}
	// This extracted media's English European fields are empty. Editing notes
	// from another column must never become guide text or trigger a seen ack.
	const source = JSON.parse(
		await readFile( "../../.generated/client-public/assets/data/event-guide-catalog.json", "utf8" )
	);
	for ( const id of [ 1, 3, 13 ] ) {
		assert.equal(
			owner.data().articles.find( a => a.id === id ).european === null,
			source.eventRowsByState[id].englishEuropeanContent === ""
		);
	}
	const retained = owner.quests( 1, [], [] );
	assert.equal( owner.quests( 1, [], [] ), retained, "unchanged quest inputs reuse eligibility" );
	const completed = owner.quests( 1, [], [ 2 ] );
	assert.notEqual( completed, retained );
	assert.ok( completed.some( r => r.id === 100002 ) );
	assert.notEqual( owner.quests( 2, [], [ 2 ] ), completed, "level changes invalidate eligibility" );
	owner.dispose();
	assert.deepEqual( owner.quests( 1, [], [] ), [] );
});
test("general guide menu carries the completed guild title with no unrenderable rows", async () => {
	const [guide, help] = await Promise.all(
		[ "data/event-guide-catalog.json", "text/texthelp.en.json" ].map( async p =>
			JSON.parse( await readFile( "../../.generated/client-public/assets/" + p, "utf8" ) )
		)
	);
	const articles = generalGuideArticles( guide, help.entries );
	const menu = articles.filter( a => a.depth === 0 );
	assert.equal( menu.find( a => a.id === 10000 ).title, "Guild system" );
	for ( const row of menu ) {
		assert.match( row.title, /[A-Za-z]/, "every general menu row stays renderable: " + row.id );
	}
});

test("native level, inventory and abnormal producers retain their discriminating gates", async () => {
	const { guideLevelEvents, guideInventoryEvents, guideAbnormalEvent, queueGuide } = await load(
		"src/engine/foundation/gameplay/guide.ts"
	);
	assert.deepEqual( guideLevelEvents( 18, 20 ), [ 8, 12, 13 ] );
	assert.deepEqual( guideLevelEvents( 19, 19 ), [] );
	assert.deepEqual( guideLevelEvents( undefined, 20 ), [] );
	const summons = new Map( [ [ 7, 0x9c6 ], [ 8, 0x11c6 ] ] ),
		items = [ { slot: 8, refObjId: 1, typeFlags: 0xbac }, { slot: 14, refObjId: 2, typeFlags: 0x1bac }, {
			slot: 15,
			refObjId: 7,
			typeFlags: 0x11ec
		}, { slot: 16, refObjId: 8, typeFlags: 0x11ec } ];
	assert.deepEqual( guideInventoryEvents( items, 13, summons ), [ 3, 14, 17, 18 ] );
	assert.deepEqual( guideInventoryEvents( [ { slot: 1, refObjId: 7, typeFlags: 0x91ec } ], 13, summons ), [] );
	assert.deepEqual( guideAbnormalEvent( new Uint8Array( 4 ) ), [] );
	assert.deepEqual( guideAbnormalEvent( Uint8Array.of( 1, 0, 0, 0, 0, 0, 0, 0, 0 ) ), [ 11 ] );
	assert.throws( () => guideAbnormalEvent( Uint8Array.of( 1, 0, 0, 0 ) ) );
	let state = queueGuide( guideBootstrap( { eventGuideStateMask: 0, localPlayerEntry: { countryByte9c: 0 } } ), [
		8,
		12,
		13,
		8
	] );
	assert.deepEqual( state.pending, [ 8, 12, 13 ] );
	state = revealGuide( state, 8 ).state;
	assert.deepEqual( queueGuide( state, [ 8, 13 ] ).pending, [ 12, 13 ] );
});

test("academy page and join are independent of persisted membership and clear only their matching requests", async () => {
	const { academyBootstrap, academyRequest, academyPacket } = await load(
		"src/engine/foundation/gameplay/academy.ts"
	);
	const original = academyBootstrap( { academyMember: false } ),
		page = academyRequest( original, { kind: "academy-page", page: 0 } );
	assert.deepEqual( [ ...page.frame.payload ], [ 0 ] );
	assert.equal( page.frame.opcode, 0x7701 );
	assert.equal( original.request, null );
	assert.throws( () => academyRequest( page.state, { kind: "academy-page", page: 1 } ) );
	const bytes = [ 1, 0, 1, 1 ],
		u32 = n => bytes.push( n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 ),
		str = ( s, wide ) => {
			bytes.push( s.length, 0 );
			for ( const c of s ) {
				bytes.push( c.charCodeAt( 0 ) );
				if ( wide ) bytes.push( 0 );
			}
		};
	u32( 7 );
	u32( 0 );
	bytes.push( 0 );
	str( "Welcome", true );
	u32( 4 );
	bytes.push( 60, 60 );
	u32( 1933 );
	str( "Guardian", false );
	u32( 2 );
	bytes.push( 3 );
	u32( 9 );
	u32( 0 );
	const frame = { opcode: 0xb701, payload: Uint8Array.from( bytes ) }, listed = academyPacket( page.state, frame );
	assert.equal( listed.request, null );
	assert.equal( listed.rows[0].name, "Guardian" );
	assert.equal( listed.rows[0].detail, "Welcome" );
	const join = academyRequest( listed, { kind: "academy-join", id: 7 }, 59 );
	assert.equal( join.frame.opcode, 0x7592 );
	assert.deepEqual( [ ...join.frame.payload ], [ 7, 0, 0, 0 ] );
	assert.equal( academyPacket( join.state, frame ).request.kind, "join" );
	assert.equal(
		academyPacket( join.state, { opcode: 0xb701, payload: Uint8Array.of( 2, 1 ) } ).request.kind,
		"join"
	);
	for ( const level of [ undefined, 0, 60, 255 ] ) {
		assert.throws( () => academyRequest( listed, { kind: "academy-join", id: 7 }, level ) );
	}
	const ack = academyPacket( join.state, { opcode: 0xb592, payload: Uint8Array.of( 1, 1 ) } );
	assert.equal( ack.request, null );
	assert.equal( ack.member, false, "join receipt cannot fabricate membership" );
	const seed = [
		10,
		1,
		7,
		0,
		0,
		0,
		...Array( 17 ).fill( 0 ),
		0,
		0,
		0,
		0,
		1,
		7,
		0,
		0,
		0,
		7,
		0,
		0,
		0,
		0,
		0,
		0,
		0,
		0,
		0,
		2,
		0,
		...Array( 16 ).fill( 0 ),
		39,
		39,
		...Array( 17 ).fill( 0 ),
		0,
		0
	];
	const admitted = academyPacket( ack, { opcode: 0x3ac5, payload: Uint8Array.from( seed ) } );
	assert.equal( admitted.member, true, "complete native roster seed commits membership" );
	assert.equal( admitted.localMemberId, 7 );
	assert.equal( admitted.members[0].kind, 2 );
	assert.equal( admitted.members[0].level, 39 );
	assert.equal( admitted.members[0].entryLevel, 39 );
	assert.throws( () => academyPacket( listed, { ...frame, payload: frame.payload.subarray( 0, -1 ) } ) );
	assert.throws( () => academyRequest( { ...listed, member: true }, { kind: "academy-join", id: 7 }, 39 ) );
});

test("anchored UI retains native sampling and drops retired character products", async () => {
	const { copyUi } = await load( "src/engine/foundation/ui/ui.ts" ),
		{ projectCharacterLabels } = await load( "src/engine/foundation/ui/character-labels.ts" );
	const source = {
			revision: 1,
			width: 800,
			height: 600,
			quads: [ {
				characterAnchor: 7,
				alphaCutoff: 128 / 255,
				uvTurn: 1,
				rect: [ -21.5, -27.5, 16, 16 ],
				clip: [ 0, 0, 800, 600 ],
				uv: [ 0, 0, 1, 1 ],
				texture: "native-icon",
				color: [ 1, 1, 1, 1 ]
			} ]
		},
		owned = copyUi( source );
	assert.equal( owned.quads[0].alphaCutoff, 128 / 255 );
	assert.equal( owned.quads[0].uvTurn, 1 );
	assert.deepEqual( projectCharacterLabels( owned, new Map( [ [ 7, [ 400.75, 300.75, .5 ] ] ] ) ).quads[0].rect, [
		378.5,
		272.5,
		16,
		16
	] );
	assert.equal( projectCharacterLabels( owned, new Map() ).quads.length, 0 );
	assert.equal( owned.quads[0].characterAnchor, 7 );
	assert.throws( () => copyUi( { ...source, quads: [ { ...source.quads[0], characterAnchor: -1 } ] } ) );
});
