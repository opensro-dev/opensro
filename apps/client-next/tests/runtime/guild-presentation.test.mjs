/*
===========================================================================

guild-presentation.test.mjs - Guild empty, selected and authority regressions

Exercise production UI paint and hit order with retail resources. The native
relation rows remain present without data; notices replace populated details.

===========================================================================
*/
import test from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const { topmostControlAt } = await import( "../../src/engine/foundation/ui/hit-test.ts" );

/*
================
guildFixture
================
*/
function guildFixture() {
	const sent = [], f = uiFixture( c => sent.push( c.command ) );
	const social = {
		...emptySocial( "Player" ),
		self: 1,
		guild: {
			id: 7,
			name: "Guild",
			level: 5,
			gp: 50,
			subject: "Notice",
			contents: "Body",
			crest: 0,
			members: Array.from( { length: 12 }, ( _, i ) => ({
				id: i + 1,
				name: i === 0 ? "Player" : "Member" + i,
				grade: i === 0 ? 0 : 1,
				level: 40,
				permissions: 31,
				grant: "",
				donated: 0,
				model: 1907,
				role: 0,
				offline: 0
			}) )
		}
	};
	Object.assign( f.state.gameplay, { social } );
	let clock = 0, output;
	/*
	================
	draw
	================
	*/
	function draw() {
		output = f.ui.step( f.state, clock += 100 ) ?? output;
		return output;
	}
	/*
	================
	activate
	================
	*/
	function activate( id ) {
		f.ui.event( { kind: "activate", id } );
		return draw();
	}
	draw();
	activate( "open-window:Guild" );
	return { f, social, sent, draw, activate };
}

test("empty relation rows are skinned and notices replace selected detail labels", () => {
	const { f, social, activate, draw } = guildFixture();
	try {
		activate( "guild-tab:1" );
		for ( const relation of [ 0, 1 ] ) {
			activate( "war-relation:" + relation );
			const bars = f.scenes.at( -1 ).quads.filter( q => q.texture.endsWith( "/com_bar01_left.png" ) );
			assert.equal( bars.length, 9, "nine authored row skins remain with no guilds" );
			assert.equal( f.hasText( "Guildmaster" ), false, "empty details do not paint populated labels" );
		}
		social.allianceMaster = 7;
		social.alliances = [ { id: 7, name: "Ally", master: "Player", level: 5, model: 1907, flags: 12 } ];
		activate( "war-relation:0" );
		activate( "social-member:7" );
		assert.equal( f.hasText( "Not in the alliance" ), false );
		assert.equal( f.hasText( "No guild is selected" ), false );
		assert.equal(
			f.scenes.at( -1 ).quads.filter( q => q.texture.endsWith( "/com_bar01select_left.png" ) ).length,
			1
		);
		social.alliances = [];
		draw();
		assert.equal(
			f.scenes.at( -1 ).quads.filter( q => q.texture.endsWith( "/com_bar01select_left.png" ) ).length,
			0
		);
		social.wars = [ {
			id: 77,
			enemyId: 8,
			name: "Enemy",
			type: 1,
			localScore: 10,
			enemyScore: 20,
			word38: 0,
			word3c: 3600
		} ];
		activate( "war-relation:1" );
		activate( "war-select:77" );
		assert.equal( f.hasText( "No guild is selected" ), false );
		assert.equal(
			f.scenes.at( -1 ).quads.filter( q => q.texture.endsWith( "/com_bar01select_left.png" ) ).length,
			1
		);
		activate( "guild-tab:2" );
		assert.equal( f.hasText( "There is no hostile guild" ), false );
		social.wars = [];
		draw();
		assert.equal( f.hasText( "Total guild acquisition point" ), false, "empty combat data hides score fields" );
	} finally {
		f.dispose();
	}
});

test("desktop member hits win over the list and authority marks survive cancel/reopen", () => {
	const { f, social, sent, activate, draw } = guildFixture();
	try {
		let output = draw();
		const row = output.controls.find( c => c.id === "social-member:2" );
		assert.ok( row );
		const hit = topmostControlAt( output.controls, row.rect[0] + 100, row.rect[1] + 10 );
		assert.ok( hit );
		assert.equal( hit?.id, row.id );
		activate( hit.id );
		assert.ok( f.scenes.at( -1 ).quads.some( q => q.texture.endsWith( "/gil_bar02_select.png" ) ) );
		output = activate( "guild-dialog:authority" );
		assert.deepEqual( f.ui.stats().windowMissing, [] );
		assert.equal(
			f.scenes.at( -1 ).quads.filter( q => q.texture.endsWith( "/com_checkbutton_on.png" ) ).length,
			25
		);
		const box = output.controls.find( c => c.id === "guild-grant:2:1" );
		assert.ok( box );
		assert.equal( topmostControlAt( output.controls, box.rect[0] + 4, box.rect[1] + 4 )?.id, box.id );
		activate( box.id );
		activate( "guild-grant-cancel" );
		assert.equal( sent.length, 0 );
		output = activate( "guild-dialog:authority" );
		assert.equal( output.controls.find( c => c.id === box.id )?.selected, true );
		activate( box.id );
		activate( "guild-grant-ok" );
		assert.deepEqual( sent.pop(), { kind: "guild-permissions", grants: [ { id: 2, permissions: 30 } ] } );
		social.self = 2;
		social.localName = "Member1";
		output = activate( "guild-dialog:authority" );
		assert.equal( output.controls.find( c => c.id === "guild-grant-ok" )?.disabled, true );
		assert.ok( output.controls.filter( c => c.id.startsWith( "guild-grant:" ) ).every( c => c.disabled ) );
	} finally {
		f.dispose();
	}
});
