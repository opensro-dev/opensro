/*
===========================================================================

press-admission-walk.test.mjs - refused presses preserve a client-led walk

Server admission runs before movement changes. Test every ordinary skill
command path while its answer is in flight, with an admitted stop control.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { product } from "../helpers/navigation-fixture.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const START = { regionId: 257, x: 100, y: 0, z: 100, angle: 0 };
/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
const LOCAL = { ...START, gid: 7, refObjId: 1907, kind: "local-player", name: "walker", heading: 0 };
/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
const TARGET = { ...LOCAL, gid: 9, kind: "monster", x: 130 };

for ( const path of [ "targeted", "self", "untargeted" ] ) {
	for ( const gate of [ "admitted", "wrong-weapon", "missing", "unknown-flat-mp", "unknown-percent-mp" ] ) {
		test(`${path} press ${gate} only holds the walk when admission is known`, () => {
			const sent = [], game = createGameplay( frame => sent.push( frame ) );
			const none = { ID: 0, Level: 0 };
			game.bootstrap( {
				simulationProtocolVersion: 1,
				character: { skills: [ 30 ] },
				refSkillSnapshot: [ {
					id: 30,
					group: 30,
					level: 1,
					status: false,
					effectRider: false,
					ui: {
						name: "SKILL_30",
						trainable: true,
						spCost: 1,
						targetRequired: path !== "untargeted",
						targetSelf: path === "self",
						targets: path === "targeted" ? 6 : 1,
						range: 60,
						haltsWalk: true,
						actionMs: 1000,
						cooldownMs: 5000,
						mp: gate === "unknown-flat-mp" ? 120 : 0,
						mpPercent: gate === "unknown-percent-mp" ? 10 : 0,
						admit: gate === "missing" ? undefined : {
							weaponKinds: gate === "wrong-weapon" ? [ 2, 2 ] : [ 255, 255 ]
						},
						masteries: [ none, none ],
						prerequisites: [ none, none, none ]
					}
				} ]
			} );
			game.seed( LOCAL );
			const navigation = product();
			navigation.objects = [];
			game.command( { kind: "navigation", regionId: 257, bundle: navigation }, 0, undefined );
			game.command( { kind: "move", destination: { ...START, x: 900 } }, 0, undefined, LOCAL );
			game.step( 100, LOCAL );
			assert.equal( game.take()?.pose?.x, 105 );
			game.command(
				{ kind: "skill", skillId: 30, ...(path === "targeted" ? { gid: 9 } : {}) },
				100,
				path === "targeted" ? TARGET : undefined,
				LOCAL
			);
			assert.equal( sent.at( -1 )?.opcode, 0x72cd, "even uncertain or refused presses reach the server" );
			game.step( 200, LOCAL );
			const pending = game.take(), admitted = gate === "admitted";
			assert.equal( pending?.pose?.x, admitted ? 105 : 110, "no latency-length stop for a refused press" );
			assert.equal( !!pending?.castPrediction, admitted );
			assert.equal(
				pending?.skillCooldowns?.some( row => row.provisionalUntilMs !== undefined ) ?? false,
				admitted
			);
			if ( !admitted ) {
				game.receive( { opcode: 0xb245, payload: Uint8Array.of( 2, 4 ) }, 210 );
				game.step( 300, LOCAL );
				assert.equal( game.take()?.pose?.x, 115, "the refusal preserves uninterrupted travel" );
			}
			game.dispose();
		});
	}
}
