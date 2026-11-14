/*
===========================================================================

contributor-titles.test.mjs - the Contributor title: roster, nameplate, chat

Port-only, not native (#3358): under SRO_CONTRIBUTOR_TITLES the server
pushes the shown roster (0x3FC1); a rostered player carries a yellow
Contributor row over the nameplate and a gold-orange tag in chat. No
roster (the flag off) draws exactly the native stack and plain lines.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";

const { createContributorTitles, OP_CONTRIBUTOR_TITLES, OP_CONTRIBUTOR_PROMPT } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/contributor-titles/contributor-titles.ts"
);
const { overheadLayout, withoutBoardPass } = await import(
	"../../src/engine/foundation/ui/overhead-layout.ts"
);
const { chatLayout } = await import( "../../src/engine/foundation/ui/chat-layout.ts" );
const { decodeAuthoredLayout } = await import( "../../src/engine/foundation/ui/authored-layout.ts" );
const { CONTRIBUTOR_TAG, CONTRIBUTOR_TAG_COLOR } = await import(
	"../../src/engine/foundation/ui/chat-layout.ts"
);
const { chatLineColor } = await import( "../../src/engine/foundation/ui/chat-presentation.ts" );
const { defaultGameOptions } = await import( "../../src/engine/foundation/gameplay/game-options.ts" );
const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );

const options = defaultGameOptions();

/*
================
rosterBody
================
*/
function rosterBody( names ) {
	let length = 2;
	const encoded = names.map( name => {
		length += 2 + name.length;
		return name;
	} );
	const payload = new Uint8Array( length ), view = new DataView( payload.buffer );
	view.setUint16( 0, encoded.length, true );
	let offset = 2;
	for ( const name of encoded ) {
		view.setUint16( offset, name.length, true );
		offset += 2;
		for ( let i = 0; i < name.length; i++ ) payload[offset++] = name.charCodeAt( i );
	}
	return payload;
}

test("the roster frame decodes its names and replaces wholesale", () => {
	const titles = createContributorTitles();
	assert.deepEqual( titles.names(), [] );
	assert.equal( titles.receive( { opcode: 0x3015, payload: rosterBody( [ "Hero" ] ) } ), false );
	assert.equal(
		titles.receive( { opcode: OP_CONTRIBUTOR_TITLES, payload: rosterBody( [ "Hero", "Sage" ] ) } ),
		true
	);
	assert.deepEqual( titles.names(), [ "Hero", "Sage" ] );
	// The next roster replaces the last, never extends it.
	assert.equal( titles.receive( { opcode: OP_CONTRIBUTOR_TITLES, payload: rosterBody( [ "Sage" ] ) } ), true );
	assert.deepEqual( titles.names(), [ "Sage" ] );
	// An empty roster clears every title (the flag turned off, the
	// last contributor logged out or hid theirs).
	assert.equal( titles.receive( { opcode: OP_CONTRIBUTOR_TITLES, payload: rosterBody( [] ) } ), true );
	assert.deepEqual( titles.names(), [] );
	titles.receive( { opcode: OP_CONTRIBUTOR_TITLES, payload: rosterBody( [ "Hero" ] ) } );
	titles.clear();
	assert.deepEqual( titles.names(), [], "world exit clears the roster" );
});

test("a malformed roster is a protocol error and keeps the last one", () => {
	const titles = createContributorTitles();
	titles.receive( { opcode: OP_CONTRIBUTOR_TITLES, payload: rosterBody( [ "Hero" ] ) } );
	const bad = [
		new Uint8Array( 0 ),
		new Uint8Array( 1 ),
		// Count two, one name present.
		rosterBody( [ "Hero" ] ).map( ( b, i ) => i === 0 ? 2 : b ),
		// A name length running past the payload.
		new Uint8Array( [ 1, 0, 9, 0, 65 ] ),
		// Trailing bytes after the last name.
		new Uint8Array( [ ...rosterBody( [ "Hero" ] ), 0 ] )
	];
	for ( const payload of bad ) {
		assert.throws( () => titles.receive( { opcode: OP_CONTRIBUTOR_TITLES, payload } ), /Invalid contributor/ );
	}
	assert.deepEqual( titles.names(), [ "Hero" ] );
});

const local = { gid: 1, refObjId: 1907, kind: "local-player", name: "Me", heading: 0, regionId: 257, x: 0, y: 0, z: 0 };
const hero = { ...local, gid: 5, kind: "player", name: "Hero", guildName: "Guild" };

/*
================
layoutFor

The layout reads the whole GameplayState type; the fixtures below carry
only the fields the overhead rows consult.
================
*/
function layoutFor( entity, game ) {
	return overheadLayout( entity, local, /** @type {any} */ (game), options );
}

test("a rostered player carries the yellow title above the guild row", () => {
	const game = { localGid: 1, social: emptySocial( "Me" ), vitals: [], contributorTitles: [ "Hero" ] };
	const layout = layoutFor( hero, game );
	assert.equal( layout.contributorText, "Contributor" );
	assert.deepEqual( layout.contributorColor, [ 1, 1, 0, 1 ] );
	// The guild row rides at -20; the title sits one row above it.
	assert.equal( layout.guildY, -20 );
	assert.equal( layout.contributorY, -40 );
	// The local player's own title shows too.
	const own = layoutFor( local, { ...game, contributorTitles: [ "Me" ] } );
	assert.equal( own.contributorText, "Contributor" );
	assert.equal( own.contributorY, -20 );
});

