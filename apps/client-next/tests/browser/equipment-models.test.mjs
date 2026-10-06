import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { bootPlayableSession } from "./helpers/playable-session.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test( "real 9th-degree equipment resources render on all four body rigs", { timeout: 180000 }, async () => {
	const out = "temp/artifacts/equipment-models";
	await mkdir( out, { recursive: true } );
	const roster = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/char/roster.json", "utf8" ) );
	const items = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/data/missionPresentation.json", "utf8" ) )
		.itemsByRefObjId;
	const byCode = new Map( Object.entries( items ).map( ( [id, r] ) => [ r.codename, Number( id ) ] ) );
	const { browser, page } = await launchProbeBrowser();
	const evidence = { errors: [], cases: [] };
	try {
		await holdProbeRuntime( page );
		page.on( "pageerror", e => evidence.errors.push( String( e ) ) );
		await page.route( "**/src/engine/runtime/renderer/device/geometry.ts*", async route => {
			const response = await route.fetch(), source = await response.text();
			const signature = /updateEquipmentGlow\(draw, color, uv, gain, alphaTest, enabled\)\s*\{/;
			assert.ok( signature.test( source ), "production glow GPU adapter must be observed" );
			await route.fulfill( {
				response,
				contentType: "application/javascript",
				body: source.replace(
					signature,
					"$& globalThis.__equipmentGlowWrites??=[];if(globalThis.__equipmentGlowWrites.length<200)globalThis.__equipmentGlowWrites.push({color:[...color],uv:[...uv],gain,alphaTest,enabled});"
				)
			} );
		} );
		await page.route( "**/src/engine/runtime/characters/characters.ts*", async route => {
			const response = await route.fetch(), source = await response.text();
			assert.ok( source.includes( "export function createCharacterPresentation(" ) );
			// Renderer-only fixture: neither gameplay authority nor inventory is changed.
			const body = source.replace(
				"export function createCharacterPresentation(",
				"function createObservedPresentation("
			) + `
export function createCharacterPresentation(...args){const renderer=args[1];args[1]={...renderer,setCharacterActors(actors){globalThis.__equipmentActors=actors;renderer.setCharacterActors(actors);}};const owner=createObservedPresentation(...args);return {...owner,step(entities,gameplay,...rest){const f=globalThis.__equipmentFixture;if(f&&gameplay){entities=entities.map(e=>e.gid===gameplay.localGid?{...e,refObjId:f.refObjId,appearanceState:[1,0,f.hwan?1:0],mountedOn:f.mountedOn,equipment:f.inventory,avatars:f.avatars??[],movementMode:f.movementMode??e.movementMode}:e);gameplay={...gameplay,inventory:f.inventory,moving:f.moving??gameplay.moving};}const result=owner.step(entities,gameplay,...rest);globalThis.__equipmentError=owner.error();return result;}};}`;
			await route.fulfill( { response, body, contentType: "application/javascript" } );
		} );
		await bootPlayableSession( page, "asd2" );
		for ( const race of [ "CH", "EU" ] ) {
			for ( const sex of [ "M", "W" ] ) {
				for ( const head of [ "HA", "CA" ] ) {
					const prefix = `${race}_${sex}`,
						model = roster.models.find( m =>
							m.codename.startsWith( `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_` )
						);
					assert.ok( model );
					const inventory = [ head, "BA", "SA", "AA", "LA", "FA" ].map( ( part, slot ) => ({
						slot,
						refObjId: byCode.get( `ITEM_${prefix}_HEAVY_09_${part}_A` ),
						quantity: 1,
						typeFlags: 0,
						plus: 0,
						durability: 100
					}) );
					const expected = inventory.flatMap( i => {
						const e = roster.dress.equipment[i.refObjId].bodies[prefix];
						return e ? [ { model: e.glb, parts: e.parts } ] : [];
					} );
					await page.evaluate( f => globalThis.__equipmentFixture = f, {
						refObjId: model.refObjId,
						inventory
					} );
					await page.waitForFunction(
						( { base, expected } ) => {
							const actor = __equipmentActors?.find( a =>
								a.gid === __playableRuntime.gameplay().localGid
							);
							return !__equipmentError && actor?.model.startsWith( "assembly:" + base + ":" ) &&
								expected.every( e =>
									actor.model.includes( e.model ) &&
									e.parts.every( p => actor.model.includes( '"' + p + '"' ) )
								);
						},
						{ base: model.glb, expected },
						{ timeout: 30000 }
					);
					const actor = await page.evaluate( () =>
						__equipmentActors.find( a => a.gid === __playableRuntime.gameplay().localGid )
					);
					evidence.cases.push( { prefix, head, model: actor.model } );
					await page.screenshot( { path: `${out}/${prefix}-${head}.png` } );
				}
			}
		}
		for ( const prefix of [ "CH_M", "CH_W", "EU_M", "EU_W" ] ) {
			const [race, sex] = prefix.split( "_" ),
				model = roster.models.find( m =>
					m.codename.startsWith( `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_` )
				);
			const find = predicate =>
				Number(
					Object.entries( roster.dress.equipment ).find( ( [id, r] ) => r.bodies[prefix] && predicate( r ) )
						?.[0]
				);
			const shoulder = find( r => r.slot === 2 && r.armorClass === 3 ),
				thief = find( r => r.thiefSuit ),
				dress = find( r => r.avatarSlot === 1 && (r.visualMask & 18) === 18 );
			assert.ok( shoulder && thief && dress );
			for ( let cycle = 0; cycle < 2; cycle++ ) {
				for (
					const [name, inventory, avatars, family] of [
						[ "bare", [], [], "clothes" ],
						[ "shoulder", [ { slot: 2, refObjId: shoulder } ], [], "light" ],
						[ "thief", [ { slot: 8, refObjId: thief } ], [], null ],
						[ "avatar", [], [ { refObjId: dress } ], null ],
						[ "restored", [], [], "clothes" ]
					]
				) {
					const defaults = race === "CH" && family ?
						[ "BA", "LA" ].map( p => roster.dress.defaultWear[prefix + "_" + family + "_" + p].glb ) :
						[];
					const expected = [
						...defaults,
						...[ ...inventory, ...avatars ].map( i =>
							roster.dress.equipment[i.refObjId].bodies[prefix].glb
						)
					].sort();
					await page.evaluate( f => globalThis.__equipmentFixture = f, {
						refObjId: model.refObjId,
						inventory,
						avatars
					} );
					await page.waitForFunction(
						( { base, expected } ) => {
							const actor = __equipmentActors?.find( a =>
								a.gid === __playableRuntime.gameplay().localGid
							);
							if ( __equipmentError || !actor ) return false;
							const key = "assembly:" + base + ":";
							const actual = actor.model.startsWith( key ) ?
								JSON.parse( actor.model.slice( key.length ) ).map( p => p.model ).sort() :
								actor.model === base ?
								[] :
								null;
							return actual !== null && JSON.stringify( actual ) === JSON.stringify( expected );
						},
						{ base: model.glb, expected },
						{ timeout: 30000 }
					);
					evidence.cases.push( {
						prefix,
						name,
						cycle,
						model: await page.evaluate( () =>
							__equipmentActors.find( a => a.gid === __playableRuntime.gameplay().localGid ).model
						)
					} );
					await page.screenshot( { path: `${out}/${prefix}-${name}-${cycle}.png` } );
				}
			}
		}
		for ( const prefix of [ "CH_M", "CH_W", "EU_M", "EU_W" ] ) {
			const [race, sex] = prefix.split( "_" ),
				model = roster.models.find( m =>
					m.codename.startsWith( `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_` )
				);
			const ids = Object.keys( roster.dress.avatarAuxiliary ).map( Number ).filter( id =>
				roster.dress.equipment[id].bodies[prefix]
			);
			for ( const id of ids ) {
				const extra = roster.dress.avatarAuxiliary[id];
				for (
					const [name, moving, movementMode, clip] of [ [ "stand", false, 3, "stand" ], [
						"run",
						true,
						3,
						"run"
					], [ "walk-retains-run", true, 2, "run" ] ]
				) {
					await page.evaluate( f => globalThis.__equipmentFixture = f, {
						refObjId: model.refObjId,
						inventory: [],
						avatars: [ { refObjId: id } ],
						moving,
						movementMode
					} );
					await page.waitForFunction(
						( { glb, clip } ) => {
							const rows = __equipmentActors?.filter( a =>
								a.attachment?.gid === __playableRuntime.gameplay().localGid && a.model === glb
							);
							return !__equipmentError && rows?.length === 1 && rows[0].clip === clip &&
								rows[0].time > .2;
						},
						{ glb: extra.glb, clip },
						{ timeout: 30000 }
					);
					const bodyClip = name === "run" ? "native:avatar_wing:7" : name === "stand" ? "stand" : "walk";
					await page.waitForFunction(
						clip =>
							!__equipmentError &&
							__equipmentActors.find( a => a.gid === __playableRuntime.gameplay().localGid )?.clip ===
								clip,
						bodyClip,
						{ timeout: 30000 }
					);
					evidence.cases.push( {
						prefix,
						id,
						name,
						wing: await page.evaluate(
							glb => __equipmentActors.find( a => a.attachment && a.model === glb ),
							extra.glb
						)
					} );
					await page.screenshot( { path: `${out}/${prefix}-wing-${id}-${name}.png` } );
				}
				await page.evaluate( f => globalThis.__equipmentFixture = f, {
					refObjId: model.refObjId,
					inventory: [],
					avatars: [],
					moving: false
				} );
				await page.waitForFunction( () =>
					!__equipmentActors.some( a => a.attachment && a.model.includes( "/avatar_aux_" ) )
				);
			}
		}
		for ( const prefix of [ "CH_M", "CH_W", "EU_M", "EU_W" ] ) {
			const [race, sex] = prefix.split( "_" ),
				model = roster.models.find( m =>
					m.codename.startsWith( `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_` )
				);
			const code = race === "CH" ? "ITEM_CH_SWORD_09_A" : "ITEM_EU_AXE_09_A", id = byCode.get( code );
			assert.ok( id && roster.dress.equipment[id]?.bodies[prefix], code );
			for ( const plus of [ 0, 3, 5, 7, 9, 0 ] ) {
				await page.evaluate( f => {
					globalThis.__equipmentGlowWrites = [];
					globalThis.__equipmentFixture = f;
				}, {
					refObjId: model.refObjId,
					inventory: [ { slot: 6, refObjId: id, plus, quantity: 1, typeFlags: 0, durability: 100 } ],
					avatars: [],
					moving: false
				} );
				await page.waitForFunction(
					( { id, plus } ) => {
						const actor = __equipmentActors?.find( a => a.gid === __playableRuntime.gameplay().localGid );
						return !__equipmentError &&
							actor?.model.includes( '"equipment":{"refObjId":' + id + ',"plus":' + plus + "}" );
					},
					{ id, plus },
					{ timeout: 30000 }
				);
				if ( plus >= 3 ) {
					await page.waitForFunction(
						() => __equipmentGlowWrites?.some( r => r.enabled && r.uv.some( v => v !== 0 ) ),
						null,
						{ timeout: 10000 }
					);
				}
				const writes = await page.evaluate( () => globalThis.__equipmentGlowWrites );
				evidence.cases.push( { prefix, enhancement: plus, id, writes } );
				await page.screenshot( { path: `${out}/${prefix}-enhancement-${plus}.png` } );
			}
		}
		for ( const prefix of [ "CH_M", "CH_W", "EU_M", "EU_W" ] ) {
			const [race, sex] = prefix.split( "_" ),
				model = roster.models.find( m =>
					m.codename.startsWith( `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_` )
				);
			const base = race === "CH" ? "ITEM_CH_SWORD_09_A" : "ITEM_EU_AXE_09_A",
				rare = byCode.get( base + "_RARE" ),
				normal = byCode.get( base );
			assert.ok( rare && normal );
			for (
				const [name, id, plus] of [ [ "rare", rare, 0 ], [ "rare-enhanced", rare, 9 ], [
					"normal-restored",
					normal,
					0
				] ]
			) {
				await page.evaluate( f => globalThis.__equipmentFixture = f, {
					refObjId: model.refObjId,
					inventory: [ { slot: 6, refObjId: id, plus, quantity: 1, typeFlags: 0, durability: 100 } ],
					avatars: [],
					moving: false
				} );
				const count = id === rare ?
					roster.dress.specialGlows[id].length * roster.dress.equipment[id].bodies[prefix].parts.length :
					0;
				await page.waitForFunction(
					( { id, plus, count } ) => {
						const local = __playableRuntime.gameplay().localGid,
							actor = __equipmentActors?.find( a => a.gid === local ),
							effects = __equipmentActors?.filter( a =>
								a.attachment?.gid === local && a.attachment.bone.startsWith( "equipment:" )
							);
						return !__equipmentError &&
							actor?.model.includes( '"equipment":{"refObjId":' + id + ',"plus":' + plus + "}" ) &&
							effects?.length === count && effects.every( a => a.time > .5 );
					},
					{ id, plus, count },
					{ timeout: 30000 }
				);
				const effects = await page.evaluate( () =>
					__equipmentActors.filter( a => a.attachment?.bone.startsWith( "equipment:" ) ).map( a => ({
						gid: a.gid,
						model: a.model,
						bone: a.attachment.bone,
						scale: a.attachment.modelScale,
						time: a.time
					}) )
				);
				evidence.cases.push( { prefix, specialState: name, id, plus, effects } );
				await page.screenshot( { path: `${out}/${prefix}-${name}.png` } );
			}
		}
		// Both native equipment slots, conceal/restore and Hwan transitions use the
		// production appearance transaction and the production particle lifetime owner.
		for ( const prefix of [ "CH_M", "CH_W", "EU_M", "EU_W" ] ) {
			const [race, sex] = prefix.split( "_" ),
				model = roster.models.find( m =>
					m.codename.startsWith( `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_` )
				);
			const weapon = byCode.get( (race === "CH" ? "ITEM_CH_SWORD_09_A" : "ITEM_EU_SWORD_09_A") + "_RARE" ),
				shield = byCode.get( `ITEM_${race}_SHIELD_09_A_RARE` );
			assert.ok( weapon && shield );
			const inventory = [ { slot: 6, refObjId: weapon, plus: 9 }, { slot: 7, refObjId: shield, plus: 9 } ];
			for (
				const [name, mountedOn, hwan] of [ [ "both", undefined, false ], [ "concealed", 999999, false ], [
					"restored",
					undefined,
					false
				], ...(race === "CH" ? [ [ "hwan", undefined, true ], [ "hwan-restored", undefined, false ] ] : []) ]
			) {
				await page.evaluate( f => globalThis.__equipmentFixture = f, {
					refObjId: model.refObjId,
					inventory,
					avatars: [],
					moving: false,
					mountedOn,
					hwan
				} );
				const count = mountedOn ?
					0 :
					inventory.reduce( ( n, i ) =>
						n +
						roster.dress.specialGlows[i.refObjId].length *
							roster.dress.equipment[i.refObjId].bodies[prefix].parts.length, 0 );
				await page.waitForFunction(
					( { count, hidden, weapon, shield, hair, hwan } ) => {
						const local = __playableRuntime.gameplay().localGid,
							actor = __equipmentActors?.find( a => a.gid === local ),
							effects = __equipmentActors?.filter( a =>
								a.attachment?.gid === local && a.attachment.bone.startsWith( "equipment:" )
							);
						return !__equipmentError && !!actor && effects?.length === count && effects.every( a =>
							a.time > .3
						) && (!hair || actor.model.includes( hair ) === hwan) && (hidden ?
							!actor.model.includes( '"equipment":' ) :
							[ weapon, shield ].every( id =>
								actor.model.includes( '"equipment":{"refObjId":' + id + "," )
							));
					},
					{ count, hidden: !!mountedOn, weapon, shield, hair: roster.dress.hwan?.[prefix]?.glb, hwan },
					{ timeout: 30000 }
				);
				const effects = await page.evaluate( () =>
					__equipmentActors.filter( a => a.attachment?.bone.startsWith( "equipment:" ) ).map( a => ({
						gid: a.gid,
						bone: a.attachment.bone,
						scale: a.attachment.modelScale
					}) )
				);
				evidence.cases.push( { prefix, specialTransition: name, effects } );
				await page.screenshot( { path: `${out}/${prefix}-special-${name}.png` } );
			}
		}
		assert.deepEqual( evidence.errors, [] );
		evidence.verdict = "PASS SUCCESS";
	} catch ( e ) {
		evidence.failure = String( e );
		evidence.runtime = await page.evaluate( () => ({
			error: globalThis.__equipmentError,
			fixture: globalThis.__equipmentFixture,
			actors: globalThis.__equipmentActors?.map( a => ({ gid: a.gid, model: a.model, clip: a.clip }) )
		}) ).catch( () => null );
		throw e;
	} finally {
		await page.evaluate( () => globalThis.__playableRuntime?.session( { kind: "logout" } ) ).catch( () => {} );
		await browser.close();
		await writeFile( out + "/incident.json", JSON.stringify( evidence, null, 2 ) );
	}
} );
