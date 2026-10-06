/*
===========================================================================

scenarios.mjs - the input each benchmark scenario drives

Each drive takes the page and more(), true while the measured span lasts,
and moves the client the way a player would: the camera drag, walking,
skills on a monster, a region crossing. Each checks that it did what it
says, so a dead character or a refused command cannot pass as fast.

===========================================================================
*/

const CROSS_TIMEOUT_MS = 30000;

/*
================
keepGoing

Drives nothing but waits, in short steps, while more is true.
================
*/
export async function keepGoing( page, more ) {
	while ( more() ) await page.waitForTimeout( 20 );
}

/*
================
drag
================
*/
export async function drag( page, more ) {
	const box = await page.evaluate( () => {
		const r = document.querySelector( "canvas" ).getBoundingClientRect();
		return { x: r.x, y: r.y, width: r.width, height: r.height };
	} );
	const y = box.y + box.height * .45, left = box.x + box.width * .15, right = box.x + box.width * .85;
	let x = box.x + box.width / 2, step = 6;
	await page.mouse.move( x, y );
	await page.mouse.down( { button: "right" } );
	try {
		while ( more() ) {
			x += step;
			if ( x > right || x < left ) step = -step;
			await page.mouse.move( x, y );
			await page.waitForTimeout( 8 );
		}
	} finally {
		await page.mouse.up( { button: "right" } );
	}
}

/*
================
walk

Walks between the start and a point 140 units west, turning every second
and a half.
================
*/
export async function walk( page, more ) {
	const start = await page.evaluate( () => globalThis.__benchRuntime.gameplay().pose );
	let travelled = 0, last = start;
	const ends = [ { ...start, x: start.x - 140 }, start ];
	for ( let leg = 0; more(); leg++ ) {
		await page.evaluate(
			destination => {
				const root = globalThis.__benchRuntime, game = root.gameplay();
				if ( globalThis.__benchInputs ) {
					const body = root.characterActors().find( actor => actor.gid === game.localGid );
					globalThis.__benchInputs.push( {
						atMs: performance.now(),
						revision: game.movementRevision,
						from: { ...(body?.pose ?? game.pose) },
						destination
					} );
					if ( globalThis.__benchInputs.length > 128 ) globalThis.__benchInputs.shift();
				}
				root.session( { kind: "gameplay", command: { kind: "move", destination } } );
			},
			ends[leg % 2]
		);
		const legStart = Date.now();
		while ( more() && Date.now() - legStart < 1500 ) {
			await page.waitForTimeout( 100 );
			const pose = await page.evaluate( () => globalThis.__benchRuntime.gameplay().pose );
			travelled += Math.hypot( pose.x - last.x, pose.z - last.z );
			last = pose;
		}
	}
	if ( travelled < 50 ) throw Error( `the move scenario walked only ${travelled.toFixed( 0 )} units` );
}

/*
================
approach

Before the skill scenario is measured: walks to within reach of the
nearest live monster.
================
*/
export async function approach( page ) {
	const distance = () =>
		page.evaluate( () => {
			const root = globalThis.__benchRuntime, pose = root.gameplay().pose;
			const world = e => [ (e.regionId & 255) * 1920 + e.x, (e.regionId >>> 8) * 1920 + e.z ];
			const here = world( pose );
			const nearest = root.entities().filter( e => e.kind === "monster" && e.appearanceState?.[0] !== 2 ).map(
				e => ({ gid: e.gid, d: Math.hypot( world( e )[0] - here[0], world( e )[1] - here[1] ) })
			).sort( ( a, b ) => a.d - b.d )[0];
			return nearest ?? null;
		} );
	// The native attack command pursues its target; it is the approach.
	// Earlier runs may have killed the nearby monsters; they respawn.
	for ( let attempt = 0; attempt < 12; attempt++ ) {
		const nearest = await distance();
		if ( !nearest ) {
			await page.waitForTimeout( 5000 );
			continue;
		}
		if ( nearest.d < 80 ) return;
		await page.evaluate(
			gid => globalThis.__benchRuntime.session( { kind: "gameplay", command: { kind: "attack", gid } } ),
			nearest.gid
		);
		await page.waitForTimeout( 5000 );
	}
	const nearest = await distance();
	if ( !nearest ) {
		console.log(
			"  entities:",
			JSON.stringify(
				await page.evaluate( () => {
					const kinds = {};
					for ( const e of globalThis.__benchRuntime.entities() ) {
						const k = `${e.kind}:${e.appearanceState?.[0] ?? "-"}`;
						kinds[k] = (kinds[k] ?? 0) + 1;
					}
					return kinds;
				} )
			)
		);
	}
	if ( !nearest || nearest.d > 400 ) throw Error( `could not reach a monster: ${JSON.stringify( nearest )}` );
}

