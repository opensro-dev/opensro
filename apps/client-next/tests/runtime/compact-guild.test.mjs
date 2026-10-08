/*
===========================================================================

compact-guild.test.mjs - populated community pages retain native glyph metrics

Use the production UI and retail asset fixture. Exercise the existing social,
grant and war controls at portrait and short landscape sizes, then return to
the authored desktop layout.

===========================================================================
*/
import test from "node:test";
import assert from "node:assert/strict";
import { uiFixture, fontAtlas } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";
const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const { topmostControlAt } = await import( "../../src/engine/foundation/ui/hit-test.ts" );

/*
================
guildState
================
*/
function guildState() {
	const members = Array.from( { length: 18 }, ( _, i ) => ({
		id: i + 1,
		name: "Member" + String( i + 1 ).padStart( 2, "0" ),
		grade: i === 0 ? 0 : 1,
		level: 40 + i,
		permissions: 31,
		donated: i * 100,
		grant: "Officer",
		model: 1907,
		role: 1,
		offline: i % 2,
		warScore: i * 10
	}) );
	return {
		...emptySocial( members[0].name ),
		self: 1,
		allianceMaster: 7,
		guild: {
			id: 7,
			name: "Native Guild",
			level: 5,
			gp: 12345,
			subject: "Guild notice",
			contents: "Notice body",
			crest: 0,
			members
		},
		alliances: Array.from(
			{ length: 8 },
			( _, i ) => ({ id: 7 + i, name: "Ally" + i, level: 5, master: members[0].name, model: 1907, flags: 18 })
		),
		wars: Array.from(
			{ length: 12 },
			( _, i ) => ({
				id: 50 + i,
				enemyId: 100 + i,
				name: "Enemy" + i,
				type: 1,
				localScore: 10,
				enemyScore: 20,
				word38: 100,
				word3c: 3600,
				ending: false
			})
		)
	};
}

/*
================
nativeGlyphs
================
*/
function nativeGlyphs( fixture ) {
	const scene = defined( fixture.scenes.at( -1 ) ),
		glyphs = scene.quads.filter( quad => quad.texture === fontAtlas.image );
	assert.ok( glyphs.length > 0, "actual font ink was published" );
	for ( const glyph of glyphs ) {
		assert.ok( glyph.rect[2] >= glyph.uv[2] * fontAtlas.atlasWidth * .99, "native glyph width" );
		assert.ok( glyph.rect[3] >= glyph.uv[3] * fontAtlas.atlasHeight * .99, "native glyph height" );
	}
}

/*
================
contained
================
*/
function contained( output, width, height ) {
	for ( const control of output.controls ) {
		if ( !/^(guild-|social-|union-|war-|close$)/.test( control.id ) ) continue;
		const [x, y, w, h] = control.rect;
		assert.ok(
			w > 0 && h > 0 && x >= 0 && y >= 0 && x + w <= width + .01 && y + h <= height + .01,
			control.id + " has no hidden hit target beyond the viewport"
		);
	}
}

