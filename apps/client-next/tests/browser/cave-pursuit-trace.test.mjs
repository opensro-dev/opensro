import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { openProbeAgentSession, readProbeCharacterSpawnFromSession } from "../../../../scripts/lib/probeSession.mjs";
import { resetMissionMovementFixture } from "../../../../scripts/lib/missionMovementFixture.mjs";
import { assertCharacterAllowed } from "../../../../scripts/lib/probeCharacter.mjs";
import { bootPlayableSession } from "./helpers/playable-session.mjs";
import { installPursuitRecorder } from "./helpers/pursuit-recorder.mjs";

/*
================
cave pursuit trace (#1647)

Verification-first evidence for the Donwhang cave stutter/clipping
report: aggro one cave monster, kite it through two legs with a hard
direction change between them (the corridor-corner case in miniature),
and record the monster's entity poses, motion segments and visible
model anchors the whole way. Offline analysis judges corrections,
per-frame speed against the walking speed, and drawn-Y continuity.
================
*/
const CAVE_REGION = 0x8001;
const CAVE_START = { regionId: CAVE_REGION, x: 629, y: -0.150344, z: 74 };
const CHASE_MS = 26000;

test( "cave pursuit trace: a cave monster chases around two legs", { timeout: 240000 }, async () => {
	const character = assertCharacterAllowed( "asd2", { context: "cave pursuit trace" } );
	let authority = await openProbeAgentSession();
	const original = await readProbeCharacterSpawnFromSession( authority, character );
	assert.ok( original );
	const out = "temp/artifacts/cave-pursuit/" + (process.env.SRO_CAVE_CAPTURE ?? "current");
	await mkdir( out, { recursive: true } );
	await writeFile( out + "/restore.json", JSON.stringify( { character, original }, null, 2 ) );
	let browser, page, primaryFailure, cleanupFailure;
	const errors = [];
	const fixture = id => ({
		session: authority,
		characterName: character,
		timeoutMs: 30000,
		fixture: { id, movementMode: 3, start: CAVE_START, startYawRadians: 0 }
	});
	try {
		await resetMissionMovementFixture( fixture( "cave-trace" ) );
		({ browser, page } = await launchProbeBrowser());
		page.on( "pageerror", e => errors.push( e.message ) );
		const instrumentation = await installPursuitRecorder( page );
		console.log( "[cave] authenticated boot" );
		await bootPlayableSession( page, character );
		assert.ok( instrumentation.core && instrumentation.motion, "Both owner observations must be installed" );
		const revived = await page.evaluate( () => {
			const g = __playableRuntime.gameplay();
			__pursuit.boot = structuredClone( { vitals: g.vitals, local: __playableRuntime.entity( g.localGid ) } );
			if ( g.vitals.some( v => v.gid === g.localGid && v.hp === 0 ) ) {
				__playableRuntime.session( { kind: "gameplay", command: { kind: "rebirth", choice: 2 } } );
				return true;
			}
			return false;
		} );
		if ( revived ) {
			await page.waitForFunction(
				() => {
					const g = __playableRuntime.gameplay();
					return g.vitals.some( v => v.gid === g.localGid && v.hp > 0 );
				},
				null,
				{ timeout: 10000 }
			);
			await page.evaluate( () => __playableRuntime.session( { kind: "logout" } ) );
			await page.waitForFunction( () => __playableRuntime.sessionState()?.phase === "signed-out" );
			await resetMissionMovementFixture( fixture( "cave-after-rebirth" ) );
			await bootPlayableSession( page, character );
		}
		const pose = await page.evaluate( () => __playableRuntime.gameplay().pose );
		assert.equal( pose?.regionId & 0xffff, CAVE_REGION, "Cave fixture must survive world admission" );

		// Pick the nearest cave monster on the player's own floor: the
		// cave stacks floors in one region, and an upper-floor monster
		// can never chase (run 1 measured exactly that).
		const target = await page.evaluate( () => {
			const p = __playableRuntime.gameplay().pose;
			const monsters = Object.values( __pursuit.entities )
				.filter( e => e.kind === "monster" && (e.regionId & 0xffff) === 0x8001 && Math.abs( e.y - p.y ) <= 4 )
				.map( e => ({ gid: e.gid, x: e.x, y: e.y, z: e.z, distance: Math.hypot( e.x - p.x, e.z - p.z ) }) )
				.sort( ( a, b ) => a.distance - b.distance );
			return monsters[0] ?? null;
		} );
		assert.ok( target, "a cave monster must be near the fixture point" );
		// Close to engagement range first (the pursuit pattern): a chase
		// that starts 150+ units out through corridors measures the
		// interest boundary, not the chase.
		if ( target.distance > 45 ) {
			await page.evaluate( t => {
				const p = __playableRuntime.gameplay().pose;
				const d = Math.hypot( t.x - p.x, t.z - p.z );
				__playableRuntime.session( {
					kind: "gameplay",
					command: {
						kind: "move",
						destination: {
							regionId: p.regionId,
							x: t.x + (p.x - t.x) * 40 / d,
							y: p.y,
							z: t.z + (p.z - t.z) * 40 / d,
							angle: p.angle
						}
					}
				} );
			}, target );
			await page.waitForFunction(
				gid => {
					const g = __playableRuntime.gameplay();
					if ( g.error ) throw Error( g.error );
					const e = __pursuit.entities[gid], p = g.pose;
					return !p || !e || Math.hypot( e.x - p.x, e.z - p.z ) <= 48 && !g.pendingMoves && !g.moving;
				},
				target.gid,
				{ timeout: 30000 }
			).catch( () => console.log( "[cave] approach did not settle; engaging anyway" ) );
		}
		console.log( "[cave] engaging", JSON.stringify( target ) );
		await page.evaluate( gid => {
			__pursuit.traceTarget = gid;
			__pursuit.traceStarted = performance.now();
			__playableRuntime.session( { kind: "gameplay", command: { kind: "attack", gid } } );
		}, target.gid );

		// Kite: two legs away from the monster with a hard turn between
		// them, reissued as the player advances; sample throughout.
		const trace = await page.evaluate( async chaseMs => {
			const r = __pursuit, gid = r.traceTarget, samples = [];
			const pose = () => __playableRuntime.gameplay().pose;
			const entity = () => r.entities[gid];
			const start = performance.now();
			const leg = ( dx, dz ) => {
				const p = pose(), e = entity();
				if ( !p || !e ) return;
				const len = Math.hypot( dx, dz ) || 1;
				__playableRuntime.session( {
					kind: "gameplay",
					command: {
						kind: "move",
						destination: {
							regionId: p.regionId,
							x: p.x + dx / len * 55,
							y: p.y,
							z: p.z + dz / len * 55,
							angle: p.angle
						}
					}
				} );
			};
			// Leg 1 directly away from the monster; leg 2 at a right angle.
			const e0 = entity(), p0 = pose();
			leg( p0.x - e0.x, p0.z - e0.z );
			let turned = false;
			while ( performance.now() - start < chaseMs ) {
				const now = performance.now(), p = pose(), e = entity();
				const visible = globalThis.__pursuitVisible;
				const drawn = __playableRuntime.entity( gid );
				samples.push( {
					at: now - start,
					player: p && { x: p.x, y: p.y, z: p.z },
					monster: e && { x: e.x, y: e.y, z: e.z, moving: e.moving, walkSpeed: e.walkSpeed },
					drawn: drawn && { x: drawn.x, y: drawn.y, z: drawn.z },
					segment: r.segments[gid] ? structuredClone( r.segments[gid] ) : null,
					anchored: !!(visible && visible.anchors[gid] && now - visible.at < 300)
				} );
				if ( !turned && now - start > chaseMs * 0.45 && p && e ) {
					turned = true;
					const dx = p.x - e.x, dz = p.z - e.z;
					leg( -dz, dx );
				}
				await new Promise( resolve => setTimeout( resolve, 45 ) );
			}
			return samples;
		}, CHASE_MS );
		await writeFile(
			out + "/samples.json",
			JSON.stringify(
				{
					target,
					samples: trace,
					events: await page.evaluate( () => __pursuit.events.slice( -1500 ) )
				},
				null,
				2
			)
		);
		console.log( "[cave] samples recorded:", trace.length );
		await page.screenshot( { path: out + "/cave.png" } ).catch( () => {} );
		assert.deepEqual( errors, [] );
	} catch ( error ) {
		primaryFailure = error;
	} finally {
		const record = page ? await page.evaluate( () => __pursuit ).catch( () => null ) : null;
		if ( page ) {
			await page.screenshot( { path: out + "/mission.png" } ).catch( () => {} );
			await page.evaluate( () => __playableRuntime?.session( { kind: "logout" } ) ).catch( () => {} );
		}
		await browser?.close();
		try {
			// The probe session is short-lived; log in again for the restore.
			authority = await openProbeAgentSession();
			await resetMissionMovementFixture( {
				session: authority,
				characterName: character,
				timeoutMs: 30000,
				fixture: {
					id: "restore-cave-origin",
					movementMode: 3,
					start: original,
					startYawRadians: original.angle / 65535 * Math.PI * 2
				}
			} );
		} catch ( error ) {
			cleanupFailure = error;
		}
		await writeFile(
			out + "/session.json",
			JSON.stringify(
				{
					errors,
					primaryFailure: String( primaryFailure ?? "" ),
					cleanupFailure: String( cleanupFailure ?? "" ),
					record
				},
				null,
				2
			)
		);
	}
	if ( primaryFailure && cleanupFailure ) {
		throw new AggregateError( [ primaryFailure, cleanupFailure ], "Cave trace and restoration both failed" );
	}
	if ( primaryFailure ) throw primaryFailure;
	if ( cleanupFailure ) throw cleanupFailure;
} );
