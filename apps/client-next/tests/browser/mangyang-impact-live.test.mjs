import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile, readFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { bootPlayableSession } from "./helpers/playable-session.mjs";

// Uses an ordinary equipped scratch character and real server outcomes. The
// route wrappers observe production traffic/render actors; they replace no result.
test( "Mangyang impacts reach renderer and retain authoritative critical flags", { timeout: 180000 }, async () => {
	const arrow = process.env.SRO_HIT_SKILL === "critical-arrow";
	const out = "temp/artifacts/mangyang-impact/" + (process.env.SRO_HIT_CAPTURE ?? "current");
	await mkdir( out, { recursive: true } );
	const criticalPaths = new Set(
		JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/audio/effectsound.json", "utf8" ) ).rules.filter( r =>
			r.object === "PLAYER" && r.handle === "SND_CRIDMG"
		).map( r => r.publicPath ).filter( Boolean )
	);
	const { browser, page } = await launchProbeBrowser(), report = { wire: [], samples: [], errors: [] };
	page.on( "pageerror", e => report.errors.push( String( e ) ) );
	page.on( "console", m => {
		if ( m.text().startsWith( "[hit-wire]" ) ) report.wire.push( JSON.parse( m.text().slice( 10 ) ) );
	} );
	try {
		await page.addInitScript( () => {
			globalThis.__hitSounds = [];
			globalThis.__hitAudioStarts = [];
		} );
		await page.route( "**/runtime/audio/audio.ts*", async route => {
			const response = await route.fetch(), source = await response.text();
			assert.ok( source.includes( "source.start();" ) );
			await route.fulfill( {
				response,
				body: source.replace(
					"source.start();",
					`source.start();globalThis.__hitAudioStarts.push({at:performance.now(),id,path:event.path,duration:buffer.duration});`
				),
				contentType: "application/javascript"
			} );
		} );
		await page.route( "**/characters/sounds/sounds.ts*", async route => {
			const response = await route.fetch(), source = await response.text();
			assert.ok( source.includes( "export function createCharacterSounds(" ) );
			const body = source.replace( "export function createCharacterSounds(", "function createObservedSounds(" ) +
				`\nexport function createCharacterSounds(play,...args){return createObservedSounds(event=>{globalThis.__hitSounds.push({at:performance.now(),...event});play(event)},...args);}`;
			await route.fulfill( { response, body, contentType: "application/javascript" } );
		} );
		await page.route( "**/runtime/simulation/worker/network/network.ts*", async route => {
			const response = await route.fetch();
			let body = await response.text();
			const anchor = "const frame = codec.decode(new Uint8Array(event.data));";
			assert.ok( body.includes( anchor ) );
			body = body.replace(
				anchor,
				anchor +
					`if([0xb245,0xb505,0xb070,0x3752,0x33a6].includes(frame.opcode))console.log('[hit-wire]'+JSON.stringify({at:performance.now(),opcode:frame.opcode,payload:Array.from(frame.payload)}));`
			);
			await route.fulfill( { response, body, contentType: "application/javascript" } );
		} );
		await page.route( "**/src/engine/runtime/renderer/renderer.ts*", async route => {
			const response = await route.fetch(), source = await response.text();
			assert.ok( source.includes( "export function createRenderer(" ) );
			const body = source.replace( "export function createRenderer(", "function createObservedRenderer(" ) +
				`\nexport function createRenderer(...args){const owner=createObservedRenderer(...args);globalThis.__hitRenderer=owner;return {...owner,setCharacterActors(actors){globalThis.__hitActors=actors;owner.setCharacterActors(actors)}};}`;
			await route.fulfill( { response, body, contentType: "application/javascript" } );
		} );
		console.log( "[hit] authenticated scratch boot" );
		await bootPlayableSession( page, process.env.SRO_HIT_CHARACTER ?? "asd3" );
		await page.waitForFunction( () =>
			globalThis.__hitActors?.length && __playableRuntime.gameplay()?.inventory?.some( i => i.slot === 6 )
		);
		await page.context().tracing.start( { screenshots: true, snapshots: true } );
		report.initial = await page.evaluate( () => {
			const g = __playableRuntime.gameplay();
			return {
				gid: g.localGid,
				pose: g.pose,
				weapon: g.inventory.find( i => i.slot === 6 ),
				ammo: g.inventory.find( i => i.slot === 7 ),
				mp: g.vitals.find( v => v.gid === g.localGid )?.mp,
				skills: g.skills,
				criticalSkill: g.skillCatalog.find( s => s.name === "SKILL_CH_BOW_CRITICAL_A_01" )?.id
			};
		} );
		if ( arrow ) {
			assert.ok(
				report.initial.skills.includes( report.initial.criticalSkill ),
				"authored critical skill must be learned"
			);
			assert.equal( report.initial.weapon.typeFlags >>> 11, 6, "equipped bow required" );
			assert.ok( report.initial.ammo?.quantity >= 12, "equipped arrows required" );
		}
		for ( let fight = 0; fight < (process.env.SRO_HIT_CAPTURE === "before" ? 2 : 12); fight++ ) {
			let target;
			for ( let turn = 0; turn < 10 && !target; turn++ ) {
				target = await page.evaluate( () => {
					const local = __playableRuntime.gameplay().localGid;
					for ( let y = .2; y < .75; y += .025 ) {
						for ( let x = .03; x < .97; x += .025 ) {
							if (
								document.elementFromPoint( x * innerWidth, y * innerHeight )?.closest( "[data-ui-id]" )
							) continue;
							const gid = __hitRenderer.pickEntity( x, y, local ),
								e = gid && __playableRuntime.entity( gid );
							if (
								e?.kind === "monster" && e.name === "Mangyang" && e.appearanceState?.[0] !== 2 &&
								__hitActors.some( a => a.gid === gid )
							) return { gid, x, y };
						}
					}
					return null;
				} );
				if ( !target ) {
					await page.mouse.move( 500, 350 );
					await page.mouse.down( { button: "right" } );
					await page.mouse.move( 640, 350, { steps: 5 } );
					await page.mouse.up( { button: "right" } );
					await page.waitForTimeout( 150 );
				}
			}
			assert.ok( target, "visible model-loaded Mangyang required" );
			console.log( "[hit] fight", fight, target.gid );
			if ( arrow ) {
				await page.evaluate(
					( { gid, skillId } ) =>
						__playableRuntime.session( { kind: "gameplay", command: { kind: "skill", gid, skillId } } ),
					{
						gid: target.gid,
						skillId: report.initial.criticalSkill
					}
				);
			} else await page.mouse.dblclick( target.x * 1024, target.y * 768, { delay: 100 } );
			for ( let i = 0; i < 180; i++ ) {
				const sample = await page.evaluate( gid => {
					const g = __playableRuntime.gameplay();
					return {
						at: performance.now(),
						casts: g.casts,
						pose: g.pose,
						target: gid,
						entity: __playableRuntime.entity( gid ),
						effects: __hitActors.filter( a => a.gid < 0 ).map( a => a.model ),
						effectActors: __hitActors.filter( a => a.gid < 0 ).map( a => ({
							gid: a.gid,
							model: a.model,
							pose: a.pose,
							time: a.time
						}) ),
						error: g.error
					};
				}, target.gid );
				report.samples.push( sample );
				const criticalCast = sample.casts.find( c =>
					c.caster === report.initial.gid && c.target === target.gid &&
					c.impacts?.some( hit => hit.flags & 2 )
				);
				if (
					criticalCast && sample.effects.some( p => /hit_3_critical/.test( p ) ) && !report.criticalScreenshot
				) {
					await page.screenshot( { path: out + "/critical.png" } );
					report.criticalScreenshot = true;
				}
				if (
					arrow && sample.casts.some( c =>
						c.caster === report.initial.gid && c.skill === report.initial.criticalSkill &&
						c.shotAtMs !== undefined && c.cancelledAtMs === undefined
					) && sample.effects.some( p =>
						/cha_arrow_normal/.test( p )
					) && !report.projectileScreenshot
				) {
					await page.screenshot( { path: out + "/projectile.png" } );
					report.projectileScreenshot = true;
				}
				if (
					sample.effects.some( p => /blood/.test( p ) ) && !report.bloodScreenshot &&
					(!arrow || sample.casts.some( c =>
						c.caster === report.initial.gid && c.impacts?.some( i => i.flags & 2 )
					))
				) {
					if ( !arrow ) await page.waitForTimeout( 100 );
					await page.screenshot( { path: out + "/blood.png" } );
					report.bloodScreenshot = true;
				}
				// Server death can precede the authored animation contact. Keep observing
				// the critical through its actual audio dispatch before ending the window.
				if ( i > 15 && (!sample.entity || sample.entity.appearanceState?.[0] === 2) ) {
					const criticalPresented = !criticalCast ||
						await page.evaluate(
							prefix => __hitAudioStarts.some( s => s.id?.startsWith( prefix ) ),
							`impact:${criticalCast.token}:${target.gid}:`
						);
					if ( criticalPresented ) break;
				}
				if (
					arrow && i > 25 &&
					!sample.casts.some( c => c.caster === report.initial.gid && c.cancelledAtMs === undefined )
				) break;
				await page.waitForTimeout( 65 );
			}
			const casts = report.samples.flatMap( s => s.casts ).filter( c => c.caster === report.initial.gid );
			const played = await page.evaluate( () => __hitAudioStarts );
			if (
				casts.some( c => c.results?.some( r => r.impacts.some( i => i.flags & 2 ) ) ) &&
				report.bloodScreenshot && played.some( s => criticalPaths.has( s.path ) )
			) break;
			if ( arrow ) await page.waitForTimeout( 4200 );
		}
		const models = [ ...new Set( report.samples.flatMap( s => s.effects ) ) ];
		report.models = models;
		Object.assign(
			report,
			await page.evaluate( () => ({
				sounds: __hitSounds,
				audioStarts: __hitAudioStarts,
				status: document.querySelector( "output" )?.textContent
			}) )
		);
		const casts = [
			...new Map(
				report.samples.flatMap( s => s.casts ).filter( c => c.caster === report.initial.gid ).map(
					c => [ c.token, c ]
				)
			).values()
		];
		report.casts = casts;
		assert.ok( casts.length, "real server must accept an attack" );
		if ( process.env.SRO_HIT_CAPTURE !== "before" ) {
			assert.ok( models.some( p => /hit_2_.*blood/.test( p ) ), "authored blood reaches render actors" );
			assert.ok( models.some( p => /hit_3_/.test( p ) ), "selected primary impact reaches render actors" );
			assert.ok(
				casts.some( c => c.results?.some( r => r.impacts.some( i => i.flags & 2 ) ) ),
				"server produces a real critical result"
			);
			const u32 = ( bytes, at ) => new DataView( Uint8Array.from( bytes ).buffer ).getUint32( at, true );
			for ( const c of casts ) {
				for ( const target of c.results ?? [] ) {
					for ( const [index, hit] of target.impacts.entries() ) {
						if ( hit.flags & 2 ) {
							const frame = arrow ?
								report.wire.find( f =>
									f.opcode === 0xb505 && f.payload[0] === 1 && f.payload.length >= 25 &&
									u32( f.payload, 1 ) === c.token && f.payload[11] === 1 &&
									u32( f.payload, 12 ) === target.target
								) :
								report.wire.find( f =>
									f.opcode === 0xb245 && f.payload.length >= 34 && u32( f.payload, 10 ) === c.token &&
									f.payload[20] === 1 && u32( f.payload, 21 ) === target.target
								);
							assert.ok( frame, "same cast and victim must exist on the actual socket" );
							assert.equal(
								frame.payload[(arrow ? 17 : 26) + 9 * index],
								hit.flags,
								"wire critical flag survives result decoding"
							);
						}
					}
				}
			}
			const criticalIds = new Set(
				casts.flatMap( c =>
					(c.results ?? []).flatMap( r =>
						r.impacts.flatMap( ( hit, i ) => hit.flags & 2 ? [ `impact:${c.token}:${r.target}:${i}` ] : [] )
					)
				)
			);
			assert.ok(
				report.sounds.some( s => criticalIds.has( s.id ) && criticalPaths.has( s.path ) ),
				"matching critical sound reaches production audio owner"
			);
			assert.ok(
				report.audioStarts.some( s => criticalIds.has( s.id ) && criticalPaths.has( s.path ) ),
				"matching critical buffer actually starts"
			);
		}
		if ( arrow ) {
			const starts = report.wire.filter( f =>
				f.opcode === 0xb245 && f.payload[0] === 1 &&
				new DataView( Uint8Array.from( f.payload ).buffer ).getUint32( 2, true ) ===
					report.initial.criticalSkill
			);
			assert.ok( starts.length, "critical arrow starts on socket" );
			for ( const f of starts ) assert.equal( f.payload.length, 19, "no early damage in start" );
			const localTokens = new Set(
				starts.map( f => new DataView( Uint8Array.from( f.payload ).buffer ).getUint32( 10, true ) )
			);
			const releases = report.wire.filter( f =>
				f.opcode === 0xb505 && f.payload[0] === 1 &&
				localTokens.has( new DataView( Uint8Array.from( f.payload ).buffer ).getUint32( 1, true ) )
			);
			const debits = report.wire.filter( f => f.opcode === 0x3752 );
			assert.equal( debits.length, releases.length, "one private debit per released shot" );
			debits.forEach( ( f, i ) =>
				assert.equal(
					new DataView( Uint8Array.from( f.payload ).buffer ).getUint16( 0, true ),
					report.initial.ammo.quantity - i - 1,
					"no cancelled or duplicate debit"
				)
			);
			const paths = new Map();
			for ( const sample of report.samples ) {
				for ( const actor of sample.effectActors ?? [] ) {
					if ( actor.model.includes( "cha_arrow_normal" ) ) {
						let positions = paths.get( actor.gid );
						if ( !positions ) {
							positions = new Set();
							paths.set( actor.gid, positions );
						}
						positions.add( JSON.stringify( actor.pose ) );
					}
				}
			}
			assert.ok(
				[ ...paths.values() ].some( positions => positions.size > 1 ),
				"same projectile actor must move across rendered samples"
			);
			assert.ok( report.projectileScreenshot, "projectile must reach rendered actors" );
		}
		assert.deepEqual( report.errors, [] );
		report.verdict = "PASS";
	} catch ( error ) {
		report.failure = String( error );
		await page.screenshot( { path: out + "/failure.png" } ).catch( () => {} );
		throw error;
	} finally {
		await page.context().tracing.stop( { path: out + "/trace.zip" } ).catch( () => {} );
		await writeFile( out + "/incident.json", JSON.stringify( report, null, 2 ) );
		await browser.close();
	}
} );
