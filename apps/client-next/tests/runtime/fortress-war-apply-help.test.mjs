/*
===========================================================================

fortress-war-apply-help.test.mjs - the war application window's help texts

660930 (CIFFortressWarApply_OnCreate) sets four style-0x80 help texts on
the window: the info area (502) and the three column heads (511..513)
carry the TP explanations, the way the tax window's 19..21 do (664DA0).

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const fortress = await import( "../../src/engine/foundation/gameplay/fortress.ts" );
const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const TEXT_FILE = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/text/textuisystem.en.json", "utf8" ) );
const TEXT = TEXT_FILE.entries ?? TEXT_FILE;

const MANAGER = 23;
const SELF = 5;

test("660930's four help texts ride the application window", t => {
	const f = uiFixture( () => {} );
	t.after( () => f.dispose() );
	f.state.entities.push( { ...f.state.entities[0], gid: MANAGER, refObjId: 2100, kind: "npc", name: "Manager" } );
	Object.assign( f.state.gameplay, {
		target: MANAGER,
		targetCapabilities: 0x800000,
		npcConversation: { phase: "menu", gid: MANAGER },
		fortress: {
			...fortress.fortressBootstrap( {
				gameWorldData: [ { gameWorldId: 7, warName: "FORTRESS_JANGAN" } ],
				siegeFortressData: [ { fortressId: 1, codeName: "FORTRESS_JANGAN", nameStrId: "SN_FORTRESS_JANGAN" } ]
			} ),
			worldId: 7,
			wars: [ { id: 1, name: "Holders", flags: 0 } ]
		},
		social: {
			...emptySocial( "Player" ),
			self: SELF,
			guild: {
				id: 9,
				name: "Holders",
				level: 3,
				gp: 0,
				subject: "",
				contents: "",
				crest: 0,
				members: [ { id: SELF, name: "Player", grade: 0, role: 1, level: 60, permissions: 31 } ]
			}
		}
	} );
	let now = 0, last = null;
	const step = () => {
		for ( let i = 0; i < 16; i++ ) last = f.ui.step( f.state, now += 100 ) ?? last;
		return last;
	};
	step();
	f.ui.event( { kind: "activate", id: "npc-fortress-war" } );
	// The official's answer (a new application sequence) opens the window.
	f.state.gameplay.fortressApplication = { warStart: null, applied: null, sequence: 1 };
	const shown = step();
	for (
		const [id, key] of [
			[ 502, "UIIT_MSG_TP_FORT_WARAPPLY_INFO" ],
			[ 511, "UIIT_MSG_TP_FORT_WARAPPLY_FORTRESS" ],
			[ 512, "UIIT_MSG_TP_FORT_WARAPPLY_GUILD" ],
			[ 513, "UIIT_MSG_TP_FORT_WARAPPLY_APPLICATION" ]
		]
	) {
		const help = shown.controls.find( c => c.id === "fortress-war-help:" + id );
		assert.ok( help, "no help region for control " + id );
		assert.equal( help.helpText, TEXT[key], "wrong help text for control " + id );
	}
});
