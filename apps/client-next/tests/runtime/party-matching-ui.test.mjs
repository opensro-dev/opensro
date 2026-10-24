/*
===========================================================================

party-matching-ui.test.mjs - current party order and color in the production HUD.

Leaders and members share the pinned row without sharing listing ownership.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { fontAtlas, uiFixture } from "../helpers/ui-fixture.mjs";

for ( const leader of [ true, false ] ) {
	test(`matching highlights the current party for ${leader ? "leaders" : "members"} on every page`, t => {
		const f = uiFixture();
		t.after( () => f.dispose() );
		const currentName = leader ? "Player" : "Leader";
		const current = {
			id: 99,
			party: 0,
			name: currentName,
			race: 0,
			members: 2,
			type: 3,
			purpose: 0,
			min: 1,
			max: 90,
			title: "Our party"
		};
		Object.assign( f.state.gameplay, {
			social: {
				localName: "Player",
				self: 1,
				leader: leader ? 1 : 2,
				options: 3,
				members: [ { id: 1, name: "Player" }, { id: 2, name: "Leader" } ]
			},
			partyMatching: {
				auto: [],
				page: 1,
				pages: 3,
				rows: [],
				own: leader ? current : null,
				request: null,
				pending: null,
				result: null
			}
		} );
		let now = 0, frame;
		/*
		================
		draw
		================
		*/
		function draw() {
			for ( let i = 0; i < 20; i++ ) frame = f.ui.step( f.state, now += 100 ) ?? frame;
			assert.ok( frame );
			return frame;
		}
		draw();
		f.ui.event( { kind: "activate", id: "open-window:Party Matching" } );
		for ( let page = 1; page <= 3; page++ ) {
			f.state.gameplay = {
				...f.state.gameplay,
				partyMatching: {
					...f.state.gameplay.partyMatching,
					page,
					rows: [ { ...current, id: page, name: "Other" }, current, {
						...current,
						id: page + 10,
						name: "Another"
					} ]
				}
			};
			for ( const sort of [ 60, 60, 63, 63 ] ) {
				f.ui.event( { kind: "activate", id: "party-match:" + sort } );
				const frame = draw();
				const rows = frame.controls.filter( control => control.id.startsWith( "party-match-row:" ) );
				assert.equal( rows[0]?.id, "party-match-row:99" );
				assert.equal( rows.length, 3 );
				for ( const row of rows ) {
					const inside = f.scenes.at( -1 ).quads.filter( quad =>
							quad.rect[0] >= row.rect[0] && quad.rect[0] < row.rect[0] + row.rect[2] &&
							quad.rect[1] >= row.rect[1] && quad.rect[1] < row.rect[1] + row.rect[3]
						),
						glyphs = inside.filter( quad => quad.texture === fontAtlas.image ),
						icons = inside.filter( quad => quad.texture.includes( "/interface/party/pt_" ) );
					assert.ok( glyphs.length > 0 );
					assert.ok( icons.length > 0 );
					// The current party's text and type icon share the native tint.
					for ( const quad of [ ...glyphs, ...icons ] ) {
						assert.deepEqual(
							quad.color,
							row.id === "party-match-row:99" ? [ 135 / 255, 233 / 255, 1, 1 ] : [ 1, 1, 1, 1 ]
						);
					}
					// The background looks the same on every row.
					assert.ok( !inside.some( quad => quad.texture === "" ) );
				}
				assert.equal( !!frame.controls.find( control => control.id === "party-match:19" )?.disabled, !leader );
				assert.equal( !!frame.controls.find( control => control.id === "party-match:20" )?.disabled, !leader );
			}
		}
		if ( !leader ) {
			f.state.gameplay = { ...f.state.gameplay, social: { ...f.state.gameplay.social, leader: 0, members: [] } };
			f.ui.event( { kind: "activate", id: "party-match:60" } );
			const rows = draw().controls.filter( control => control.id.startsWith( "party-match-row:" ) );
			assert.equal( rows[0].id, "party-match-row:3" );
		}
	});
}
