/*
===========================================================================

npc-reopen-live.test.mjs - repeat NPC interaction through a real session

Records the command, published selection, conversation and DOM boundaries.
Uses the normal runtime and authenticated scratch actor without rewriting
served source or server replies.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { resolveProbeCredentials, resolveProbeDivisionId } from "../../../../scripts/lib/probeSession.mjs";
import { resolveProbeCharacter } from "../../../../scripts/lib/probeCharacter.mjs";
import { decodeProbeAlphaFrame } from "../../../../scripts/lib/probeTransportProtocol.mjs";
import { bindPlayableRuntime, waitPlayableWorld } from "./helpers/playable-session.mjs";

const RESPONSE_TIMEOUT_MS = 10000;
const REGION_SIZE = 1920;
const SINGLE_ACTOR_DOCK_POINT = { x: 505, y: 430 };
const NPC_OPCODES = new Set( [ 0x745a, 0xb45a, 0x74b3, 0xb4b3, 0x7338, 0xb338, 0x3773, 0x3230 ] );

test(
	"live repeated NPC selection preserves an open conversation and permits reopening",
	{ timeout: 180000 },
	async () => {
		const character = resolveProbeCharacter( { context: "NPC reopen regression" } );
		const directory = process.env.SRO_NPC_REOPEN_ARTIFACTS ?? "temp/artifacts/npc-reopen";
		await mkdir( directory, { recursive: true } );
		const { browser, page } = await launchProbeBrowser();
		/** @type {{ direction: string, opcode: number, payload: string }[]} */
		const wire = [];
		let tracing = false;
		page.on( "websocket", socket => {
			/*
			================
			record

			Keep only NPC protocol frames; authentication and world-entry data are excluded.
			================
			*/
			function record( direction, event ) {
				if ( !Buffer.isBuffer( event.payload ) || event.payload.length < 2 ) return;
				const frame = decodeProbeAlphaFrame( event.payload );
				if ( NPC_OPCODES.has( frame.opcode ) ) {
					wire.push( {
						direction,
						opcode: frame.opcode,
						payload: Buffer.from( frame.payload ).toString( "hex" )
					} );
				}
			}
			socket.on( "framesent", event => record( "sent", event ) );
			socket.on( "framereceived", event => record( "received", event ) );
		} );
		/** @type {{ character: string, samples: object[], errors: string[], verdict: string, target?: object, failure?: string }} */
		const evidence = { character, samples: [], errors: [], verdict: "FAIL" };
		page.on( "pageerror", error => evidence.errors.push( String( error ) ) );
		/*
	================
	capture
	================
	*/
		async function capture( phase ) {
			const state = await page.evaluate( () => {
				const game = globalThis.__playableRuntime?.gameplay();
				return {
					session: globalThis.__playableRuntime?.sessionState()?.phase,
					diagnostics: document.querySelector( "output" )?.textContent,
					target: game?.target,
					pending: game?.targetPending,
					conversation: game?.npcConversation,
					controls: [ ...document.querySelectorAll( '[data-ui-id^="npc-"]' ) ].map( node =>
						node.getAttribute( "data-ui-id" )
					)
				};
			} );
			evidence.samples.push( { phase, ...state } );
			return state;
		}
		try {
			console.log( "[npc-reopen] authenticated scratch-session boot" );
			await page.setViewportSize( { width: 1024, height: 768 } );
			await page.goto( CLIENT_NEXT_BASE_URL );
			await bindPlayableRuntime( page );
			await page.waitForFunction( () => __playableRuntime.sessionState()?.phase === "signed-out" );
			await page.locator( '[data-ui-id="frontend:reveal"]' ).click( { timeout: 60000 } );
			await page.locator( '[data-ui-id="login"]' ).waitFor( { timeout: 60000 } );
			const credentials = resolveProbeCredentials();
			await page.evaluate( ( { id, password, serverId } ) =>
				__playableRuntime.session( {
					kind: "login",
					apiBase: location.origin + "/api",
					id,
					password,
					serverId
				} ), {
				id: credentials.loginId,
				password: credentials.loginPassword,
				serverId: resolveProbeDivisionId()
			} );
			await page.waitForFunction( () => __playableRuntime.sessionState()?.phase === "character-select" );
			await page.locator( '[data-ui-id="frontend:create"]' ).waitFor( { timeout: 60000 } );
			// The dock owns the selected actor and admits entry only after its camera settles.
			assert.deepEqual(
				await page.evaluate( () => __playableRuntime.sessionState().characters.map( row => row.name ) ),
				[ character ],
				"This probe requires a dedicated single-character scratch roster"
			);
			await page.mouse.click( SINGLE_ACTOR_DOCK_POINT.x, SINGLE_ACTOR_DOCK_POINT.y );
			const enter = page.locator( '[data-ui-id="enter"]' );
			await enter.waitFor();
			await page.waitForFunction( () => document.querySelector( '[data-ui-id="enter"]' )?.matches( ":enabled" ) );
			await enter.click();
			await waitPlayableWorld( page, character );
			const destination = process.env.SRO_NPC_REOPEN_POSITION;
			if ( destination ) {
				await page.waitForFunction( () =>
					document.querySelector( "output" )?.textContent?.includes( "Navigation: ready" )
				);
				console.log( "[npc-reopen] walk scratch actor to the requested NPC fixture" );
				await page.evaluate( destination =>
					__playableRuntime.session( {
						kind: "gameplay",
						command: { kind: "move", destination }
					} ), JSON.parse( destination ) );
			}
			await page.waitForFunction( () => __playableRuntime.entities().some( entity => entity.kind === "npc" ) );
			const target = await page.evaluate( regionSize => {
				const pose = __playableRuntime.gameplay().pose;
				return __playableRuntime.entities().filter( entity => entity.kind === "npc" ).map( entity => ({
					gid: entity.gid,
					name: entity.name,
					distance: Math.hypot(
						entity.x + ((entity.regionId & 255) - (pose.regionId & 255)) * regionSize - pose.x,
						entity.z + ((entity.regionId >>> 8) - (pose.regionId >>> 8)) * regionSize - pose.z
					)
				}) ).sort( ( a, b ) => a.distance - b.distance )[0];
			}, REGION_SIZE );
			assert.ok( target );
			evidence.target = target;
			await page.context().tracing.start( { screenshots: true, snapshots: true } );
			tracing = true;
			/*
		================
		select
		================
		*/
			async function select() {
				await page.evaluate(
					gid => __playableRuntime.session( { kind: "gameplay", command: { kind: "select", gid } } ),
					target.gid
				);
			}
			console.log( "[npc-reopen] select", target.name );
			await select();
			await page.locator( '[data-ui-id="npc-close"]' ).waitFor( { timeout: RESPONSE_TIMEOUT_MS } );
			await capture( "first-open" );
			console.log( "[npc-reopen] repeat selection without a target change" );
			const tick = await page.evaluate( () =>
				Number( /Simulation tick: (\d+)/.exec( document.querySelector( "output" )?.textContent ?? "" )?.[1] )
			);
			await select();
			await page.waitForFunction( () => !__playableRuntime.gameplay().targetPending );
			// Cross the worker publication boundary before inspecting a coalesced command.
			await page.waitForFunction(
				tick =>
					Number(
						/Simulation tick: (\d+)/.exec( document.querySelector( "output" )?.textContent ?? "" )?.[1]
					) >= tick + 2,
				tick
			);
			const repeated = await capture( "repeated-selection" );
			await page.screenshot( { path: directory + "/repeated-selection.png" } );
			assert.equal( repeated.conversation?.phase, "menu", "Repeated selection must retain the granted NPC menu" );
			assert.ok( repeated.controls.includes( "npc-close" ) );
			assert.equal( wire.filter( frame => frame.direction === "sent" && frame.opcode === 0x745a ).length, 1 );
			for ( const close of [ "button", "escape" ] ) {
				if ( close === "button" ) await page.locator( '[data-ui-id="npc-close"]' ).click();
				else await page.keyboard.press( "Escape" );
				await page.waitForFunction(
					() => {
						const game = __playableRuntime.gameplay();
						return game.target === 0 && !game.targetPending && game.npcConversation?.phase === "closed";
					},
					null,
					{ timeout: RESPONSE_TIMEOUT_MS }
				);
				await capture( close + "-closed" );
				await select();
				await page.locator( '[data-ui-id="npc-close"]' ).waitFor( { timeout: RESPONSE_TIMEOUT_MS } );
				await capture( close + "-reopened" );
			}
			assert.deepEqual( evidence.errors, [] );
			for ( const [opcode, count] of [ [ 0x745a, 3 ], [ 0xb45a, 3 ], [ 0x74b3, 2 ], [ 0xb4b3, 2 ] ] ) {
				assert.equal(
					wire.filter( frame => frame.opcode === opcode ).length,
					count,
					"Selection and release transactions must balance"
				);
			}
			evidence.verdict = "PASS SUCCESS";
			await page.screenshot( { path: directory + "/reopened.png" } );
		} catch ( error ) {
			evidence.failure = String( error );
			await capture( "failure" ).catch( () => {} );
			await page.screenshot( { path: directory + "/failure.png" } ).catch( () => {} );
			throw error;
		} finally {
			if ( tracing ) await page.context().tracing.stop( { path: directory + "/trace.zip" } );
			await writeFile( directory + "/report.json", JSON.stringify( { ...evidence, wire }, null, 2 ) + "\n" );
			await browser.close();
		}
	}
);