for ( const [width, height] of [ [ 375, 667 ], [ 667, 375 ] ] ) {
	test(`populated Guild pages keep native text and controls at ${width}x${height}`, () => {
		const sent = [], fixture = uiFixture( command => sent.push( command ) );
		let time = 0, published;
		/*
		================
		draw
		================
		*/
		function draw() {
			published = fixture.ui.step( fixture.state, ++time * 100 ) ?? published;
			assert.equal( fixture.ui.stats().error, null, "Guild draw must not retain an old product after an error" );
			const output = defined( published );
			contained( output, fixture.state.width, fixture.state.height );
			return output;
		}
		try {
			Object.assign( fixture.state.gameplay, { social: guildState() } );
			fixture.state.width = width;
			fixture.state.height = height;
			draw();
			fixture.ui.event( { kind: "activate", id: "open-window:Guild" } );
			let output = draw();
			nativeGlyphs( fixture );
			const tabs = output.controls.filter( control => control.id.startsWith( "guild-tab:" ) );
			assert.deepEqual( tabs.filter( control => !control.disabled ).map( control => control.id ), [
				"guild-tab:0",
				"guild-tab:1",
				"guild-tab:2",
				"guild-tab:4"
			] );
			assert.equal( new Set( tabs.map( control => control.rect[1] ) ).size, width < 477 ? 2 : 1 );
			const firstMembers = output.controls.filter( control => control.id.startsWith( "social-member:" ) ).map(
				control => control.id
			);
			assert.ok( firstMembers.length >= 3 );
			const member = defined( output.controls.find( control => control.id === firstMembers[1] ) );
			const hit = defined(
				topmostControlAt(
					output.controls,
					member.rect[0] + member.rect[2] / 2,
					member.rect[1] + member.rect[3] / 2
				)
			);
			assert.equal( hit.id, member.id, "member wins the DOM bridge hit order over guild-list" );
			fixture.ui.event( { kind: "activate", id: hit.id } );
			output = draw();
			assert.equal( output.controls.find( control => control.id === member.id )?.selected, true );
			fixture.ui.event( { kind: "activate", id: "social-next" } );
			output = draw();
			assert.notDeepEqual(
				output.controls.filter( control => control.id.startsWith( "social-member:" ) ).map( control =>
					control.id
				),
				firstMembers
			);
			fixture.ui.event( { kind: "activate", id: "guild-tab:1" } );
			output = draw();
			assert.equal( output.controls.filter( control => control.id.startsWith( "social-member:" ) ).length, 8 );
			nativeGlyphs( fixture );
			fixture.ui.event( { kind: "activate", id: "war-relation:1" } );
			output = draw();
			assert.equal( output.controls.filter( control => control.id.startsWith( "war-select:" ) ).length, 9 );
			const enemies = output.controls.filter( control => control.id.startsWith( "war-select:" ) ).map( control =>
				control.id
			);
			fixture.ui.event( { kind: "activate", id: "war-scroll-down" } );
			output = draw();
			assert.notDeepEqual(
				output.controls.filter( control => control.id.startsWith( "war-select:" ) ).map( control =>
					control.id
				),
				enemies
			);
			nativeGlyphs( fixture );
			fixture.ui.event( { kind: "activate", id: "guild-tab:2" } );
			output = draw();
			assert.equal( output.controls.filter( control => control.id.startsWith( "war-select:" ) ).length, 6 );
			assert.ok( output.controls.some( control => control.id === "war-members-down" ) );
			nativeGlyphs( fixture );
			fixture.ui.event( { kind: "activate", id: "guild-tab:4" } );
			output = draw();
			assert.ok(
				!output.controls.some( control => control.id === "guild-tab:0" ),
				"Blocking retains its existing separate route"
			);
			fixture.ui.event( { kind: "activate", id: "close" } );
			draw();
			fixture.state.width = 1600;
			fixture.state.height = 900;
			fixture.ui.event( { kind: "activate", id: "open-window:Guild" } );
			output = draw();
			fixture.ui.event( { kind: "activate", id: "guild-tab:0" } );
			output = draw();
			assert.equal( output.controls.filter( control => control.id.startsWith( "social-member:" ) ).length, 6 );
			assert.ok(
				!output.controls.some( control => control.id === "social-next" ),
				"native desktop branch restored"
			);
			nativeGlyphs( fixture );
			fixture.state.width = width;
			fixture.state.height = height;
			draw();
			fixture.ui.event( { kind: "activate", id: "guild-dialog:authority" } );
			output = draw();
			assert.deepEqual( fixture.ui.stats().windowMissing, [], "authority artwork must be admitted" );
			assert.equal(
				output.controls.filter( control => control.id.startsWith( "guild-grant:" ) ).length,
				25,
				"authority panel: " + JSON.stringify( fixture.ui.stats() )
			);
			nativeGlyphs( fixture );
			const checkedInk = defined( fixture.scenes.at( -1 ) ).quads.filter( quad =>
				quad.texture.endsWith( "/com_checkbutton_on.png" )
			).length;
			fixture.ui.event( { kind: "activate", id: "guild-grant:2:1" } );
			output = draw();
			assert.equal(
				output.controls.find( control =>
					control.id === "guild-grant:2:1"
				)?.selected,
				false
			);
			assert.equal(
				defined( fixture.scenes.at( -1 ) ).quads.filter( quad =>
					quad.texture.endsWith( "/com_checkbutton_on.png" )
				).length,
				checkedInk - 1,
				"toggling a right removes its painted check mark"
			);
			if ( width > height ) {
				fixture.state.height = 320;
				output = draw();
				assert.equal(
					output.controls.filter( control => control.id.startsWith( "guild-grant:" ) ).length,
					15,
					"shorter landscape admits three complete authority rows"
				);
			}
			const visited = new Set();
			for ( let page = 0; page < 18; page++ ) {
				const pager = defined( output.controls.find( control => control.id === "guild-grant-next" ) );
				const confirm = defined( output.controls.find( control => control.id === "guild-grant-ok" ) );
				assert.ok( pager.rect[1] + pager.rect[3] <= confirm.rect[1], "pager does not overlap confirmation" );
				for ( const control of output.controls.filter( control => control.id.startsWith( "guild-grant:" ) ) ) {
					visited.add( Number( control.id.split( ":" )[1] ) );
					assert.ok( control.rect[1] + control.rect[3] < pager.rect[1], "rights stay above pager and OK" );
				}
				if ( pager.disabled ) break;
				fixture.ui.event( { kind: "activate", id: "guild-grant-next" } );
				output = draw();
			}
			assert.deepEqual(
				[ ...visited ].sort( ( a, b ) => a - b ),
				Array.from( { length: 17 }, ( _, i ) => i + 2 ),
				"all non-master members are reachable; native policy excludes the master"
			);
			fixture.ui.event( { kind: "activate", id: "guild-grant:18:1" } );
			output = draw();
			nativeGlyphs( fixture );
			for ( let page = 0; page < 18; page++ ) {
				if ( output.controls.find( control => control.id === "guild-grant-prev" )?.disabled ) break;
				fixture.ui.event( { kind: "activate", id: "guild-grant-prev" } );
				output = draw();
			}
			assert.equal(
				output.controls.find( control => control.id === "guild-grant:2:1" )?.selected,
				false,
				"earlier edits survive paging away and back"
			);
			fixture.ui.event( { kind: "activate", id: "guild-grant-ok" } );
			assert.deepEqual( sent.at( -1 )?.command, {
				kind: "guild-permissions",
				grants: [ { id: 2, permissions: 30 }, { id: 18, permissions: 30 } ]
			} );
		} finally {
			fixture.dispose();
		}
	});
}