/*
================
fight

Every 700 ms, a learned skill (in turn) and an attack on the nearest live
monster within reach; skills that need no target still cast.
================
*/
export async function fight( page, more ) {
	const tally = { skills: 0, targets: 0, turns: 0 };
	for ( let turn = 0; more(); turn++ ) {
		const done = await page.evaluate( turn => {
			const root = globalThis.__benchRuntime, game = root.gameplay(), pose = game.pose;
			const world = e => [ (e.regionId & 255) * 1920 + e.x, (e.regionId >>> 8) * 1920 + e.z ];
			const here = world( pose );
			const target = root.entities().filter( e => e.kind === "monster" && e.appearanceState?.[0] !== 2 ).map(
				e => ({ gid: e.gid, d: Math.hypot( world( e )[0] - here[0], world( e )[1] - here[1] ) })
			).filter( e => e.d < 400 ).sort( ( a, b ) => a.d - b.d )[0];
			const skills = (game.skills ?? []).map( s => s.id ?? s ).filter( id => Number.isInteger( id ) );
			if ( skills.length ) {
				const skillId = skills[turn % skills.length];
				root.session( {
					kind: "gameplay",
					command: target ? { kind: "skill", skillId, gid: target.gid } : { kind: "skill", skillId }
				} );
			}
			if ( target ) root.session( { kind: "gameplay", command: { kind: "attack", gid: target.gid } } );
			return { skill: skills.length > 0, target: !!target };
		}, turn );
		tally.turns++;
		tally.skills += Number( done.skill );
		tally.targets += Number( done.target );
		const turnStart = Date.now();
		while ( more() && Date.now() - turnStart < 700 ) await page.waitForTimeout( 20 );
	}
	console.log( `  skill turns ${tally.turns}, with a skill ${tally.skills}, with a target ${tally.targets}` );
	if ( !tally.skills || !tally.targets ) throw Error( "the skill scenario had no skill or no target" );
}

/*
================
cross

Walks to the fixture's destination across the east region boundary and
returns the run's statistics from the walk's start to its arrival.
================
*/
export async function cross( page, fixture ) {
	// Leave any fight the skill scenario started before walking.
	await page.evaluate( () => globalThis.__benchRuntime.session( { kind: "gameplay", command: { kind: "cancel" } } ) );
	await page.waitForTimeout( 500 );
	await page.evaluate(
		target => {
			// A movement destination is a whole pose; the walk keeps the rest of it.
			const destination = { ...globalThis.__benchRuntime.gameplay().pose, ...target };
			globalThis.__benchRuntime.session( { kind: "gameplay", command: { kind: "move", destination } } );
		},
		fixture.destination
	);
	const started = Date.now();
	return async more => {
		while ( Date.now() - started < CROSS_TIMEOUT_MS ) {
			const arrived = await page.evaluate( destination => {
				const p = globalThis.__benchRuntime.gameplay().pose;
				return p.regionId === destination.regionId &&
					Math.hypot( p.x - destination.x, p.z - destination.z ) < 8;
			}, fixture.destination );
			if ( arrived ) return;
			await page.waitForTimeout( 50 );
			void more;
		}
		const pose = await page.evaluate( () => {
			const game = globalThis.__benchRuntime.gameplay();
			return { pose: game.pose, moving: game.moving, health: game.vitals?.find( v => v.gid === game.localGid ) };
		} );
		throw Error( `region crossing did not arrive: ${JSON.stringify( pose )}` );
	};
}

// Units around the character that hold the loaded combat scene. GM-loaded
// monsters appear on the GM's point (SR_GameServer 520A40) and fight there.
const COMBAT_RADIUS = 150;
const COMBAT_LOAD_TIMEOUT_MS = 15000;
// Bound on fighting the scene down after the window; past it, restart instead.
const COMBAT_CLEAR_TIMEOUT_MS = 180000;
const COMBAT_TURN_MS = 700;
const COMBAT_SAMPLE_MS = 100;

/*
================
gmCommand
================
*/
function gmCommand( page, line ) {
	return page.evaluate(
		line => globalThis.__benchRuntime.session( { kind: "gameplay", command: { kind: "gm-command", line } } ),
		line
	);
}

/*
================
sceneMonsters

Every live monster within COMBAT_RADIUS of the character: gid, reference,
distance.
================
*/
function sceneMonsters( page ) {
	return page.evaluate( radius => {
		const root = globalThis.__benchRuntime, pose = root.gameplay().pose;
		const world = e => [ (e.regionId & 255) * 1920 + e.x, (e.regionId >>> 8) * 1920 + e.z ];
		const here = world( pose );
		return root.entities().filter( e => e.kind === "monster" && e.appearanceState?.[0] !== 2 ).map( e => ({
			gid: e.gid,
			refObjId: e.refObjId,
			d: Math.hypot( world( e )[0] - here[0], world( e )[1] - here[1] )
		}) ).filter( e => e.d <= radius );
	}, COMBAT_RADIUS );
}

