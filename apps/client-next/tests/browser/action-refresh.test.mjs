import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { bootPlayableSession } from "./helpers/playable-session.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test( "native clothing freeze and action refresh through the real presenter and GPU", { timeout: 180000 }, async () => {
	const out = "temp/artifacts/action-refresh-browser";
	await mkdir( out, { recursive: true } );
	const roster = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/char/roster.json", "utf8" ) );
	const { browser, page } = await launchProbeBrowser(), evidence = { errors: [], cases: [] };
	try {
		await holdProbeRuntime( page );
		page.on( "pageerror", e => evidence.errors.push( String( e ) ) );
		await page.route( "**/src/engine/runtime/renderer/device/geometry.ts*", async route => {
			const response = await route.fetch(),
				source = await response.text(),
				signature = /upload\(data, image, paletteOffsets, environmentImage\)\s*\{/;
			assert.ok( signature.test( source ) );
			await route.fulfill( {
				response,
				contentType: "application/javascript",
				body: source.replace(
					signature,
					"$& if(data.material?.groundDecal){globalThis.__toeGpuUploads=(globalThis.__toeGpuUploads??0)+1;}"
				)
			} );
		} );
		await page.route( "**/src/engine/runtime/characters/characters.ts*", async route => {
			const response = await route.fetch(), source = await response.text();
			assert.ok( source.includes( "export function createCharacterPresentation(" ) );
			// Fixtures enter at metadata/session/entity admission. No inventory writes,
			// no replacement final actors, and no guessed sockets or frame timers.
			const body =
				source.replace( "export function createCharacterPresentation(", "function observedPresentation(" ) + `
export function createCharacterPresentation(...args){
 const assets=args[0],jobs=new Map();args[0]={...assets,request(...params){const id=assets.request(...params);jobs.set(id,params[0]);return id;},take(id){const row=assets.take(id);if(row?.kind==='bytes'&&jobs.get(id)?.endsWith('/char/roster.json')){const value=JSON.parse(new TextDecoder().decode(row.buffer));value.dress.defaultWearLanguage=0;return {...row,buffer:new TextEncoder().encode(JSON.stringify(value)).buffer};}return row;}};
 const renderer=args[1];args[1]={...renderer,setCharacterActors(rows){globalThis.__refreshActors=rows;renderer.setCharacterActors(rows);},setFootprints(rows){globalThis.__refreshFeet=rows;renderer.setFootprints(rows);}};
 args[5]=()=> 'SNOW';const owner=observedPresentation(...args);
 return {...owner,step(entities,gameplay,...rest){const f=globalThis.__refreshFixture;rest[9]=f?.shard??'Server#$T';if(f&&gameplay){entities=entities.map(e=>e.gid===gameplay.localGid?{...e,refObjId:f.refObjId,equipment:f.equipment??[],avatars:f.avatars??[],movementMode:f.mode??3,mountedOn:undefined,appearanceState:[1,0,0]}:e);gameplay={...gameplay,inventory:f.equipment??[],moving:f.moving??false,movementRevision:f.revision??0};rest[9]=f.shard??'Server#$T';}const result=owner.step(entities,gameplay,...rest);globalThis.__refreshError=owner.error();return result;}};}`;
			await route.fulfill( { response, body, contentType: "application/javascript" } );
		} );
		await bootPlayableSession( page, "asd2" );
		const actor = () =>
			page.evaluate( () => __refreshActors.find( a => a.gid === __playableRuntime.gameplay().localGid ) );
		for ( const prefix of [ "CH_M", "CH_W", "EU_M", "EU_W" ] ) {
			const [race, sex] = prefix.split( "_" ),
				model = roster.models.find( m =>
					m.codename.startsWith( `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_` )
				);
			const shoulder = Number(
				Object.entries( roster.dress.equipment ).find( ( [id, r] ) =>
					r.bodies[prefix] && r.slot === 2 && r.armorClass === 3
				)[0]
			);
			const ids = Object.keys( roster.dress.avatarAuxiliary ).map( Number ).filter( id =>
				roster.dress.equipment[id].bodies[prefix]
			);
			const set = async extra => {
				await page.evaluate( f => globalThis.__refreshFixture = f, { refObjId: model.refObjId, ...extra } );
			};
			const wait = async clip =>
				page.waitForFunction(
					( { base, clip } ) => {
						const a = __refreshActors?.find( a => a.gid === __playableRuntime.gameplay().localGid );
						return !__refreshError && a?.model.includes( base ) && a.clip === clip;
					},
					{ base: model.glb, clip },
					{ timeout: 20000 }
				);
			await set( {} );
			await wait( "stand" );
			const defaults = race === "CH" ?
				[ "BA", "LA" ].map( p => roster.dress.defaultWear[prefix + "_clothes_" + p].glb ) :
				[];
			await page.waitForFunction(
				paths =>
					paths.every( p =>
						__refreshActors.find( a => a.gid === __playableRuntime.gameplay().localGid ).model.includes( p )
					),
				defaults
			);
			await set( { shard: "Normal", equipment: [ { slot: 2, refObjId: shoulder } ] } );
			await page.waitForFunction(
				path =>
					__refreshActors.find( a => a.gid === __playableRuntime.gameplay().localGid ).model.includes( path ),
				roster.dress.equipment[shoulder].bodies[prefix].glb
			);
			const frozen = await actor();
			assert.ok( defaults.every( p => frozen.model.includes( p ) ) );
			evidence.cases.push( { prefix, case: "frozen-defaults-with-equipped-shoulder", model: frozen.model } );
			await set( { moving: true, revision: 1 } );
			await wait( "run" );
			await set( { moving: true, revision: 1, avatars: [ { refObjId: ids[0] } ] } );
			await wait( "stand" );
			await page.waitForFunction( () => __refreshFeet?.length >= 2 && globalThis.__toeGpuUploads > 0 );
			evidence.cases.push( {
				prefix,
				case: "move-exit",
				feet: await page.evaluate( () => __refreshFeet ),
				gpuUploads: await page.evaluate( () => __toeGpuUploads )
			} );
			await set( { moving: true, revision: 2, avatars: [ { refObjId: ids[0] } ] } );
			await wait( "native:avatar_wing:7" );
			await page.screenshot( { path: `${out}/${prefix}-move.png` } );
			await set( { mode: 4, revision: 2, avatars: [ { refObjId: ids[0] } ] } );
			await wait( "charselect-state14" );
			await page.waitForFunction( () => {
				const a = __refreshActors.find( a => a.gid === __playableRuntime.gameplay().localGid );
				return a.time > 1.2 && !(a.layers ?? []).some( l => l.clip === "charselect-state13" );
			} );
			const sitting = await actor();
			await set( { mode: 4, revision: 2, avatars: [ { refObjId: ids[1] } ] } );
			await page.waitForFunction(
				id => __refreshActors.find( a => a.gid === __playableRuntime.gameplay().localGid ).model.includes( id ),
				roster.dress.equipment[ids[1]].bodies[prefix].glb
			);
			const replaced = await actor();
			assert.equal( replaced.clip, "charselect-state14" );
			assert.ok( !(replaced.layers ?? []).some( l => l.clip === "charselect-state13" ) );
			assert.equal( replaced.height, sitting.height );
			await page.screenshot( { path: `${out}/${prefix}-sit-refresh.png` } );
			evidence.cases.push( { prefix, case: "sit-refresh", height: replaced.height } );
			await set( { revision: 3 } );
			await wait( "stand" );
			await page.waitForFunction(
				h => __refreshActors.find( a => a.gid === __playableRuntime.gameplay().localGid ).height >=
					h * 2 - 1e-5,
				sitting.height
			);
		}
		assert.deepEqual( evidence.errors, [] );
		evidence.verdict = "PASS";
	} catch ( error ) {
		evidence.failure = String( error );
		evidence.runtime = await page.evaluate( () => ({
			fixture: globalThis.__refreshFixture,
			error: globalThis.__refreshError,
			actors: globalThis.__refreshActors?.map( a => ({
				gid: a.gid,
				clip: a.clip,
				height: a.height,
				model: a.model
			}) )
		}) ).catch( () => null );
		throw error;
	} finally {
		await page.evaluate( () => __playableRuntime?.session( { kind: "logout" } ) ).catch( () => {} );
		await browser.close();
		await writeFile( out + "/incident.json", JSON.stringify( evidence, null, 2 ) );
	}
} );
