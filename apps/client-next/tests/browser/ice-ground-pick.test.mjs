/*
===========================================================================

ice-ground-pick.test.mjs - ground clicks above special water in the ice area

Boots the scratch character on an ice-area spot where terrain rises above a
special-water plane, and checks that a ground click picks the visible
terrain (not the water plane behind it) and that the character walks there.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { openProbeAgentSession, readProbeCharacterSpawnFromSession } from "../../../../scripts/lib/probeSession.mjs";
import { resetMissionMovementFixture } from "../../../../scripts/lib/missionMovementFixture.mjs";
import { bootPlayableSession } from "./helpers/playable-session.mjs";

test( "ice-area ground click selects nearer terrain above the special-water plane", { timeout: 150000 }, async () => {
	const out = "temp/artifacts/ice-ground-pick";
	await mkdir( out, { recursive: true } );
	const session = await openProbeAgentSession(),
		original = await readProbeCharacterSpawnFromSession( session, "asd2" );
	assert.ok( original );
	const start = { regionId: 0x587d, x: 1423, y: 832.9823, z: 1859 };
	await resetMissionMovementFixture( {
		session,
		characterName: "asd2",
		timeoutMs: 30000,
		fixture: { id: "ice-ground-pick", movementMode: 3, start, startYawRadians: Math.PI }
	} );
	const { browser, page } = await launchProbeBrowser(), report = { start, errors: [] };
	try {
		page.on( "pageerror", e => report.errors.push( String( e ) ) );
		await page.route( "**/src/engine/runtime/renderer/world/world.ts*", async route => {
			const response = await route.fetch(),
				source = await response.text(),
				needle = "return pickTerrainCells(interactionCells, ray, terrainPicks);";
			assert.ok( source.includes( needle ) );
			await route.fulfill( {
				response,
				body: source.replace( needle, "globalThis.__iceCells=interactionCells; " + needle ),
				contentType: "application/javascript"
			} );
		} );
		await page.route( "**/src/engine/runtime/renderer/renderer.ts*", async route => {
			const response = await route.fetch(), source = await response.text();
			assert.ok( source.includes( "export function createRenderer(" ) );
			await route.fulfill( {
				response,
				body: source.replace( "export function createRenderer(", "function createObservedRenderer(" ) +
					"\nexport function createRenderer(...args){const owner=createObservedRenderer(...args);globalThis.__iceRenderer=owner;return {...owner,setSelectionDecal(value){globalThis.__iceDecal=value;return owner.setSelectionDecal(value);}};}",
				contentType: "application/javascript"
			} );
		} );
		await bootPlayableSession( page, "asd2" );
		report.sample = await page.evaluate( async () => {
			const { pickTerrainCells, terrainCellKey } = await import(
				"/src/engine/foundation/rendering/terrain-interaction.ts"
			);
			const { pickDestination } = await import( "/src/engine/foundation/rendering/pick-destination.ts" );
			const pose = __playableRuntime.gameplay().pose, local = __playableRuntime.gameplay().localGid;
			let best = null;
			for ( const y of [ .4, .45, .5, .55, .6 ] ) {
				for ( const x of [ .3, .4, .6, .7 ] ) {
					if ( __iceRenderer.pickEntity( x, y, local ) ) continue;
					const query = __iceRenderer.pickGround( x, y );
					if ( !query || query.terrainDepth === null ) continue;
					const terrainCells = new Map(
						[ ...__iceCells ].map( ( [k, c] ) => [ k, { ...c, water: undefined } ] )
					);
					const depth = pickTerrainCells( terrainCells, query.ray );
					if ( depth === null ) continue;
					const point = query.ray.start.map( ( v, i ) => v + query.ray.delta[i] * depth ),
						cell = __iceCells.get(
							terrainCellKey( Math.floor( point[0] / 320 ), Math.floor( point[2] / 320 ) )
						);
					if (
						cell?.water?.type !== 1 || !cell.water.waveType || point[1] < cell.water.height + 10
					) continue;
					const expected = pickDestination( query.ray, depth, query.originRegion ),
						distance = Math.hypot(
							expected.x - pose.x + ((expected.regionId & 255) - (pose.regionId & 255)) * 1920,
							expected.z - pose.z + ((expected.regionId >>> 8) - (pose.regionId >>> 8)) * 1920
						);
					if ( distance < 25 || distance > 180 ) continue;
					const sample = {
						x,
						y,
						query,
						depth,
						expected,
						actual: pickDestination( query.ray, query.terrainDepth, query.originRegion ),
						water: cell.water,
						distance
					};
					if ( !best || distance < best.distance ) best = sample;
				}
			}
			return best;
		} );
		assert.ok( report.sample, "must exercise ground above a special-water plane" );
		await page.screenshot( { path: out + "/before-click.png" } );
		assert.ok(
			Math.abs( report.sample.query.terrainDepth - report.sample.depth ) < 1e-6,
			`pick went behind visible terrain: ${JSON.stringify( report.sample )}`
		);
		const { x, y, expected } = report.sample;
		await page.mouse.click( x * 1024, y * 768 );
		await page.waitForFunction( () => !!globalThis.__iceDecal, null, { timeout: 10000 } );
		report.decal = await page.evaluate( () => __iceDecal );
		const delta = ( p, q ) =>
			Math.hypot(
				p.x - q.x + ((p.regionId & 255) - (q.regionId & 255)) * 1920,
				p.z - q.z + ((p.regionId >>> 8) - (q.regionId >>> 8)) * 1920
			);
		assert.ok( delta( report.decal.pose, expected ) < 2, JSON.stringify( report.decal ) );
		await page.waitForFunction( () => !__playableRuntime.gameplay().moving, null, { timeout: 15000 } );
		report.after = await page.evaluate( () => __playableRuntime.gameplay().pose );
		assert.ok( delta( report.after, expected ) < 3, JSON.stringify( report.after ) );
		await page.screenshot( { path: out + "/arrived.png" } );
		assert.deepEqual( report.errors, [] );
		report.verdict = "PASS SUCCESS";
	} catch ( error ) {
		report.failure = String( error );
		throw error;
	} finally {
		await page.evaluate( () => globalThis.__playableRuntime?.session( { kind: "logout" } ) ).catch( () => {} );
		await browser.close();
		await writeFile( out + "/incident.json", JSON.stringify( report, null, 2 ) );
		await resetMissionMovementFixture( {
			session,
			characterName: "asd2",
			timeoutMs: 30000,
			fixture: {
				id: "restore-ice-probe",
				movementMode: 3,
				start: original,
				startYawRadians: original.angle / 65535 * Math.PI * 2
			}
		} );
	}
} );