/*
================
monsterReference

The requested codename's reference id from the client's published monster
manifest, the same rows the renderer resolves models from.
================
*/
function monsterReference( page, codename ) {
	return page.evaluate( async codename => {
		const manifest = await (await fetch( "/assets/npc/manifest.json" )).json();
		const row = Object.values( manifest.models ).find( m => m.codename === codename && m.kind === "monster" );
		return row ? row.refObjId : null;
	}, codename );
}

/*
================
loadCombat

Loads the repeatable combat scene with the native GM commands: /INVINCIBLE
unless the scene is vulnerable, then /LOADMONSTER codename count type at the
character's feet. The scene is exactly count new monsters of the requested
reference; the ambient population within the radius is recorded beside it.
Positions are not repeatable (the monsters run on their AI); codename and
count are. Returns the scene with its gids.
================
*/
export async function loadCombat( page, scene ) {
	const refObjId = await monsterReference( page, scene.codename );
	if ( refObjId === null ) throw Error( `no monster ${scene.codename} in the published manifest` );
	const ambient = await sceneMonsters( page ), before = new Set( ambient.map( m => m.gid ) );
	if ( !scene.vulnerable ) await gmCommand( page, "/INVINCIBLE" );
	await gmCommand( page, `/LOADMONSTER ${scene.codename} ${scene.count} ${scene.type}` );
	const started = Date.now();
	let gids = [];
	while ( Date.now() - started < COMBAT_LOAD_TIMEOUT_MS ) {
		gids = (await sceneMonsters( page )).filter( m => !before.has( m.gid ) && m.refObjId === refObjId ).map(
			m => m.gid
		);
		if ( gids.length >= scene.count ) break;
		await page.waitForTimeout( 250 );
	}
	if ( gids.length !== scene.count ) {
		throw Error(
			`/LOADMONSTER ${scene.codename} ${scene.count}: ${gids.length} of reference ${refObjId} appeared`
		);
	}
	return { ...scene, refObjId, gids, ambient: ambient.length };
}

/*
================
castEvidence

Every server cast in the gameplay view (positive tokens; predictions do not
count): its caster and, per target, the damage its results carry so far.
results covers every target of a multi-target skill; later result stages
append to the same token.
================
*/
function castEvidence( page ) {
	return page.evaluate( () => {
		const game = globalThis.__benchRuntime.gameplay();
		const sum = impacts => (impacts ?? []).reduce( ( total, i ) => total + (i.damage ?? 0), 0 );
		return (game.casts ?? []).filter( c => c.token > 0 ).map( c => ({
			token: c.token,
			caster: c.caster,
			targets: c.results?.length ?
				c.results.map( r => ({ target: r.target, damage: sum( r.impacts ) }) ) :
				[ { target: c.target, damage: sum( c.impacts ) } ]
		}) );
	} );
}

/*
================
damageTo

A cast's damage so far to the targets accepted by keep.
================
*/
function damageTo( cast, keep ) {
	return cast.targets.reduce( ( total, t ) => total + (keep( t.target ) ? t.damage : 0), 0 );
}

/*
================
strike

One combat turn: the next skill and a basic attack on the nearest living
monster of gids.
================
*/
function strike( page, gids, turn ) {
	return page.evaluate( ( { turn, gids } ) => {
		const root = globalThis.__benchRuntime, game = root.gameplay(), ids = new Set( gids );
		const world = e => [ (e.regionId & 255) * 1920 + e.x, (e.regionId >>> 8) * 1920 + e.z ];
		const here = world( game.pose );
		const target = root.entities().filter( e => ids.has( e.gid ) && e.appearanceState?.[0] !== 2 ).map(
			e => ({ gid: e.gid, d: Math.hypot( world( e )[0] - here[0], world( e )[1] - here[1] ) })
		).sort( ( a, b ) => a.d - b.d )[0];
		const skills = (game.skills ?? []).map( s => s.id ?? s ).filter( id => Number.isInteger( id ) );
		if ( target && skills.length ) {
			root.session( {
				kind: "gameplay",
				command: { kind: "skill", skillId: skills[turn % skills.length], gid: target.gid }
			} );
		}
		if ( target ) root.session( { kind: "gameplay", command: { kind: "attack", gid: target.gid } } );
	}, { turn, gids } );
}