test("no roster, no title: the native stack is untouched", () => {
	const bare = { localGid: 1, social: emptySocial( "Me" ), vitals: [] };
	for ( const game of [ bare, { ...bare, contributorTitles: [] }, { ...bare, contributorTitles: [ "Sage" ] } ] ) {
		const layout = layoutFor( hero, game );
		assert.equal( layout.contributorText, "" );
		assert.equal( layout.contributorY, 0 );
		assert.equal( layout.guildY, -20 );
	}
	// Only a player carries the title, whatever the roster names.
	const monster = { ...hero, kind: "monster" };
	assert.equal(
		layoutFor( monster, { ...bare, contributorTitles: [ "Hero" ] } ).contributorText,
		""
	);
	// The /id-off pass hides it with the rest of the kind-4 rows.
	const layout = layoutFor( hero, { ...bare, contributorTitles: [ "Hero" ] } );
	const stripped = withoutBoardPass( layout );
	assert.equal( stripped.contributorText, "" );
	assert.equal( stripped.contributorY, 0 );
});

const chatFixtureLayout = decodeAuthoredLayout(
	JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifchatviewer.json", "utf8" ) )
);

/*
================
chatDraws

Run the chat layout over lines with a recording text service; the
drawn runs come back in order.
================
*/
function chatDraws( lines, contributorTitles ) {
	const draws = [];
	chatLayout( {
		layout: chatFixtureLayout,
		width: 1024,
		height: 768,
		rows: 2,
		tab: 0,
		input: "",
		lines,
		contributorTitles,
		welcome: "",
		copy: key => key,
		size: () => [ 16, 16 ],
		text: ( value, rect, clip, color, style ) => {
			draws.push( { value, rect, color, style } );
			return [];
		},
		hover: null,
		pressed: null,
		measure: value => value.length * 7
	} );
	return draws;
}

test("a rostered sender is tagged in gold-orange on global and normal chat alike", () => {
	const lines = [
		{ channel: 6, name: "Hero", text: "hello all", outgoing: false },
		{ channel: 1, name: "Hero", text: "hello near", outgoing: false },
		{ channel: 1, name: "Sage", text: "untagged", outgoing: false },
		{ channel: 7, name: "Hero", text: "a notice", outgoing: false }
	];
	const draws = chatDraws( lines, [ "Hero" ] );
	const tags = draws.filter( d => d.value === CONTRIBUTOR_TAG );
	assert.equal( tags.length, 2, "one tag per rostered sender line, never on a notice" );
	assert.equal( CONTRIBUTOR_TAG_COLOR, 0xffb000 );
	for ( const tag of tags ) {
		assert.deepEqual( tag.color, [ 1, 0xb0 / 255, 0, 1 ] );
		assert.deepEqual( tag.style, { fontStyle: 2 } );
	}
	// The rest of each tagged line keeps its channel colour: yellow on
	// global (6), white nearby (1) - the tag is what stands out.
	const rests = draws.filter( d => d.value.startsWith( "Hero:" ) );
	assert.equal( rests.length, 2 );
	assert.deepEqual( rests[0].color, chatLineColor( 6 ) );
	assert.deepEqual( rests[1].color, chatLineColor( 1 ) );
	assert.equal( rests[0].rect[0], tags[0].rect[0] + CONTRIBUTOR_TAG.length * 7 );
	// The untagged sender draws as one plain run.
	const plain = draws.filter( d => d.value.startsWith( "Sage:" ) );
	assert.equal( plain.length, 1 );
	assert.deepEqual( plain[0].color, chatLineColor( 1 ) );
	// Without a roster nothing is tagged, on any channel.
	const bare = chatDraws( lines, undefined );
	assert.equal( bare.filter( d => d.value === CONTRIBUTOR_TAG ).length, 0 );
	assert.equal( bare.filter( d => d.value.startsWith( "Hero:" ) ).length, 2 );
});

test("the login prompt raises one dialog per frame, answering with the contributor commands", () => {
	const titles = createContributorTitles();
	assert.equal( titles.receive( { opcode: OP_CONTRIBUTOR_PROMPT, payload: Uint8Array.of( 1 ) } ), true );
	const elsewhere = titles.takePrompt();
	assert.deepEqual( elsewhere?.dialog?.answers, {
		confirmLabel: "UIIT_PORT_CONTRIBUTOR_MOVE",
		confirmChat: "/contributor here",
		cancelLabel: "UIIT_PORT_CONTRIBUTOR_KEEP",
		cancelChat: "/contributor keep"
	} );
	// Taken once: the next look finds nothing until the server asks again.
	assert.equal( titles.takePrompt(), undefined );
	titles.receive( { opcode: OP_CONTRIBUTOR_PROMPT, payload: Uint8Array.of( 2 ) } );
	const holder = titles.takePrompt();
	assert.deepEqual( holder?.dialog?.answers, { confirmLabel: "UIIT_CTL_CONFIRM", confirmChat: "/contributor keep" } );
	for ( const payload of [ Uint8Array.of(), Uint8Array.of( 3 ), Uint8Array.of( 1, 1 ) ] ) {
		assert.throws( () => titles.receive( { opcode: OP_CONTRIBUTOR_PROMPT, payload } ) );
	}
	// A roster frame never raises the prompt.
	titles.receive( { opcode: OP_CONTRIBUTOR_TITLES, payload: Uint8Array.of( 0, 0 ) } );
	assert.equal( titles.takePrompt(), undefined );
});
