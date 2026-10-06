import { CLIENT_PUBLIC_ROOT } from "../../../../../scripts/lib/generatedRoot.mjs";
import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../../scripts/lib/probeBrowser.mjs";
import { resetMissionMovementFixture } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { bootPlayableSession } from "./playable-session.mjs";

// Scratch provisioning uses normal merchant, equipment and learning commands.
// Only relocation uses the existing gated offline development fixture route.
export async function prepareCriticalArrow( name = "asd3" ) {
	const out = "temp/artifacts/critical-arrow";
	await mkdir( out, { recursive: true } );
	const position = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/data/npcpos.json", "utf8" ) ).rows.map(
		r => r.split( "\t" ).map( Number )
	).find( r => r[0] === 2003 );
	assert.ok( position, "authored Jangan smith position" );
	await resetMissionMovementFixture( {
		characterName: name,
		fixture: {
			id: "critical-arrow-merchant",
			movementMode: 3,
			start: { regionId: position[1], x: position[2] + 15, y: position[3], z: position[4] },
			startYawRadians: 0
		},
		onLog: console.log
	} );
	const { browser, page } = await launchProbeBrowser();
	page.setDefaultTimeout( 20000 );
	const report = { commands: [] };
	try {
		await page.route( "**/src/engine/runtime/renderer/renderer.ts*", async route => {
			const response = await route.fetch(), source = await response.text();
			assert.ok( source.includes( "export function createRenderer(" ) );
			await route.fulfill( {
				response,
				body: source.replace( "export function createRenderer(", "function observedRenderer(" ) +
					`\nexport function createRenderer(...args){const r=observedRenderer(...args);return {...r,setCharacterActors(a){globalThis.__arrowActors=a;r.setCharacterActors(a)}}}`,
				contentType: "application/javascript"
			} );
		} );
		await bootPlayableSession( page, name );
		await page.context().tracing.start( { screenshots: true, snapshots: true } );
		const command = async c => {
			report.commands.push( c );
			await page.evaluate( command => __playableRuntime.session( { kind: "gameplay", command } ), c );
		};
		const snapshot = () =>
			page.evaluate( () => {
				const g = __playableRuntime.gameplay();
				return {
					progression: g.progression,
					skills: g.skills,
					inventory: g.inventory.map( ( { slot, name, refObjId, quantity, typeFlags } ) => ({
						slot,
						name,
						refObjId,
						quantity,
						typeFlags
					}) ),
					vitals: g.vitals,
					shop: g.shop,
					error: g.error,
					trainingError: g.trainingError
				};
			} );
		report.before = await snapshot();
		assert.ok( report.before.progression.level >= 5, "scratch character must have earned level 5" );
		await page.waitForFunction( () =>
			globalThis.__arrowActors?.some( a => __playableRuntime.entity( a.gid )?.refObjId === 2003 )
		);
		const gid = await page.evaluate( () =>
			__arrowActors.find( a => __playableRuntime.entity( a.gid )?.refObjId === 2003 ).gid
		);
		await command( { kind: "select", gid } );
		await page.waitForFunction( gid => __playableRuntime.gameplay().target === gid, gid );
		await command( { kind: "shop-open", gid } );
		await page.waitForFunction( () => __playableRuntime.gameplay().shop?.offers?.length > 0 );
		for ( const [pattern, quantity] of [ [ "Bow", 1 ], [ "Arrow", 100 ] ] ) {
			let state = await snapshot();
			const existing = state.inventory.find( i => i.name.includes( pattern ) );
			if ( existing && (pattern === "Bow" || existing.quantity >= 20) ) continue;
			const offer = state.shop.offers.find( o => o.name.includes( pattern ) );
			assert.ok( offer, pattern + " must be sold by authored merchant" );
			await command( { kind: "shop-buy", tab: offer.tab, slot: offer.slot, quantity } );
			await page.waitForFunction(
				ref => __playableRuntime.gameplay().inventory.some( i => i.refObjId === ref ),
				offer.refObjId
			);
			await page.waitForFunction( () => !__playableRuntime.gameplay().inventoryPending );
		}
		await command( { kind: "npc-close" } );
		for ( const [pattern, destination] of [ [ "Bow", 6 ], [ "Arrow", 7 ] ] ) {
			const item = (await snapshot()).inventory.find( i => i.name.includes( pattern ) );
			assert.ok( item );
			if ( item.slot !== destination ) {
				await command( { kind: "inventory-move", source: item.slot, destination, quantity: item.quantity } );
				await page.waitForFunction(
					( { ref, destination } ) =>
						__playableRuntime.gameplay().inventory.some( i =>
							i.slot === destination && i.refObjId === ref
						),
					{ ref: item.refObjId, destination }
				);
			}
		}
		for (
			let level = (await snapshot()).progression.masteries.find( m => m.id === 259 )?.level ?? 0;
			level < 5;
			level++
		) {
			await command( { kind: "mastery-train", id: 259 } );
			await page.waitForFunction(
				level => __playableRuntime.gameplay().progression.masteries.find( m => m.id === 259 )?.level > level,
				level
			);
		}
		const skill = await page.evaluate( () =>
			__playableRuntime.gameplay().skillCatalog.find( s => s.name === "SKILL_CH_BOW_CRITICAL_A_01" )
		);
		assert.ok( skill );
		if ( !(await snapshot()).skills.includes( skill.id ) ) {
			await command( { kind: "skill-train", id: skill.id } );
			await page.waitForFunction( id => __playableRuntime.gameplay().skills.includes( id ), skill.id );
		}
		report.after = await snapshot();
		report.skill = skill.id;
		report.verdict = "PASS";
		console.log(
			"critical-arrow scratch prepared",
			JSON.stringify( {
				skill: skill.id,
				progression: report.after.progression,
				inventory: report.after.inventory
			} )
		);
	} catch ( e ) {
		report.error = String( e );
		report.failureState = await page.evaluate( () => ({
			gameplay: __playableRuntime.gameplay(),
			session: __playableRuntime.sessionState()
		}) ).catch( () => null );
		await page.screenshot( { path: out + "/prepare-failure.png" } ).catch( () => {} );
		throw e;
	} finally {
		await page.context().tracing.stop( { path: out + "/prepare-trace.zip" } ).catch( () => {} );
		await writeFile( out + "/prepare.json", JSON.stringify( report, null, 2 ) );
		await browser.close();
	}
	return report;
}