/*
================
deathsInView

The scene gids the client is showing dead right now: positive evidence of
a kill, which a monster that merely left view range never gives.
================
*/
function deathsInView( page, gids ) {
	return page.evaluate( gids => {
		const ids = new Set( gids );
		return globalThis.__benchRuntime.entities().filter( e => ids.has( e.gid ) && e.appearanceState?.[0] === 2 )
			.map( e => e.gid );
	}, gids );
}

/*
================
clearCombat

Fights the GM-loaded scene to the end. Those monsters have no respawning
nest and nothing else despawns them, so a session that leaves them alive
changes every later measurement on the shared server. Stops when none is
left inside the fight radius or the bound runs out, and returns the gids
it saw die; the caller decides the residue.
================
*/
export async function clearCombat( page, scene ) {
	const dead = new Set();
	const living = async () => {
		const alive = new Set( (await sceneMonsters( page )).map( m => m.gid ) );
		return scene.gids.filter( gid => alive.has( gid ) ).length;
	};
	const until = Date.now() + COMBAT_CLEAR_TIMEOUT_MS;
	for ( let turn = 0; Date.now() < until && await living() > 0; turn++ ) {
		await strike( page, scene.gids, turn );
		// Sample through the turn: a corpse leaves view soon after it falls.
		for ( let wait = 0; wait < COMBAT_TURN_MS; wait += COMBAT_SAMPLE_MS ) {
			for ( const gid of await deathsInView( page, scene.gids ) ) dead.add( gid );
			await page.waitForTimeout( COMBAT_SAMPLE_MS );
		}
	}
	for ( const gid of await deathsInView( page, scene.gids ) ) dead.add( gid );
	return [ ...dead ];
}

/*
================
combat

fight against the loaded scene only. Evidence is measured against the cast
tokens already in view when the window starts, never a clock: casts in the
gameplay view carry simulation time, a different origin from the page's.
The window counts only if the server accepted local casts that dealt
damage, a vulnerable scene also hit the character, and every scene monster
is alive within COMBAT_RADIUS at its end; otherwise it throws. Returns the
window's evidence, which the bench stores with the result.
================
*/
export async function combat( page, more, scene ) {
	const local = await page.evaluate( () => globalThis.__benchRuntime.gameplay().localGid );
	const scenery = new Set( scene.gids ), latest = new Map();
	const outgoing = cast => damageTo( cast, target => scenery.has( target ) );
	const incoming = cast => damageTo( cast, target => target === local );
	const baseline = new Map( (await castEvidence( page )).map( c => [ c.token, c ] ) );
	let turns = 0;
	for ( let turn = 0; more(); turn++ ) {
		await strike( page, scene.gids, turn );
		turns++;
		const turnStart = Date.now();
		// Sample while waiting: a short cast can leave the view between turns.
		while ( more() && Date.now() - turnStart < 700 ) {
			for ( const cast of await castEvidence( page ) ) latest.set( cast.token, cast );
			await page.waitForTimeout( 20 );
		}
	}
	for ( const cast of await castEvidence( page ) ) latest.set( cast.token, cast );
	const evidence = { turns, acceptedCasts: 0, damage: 0, incomingCasts: 0, incomingDamage: 0 };
	for ( const cast of latest.values() ) {
		// A cast already in view at the start counts only its new result stages.
		const before = baseline.get( cast.token );
		if ( cast.caster === local ) {
			if ( !before ) evidence.acceptedCasts++;
			evidence.damage += outgoing( cast ) - (before ? outgoing( before ) : 0);
		}
		if ( scenery.has( cast.caster ) ) {
			if ( !before ) evidence.incomingCasts++;
			evidence.incomingDamage += incoming( cast ) - (before ? incoming( before ) : 0);
		}
	}
	const alive = new Set( (await sceneMonsters( page )).map( m => m.gid ) );
	evidence.alive = scene.gids.filter( gid => alive.has( gid ) ).length;
	console.log(
		`  combat ${scene.codename} x${scene.count} ${scene.type}${
			scene.vulnerable ? " vulnerable" : " invincible"
		}: ` +
			`turns ${evidence.turns}, accepted casts ${evidence.acceptedCasts}, damage ${evidence.damage}, ` +
			`incoming casts ${evidence.incomingCasts}, incoming damage ${evidence.incomingDamage}, ` +
			`alive ${evidence.alive}/${scene.count}, ambient ${scene.ambient}`
	);
	if ( !evidence.acceptedCasts || evidence.damage <= 0 ) {
		throw Error( "the combat window had no accepted, damaging cast" );
	}
	if ( scene.vulnerable && evidence.incomingDamage <= 0 ) {
		throw Error( "the vulnerable combat window took no incoming damage" );
	}
	if ( evidence.alive !== scene.count ) throw Error( "the combat scene drifted: a monster died or left the radius" );
	return evidence;
}
