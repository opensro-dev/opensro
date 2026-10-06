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
