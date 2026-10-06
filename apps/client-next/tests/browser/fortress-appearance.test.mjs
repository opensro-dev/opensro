import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { bootPlayableSession } from "./helpers/playable-session.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test( "native fortress/team uniforms replace and restore gear on all four rigs", { timeout: 180000 }, async () => {
	const out = "temp/artifacts/fortress-browser";
	await mkdir( out, { recursive: true } );
	const roster = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/char/roster.json", "utf8" ) );
	const { browser, page } = await launchProbeBrowser(), evidence = { errors: [], cases: [] };
	try {
		await holdProbeRuntime( page );
		page.on( "pageerror", e => evidence.errors.push( String( e ) ) );
		await page.route( "**/src/engine/runtime/characters/characters.ts*", async route => {
			const response = await route.fetch(), source = await response.text();
			assert.ok( source.includes( "export function createCharacterPresentation(" ) );
			const body = source.replace(
				"export function createCharacterPresentation(",
				"function createObservedPresentation("
			) + `
export function createCharacterPresentation(...args){const renderer=args[1];args[1]={...renderer,setCharacterActors(actors){globalThis.__fortActors=actors;renderer.setCharacterActors(actors);}};const health=args[6];if(health)args[6]={...health,dead(gid){return globalThis.__fortFixture?false:health.dead(gid);}};const owner=createObservedPresentation(...args);return {...owner,step(entities,gameplay,...rest){const f=globalThis.__fortFixture;if(f&&gameplay){entities=entities.map(e=>e.gid===gameplay.localGid?{...e,refObjId:f.refObjId,appearanceState:[1,0,0],movementMode:3,arenaTeam:f.team??255,mountedOn:f.mountedOn,equipment:f.inventory,avatars:f.avatars??[]}:e);gameplay={...gameplay,moving:false,inventory:f.inventory,social:{...gameplay.social,guild:{...gameplay.social?.guild,id:10}},fortress:{worldId:1,worlds:[{id:1,code:'war'}],fortresses:[{id:99,code:'war'}],wars:[{id:99,flags:f.war?1:0}],registered:[10],listId:99}};rest[10]=!!f.normal;}const result=owner.step(entities,gameplay,...rest);globalThis.__fortError=owner.error();return result;}};}`;
			await route.fulfill( { response, body, contentType: "application/javascript" } );
		} );
		await bootPlayableSession( page, "asd2" );
		for ( const prefix of [ "CH_M", "CH_W", "EU_M", "EU_W" ] ) {
			const [race, sex] = prefix.split( "_" ),
				model = roster.models.find( m =>
					m.codename.startsWith( `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_` )
				);
			assert.ok( model );
			const find = fn =>
				Number(
					Object.entries( roster.dress.equipment ).find( ( [, r] ) => r.bodies[prefix] && fn( r ) )?.[0]
				);
			const armor = find( r => r.slot === 1 ),
				weapon = find( r => r.slot === 6 ),
				shield = find( r => r.slot === 7 ),
				avatar = find( r => r.avatarSlot === 1 );
			assert.ok( armor && weapon && shield && avatar );
			const inventory = [ { slot: 1, refObjId: armor, plus: 0 }, { slot: 6, refObjId: weapon, plus: 0 }, {
				slot: 7,
				refObjId: shield,
				plus: 0
			} ];
			for ( let cycle = 0; cycle < 2; cycle++ ) {
				for (
					const f of [
						{ name: "normal", index: -1 },
						{ name: "war", war: true, index: 0 },
						{ name: "normal-option", war: true, normal: true, index: -1 },
						{ name: "team0", team: 0, normal: true, index: 3 },
						{ name: "team1", team: 1, index: 4 },
						{ name: "mounted", team: 1, mountedOn: 999999, index: 4 },
						{ name: "restored", index: -1 }
					]
				) {
					await page.evaluate( f => globalThis.__fortFixture = f, {
						...f,
						refObjId: model.refObjId,
						inventory,
						avatars: [ { refObjId: avatar } ]
					} );
					const uniform = f.index >= 0 && race === "CH" ?
						roster.dress.fortressWear[prefix + "_" + f.index].glb :
						null;
					const kept = f.index >= 0 ? f.mountedOn ? [] : [ weapon, shield ] : [ weapon, shield, avatar ];
					const expected = kept.map( id => roster.dress.equipment[id].bodies[prefix].glb );
					await page.waitForFunction( ( { base, uniform, expected, active, avatar } ) => {
						const a = __fortActors?.find( a => a.gid === __playableRuntime.gameplay().localGid );
						if ( __fortError || !a ) return false;
						const key = "assembly:" + base + ":",
							parts = a.model.startsWith( key ) ?
								JSON.parse( a.model.slice( key.length ) ) :
								a.model === base ?
								[] :
								null;
						if ( !parts ) return false;
						return expected.every( path => parts.some( p => p.model === path ) ) && parts.filter( p =>
									p.model.includes( "/fortress_" )
								).length === (uniform ? 1 : 0) &&
							(!uniform || parts.some( p => p.model === uniform )) && (!active || !parts.some( p =>
								p.model === avatar
							));
					}, {
						base: model.glb,
						uniform,
						expected,
						active: f.index >= 0,
						avatar: roster.dress.equipment[avatar].bodies[prefix].glb
					}, { timeout: 30000 } );
					evidence.cases.push( {
						prefix,
						cycle,
						...f,
						model: await page.evaluate( () =>
							__fortActors.find( a => a.gid === __playableRuntime.gameplay().localGid ).model
						)
					} );
					if ( cycle === 0 ) await page.screenshot( { path: `${out}/${prefix}-${f.name}.png` } );
				}
			}
		}
		assert.deepEqual( evidence.errors, [] );
	} finally {
		await writeFile( `${out}/evidence.json`, JSON.stringify( evidence, null, 2 ) );
		await browser.close();
	}
} );
