/*
===========================================================================

combat-cleanup.mjs - bounded retirement of a benchmark's own loaded monsters

Only the recorded scene GIDs may be attacked. A missing row in a truncated
server snapshot is unknown, and a client death never overrides server HP.

===========================================================================
*/
const CLEANUP_LIMIT_MS = 180000;
const CLEANUP_TURN_MS = 700;

/*
================
combatResidue

Classify authoritative rows without treating a missing observation as a kill.
================
*/
export function combatResidue( scene, population ) {
	const alive = [], unknown = [], dead = [];
	const gids = new Set( scene.gids );
	for ( const gid of gids ) {
		const row = population.monsters.get( gid );
		if ( row && Number.isFinite( row.hp ) ) {
			if ( row.hp > 0 ) alive.push( gid );
			else dead.push( gid );
		} else if ( row || population.truncated ) unknown.push( gid );
	}
	return { alive, unknown, dead, unaccounted: Math.max( 0, scene.count - gids.size ) };
}

/*
================
cleanupCombat

The caller owns browser and server I/O. This loop runs only after measurement,
uses ordinary attacks, and returns a failed cleanup without masking a rejected
measurement. It never respawns, toggles immunity, or touches unrelated monsters.
================
*/
export async function cleanupCombat( options ) {
	const { scene, read, attack, isAlive, pause, now = Date.now, limitMs = CLEANUP_LIMIT_MS } = options;
	const started = now();
	const confirmedDead = new Set();
	let rounds = 0,
		residue = { alive: [], unknown: [ ...scene.gids ], dead: [], unaccounted: scene.count - scene.gids.length };
	while ( now() - started < limitMs ) {
		residue = combatResidue( scene, await read( limitMs - (now() - started) ) );
		for ( const gid of residue.dead ) confirmedDead.add( gid );
		for ( const gid of residue.alive ) confirmedDead.delete( gid );
		residue.unknown = residue.unknown.filter( gid => !confirmedDead.has( gid ) );
		residue.dead = [ ...confirmedDead ];
		if ( !residue.alive.length && !residue.unknown.length && !residue.unaccounted ) {
			return { status: "clean", rounds, elapsedMs: now() - started, ...residue };
		}
		if ( !residue.alive.length && residue.unaccounted ) {
			return { status: "unaccounted", rounds, elapsedMs: now() - started, ...residue };
		}
		if ( now() - started >= limitMs ) break;
		if ( !await isAlive() ) return { status: "character-dead", rounds, elapsedMs: now() - started, ...residue };
		if ( residue.alive.length ) await attack( residue.alive, rounds++ );
		const remaining = limitMs - (now() - started);
		if ( remaining > 0 ) await pause( Math.min( CLEANUP_TURN_MS, remaining ) );
	}
	return { status: "timeout", rounds, elapsedMs: now() - started, ...residue };
}
