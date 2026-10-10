/*
===========================================================================

server-tick-bench.mjs - GameWorld tick cost under an admitted crowd

Usage:
  node tools/perf/bench/server-tick-bench.mjs [--count 20] [--warmup 30]
       [--seconds 120] [--skills 64] [--metrics http://127.0.0.1:8788]
       [--out FILE.json]

Admits a crowd of authenticated peers on the LOCAL development stack, each
with its own level-90 loadout of skills, then reads the GameWorld's
/transport/metrics before and after a measured window. The production lag
(2026-10-11) came from per-tick skill lookups over every learned skill of
every player; peers that all know the same few skills would never reach
it, so each peer learns a different rank of the same skill groups.

The window reports ticks, the overrun rate, the window's mean tick and,
when the server serves tick_ms_histogram, its p50/p90/p99/p999. Run it
on the same stack before and after a change; compare ratios, not a
Windows number against the Linux production host.

===========================================================================
*/
import path from "node:path";
import { readFile, readdir, writeFile, mkdir } from "node:fs/promises";
import { setTimeout as delay } from "node:timers/promises";
import { parseOptions } from "../core/report.mjs";
import { createCrowd } from "../core/crowd.mjs";
import { MISSION_MOVEMENT_FIXTURES } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { serverGameDataRoot } from "../../../../../scripts/build/world/paths.mjs";

const USAGE =
	"server-tick-bench.mjs [--count 20] [--warmup 30] [--seconds 120] [--skills 64] [--metrics URL] [--out FILE.json]";
// The fixture reset bounds (benchmark_fixture.go): one request teaches at
// most 64 skills, at most the v1.150 level cap (progression.LevelCap 90);
// intellect 500 pays any of them.
const LOADOUT_LEVEL = 90;
const LOADOUT_INTELLECT = 500;
const MAX_LOADOUT_SKILLS = 64;
// skilldata rows: service, id, group id, codename (tab-separated, UTF-16).
const SKILL_SERVICE = 0, SKILL_ID = 1, SKILL_GROUP = 2, SKILL_CODENAME = 3;

const options = parseOptions( process.argv.slice( 2 ), {
	count: 20,
	warmup: 30,
	seconds: 120,
	skills: MAX_LOADOUT_SKILLS,
	metrics: "http://127.0.0.1:8788",
	out: "",
	provisioning: "http://127.0.0.1:8789",
	token: path.join( process.cwd(), "../server/.state/cluster/agent-provisioning-token" ),
	journal: path.join( process.cwd(), "../../.state/server-tick-bench/crowd-cleanup.json" )
}, USAGE );

/*
================
readSkillGroups

The shipped player skill groups (Chinese weapon and force lines), each with
its ranks in id order: the same rows the server's skill data loads.
================
*/
async function readSkillGroups() {
	const dir = path.join( serverGameDataRoot, "textdata" );
	const names = (await readdir( dir )).filter( name => /^skilldata_\d+\.txt$/i.test( name ) );
	const groups = new Map();
	for ( const name of names ) {
		const text = (await readFile( path.join( dir, name ) )).toString( "utf16le" ).replace( /^﻿/, "" );
		for ( const line of text.split( /\r?\n/ ) ) {
			const cells = line.split( "\t" );
			if ( cells[SKILL_SERVICE] !== "1" || !cells[SKILL_CODENAME]?.startsWith( "SKILL_CH_" ) ) continue;
			const id = Number( cells[SKILL_ID] ), group = Number( cells[SKILL_GROUP] );
			if ( !Number.isInteger( id ) || id <= 0 || !Number.isInteger( group ) || group <= 0 ) continue;
			if ( !groups.has( group ) ) groups.set( group, [] );
			groups.get( group ).push( id );
		}
	}
	for ( const ranks of groups.values() ) ranks.sort( ( a, b ) => a - b );
	return groups;
}

/*
================
planLoadouts

The groups with the most ranks; peer i learns rank i of each, so peers
know different skill ids and together exceed any small catalogue window.
================
*/
function planLoadouts( groups, count, perPeer ) {
	const chosen = [ ...groups.values() ].sort( ( a, b ) => b.length - a.length || a[0] - b[0] ).slice( 0, perPeer );
	const loadouts = Array.from( { length: count }, ( _, peer ) => ({
		level: LOADOUT_LEVEL,
		intellect: LOADOUT_INTELLECT,
		skills: chosen.map( ranks => ranks[peer % ranks.length] )
	}) );
	const distinct = new Set( loadouts.flatMap( loadout => loadout.skills ) ).size;
	return { loadouts, distinct, groups: chosen.length };
}

/*
================
readMetrics
================
*/
async function readMetrics() {
	const response = await fetch( new URL( "/transport/metrics", options.metrics ), {
		signal: AbortSignal.timeout( 5000 )
	} );
	if ( !response.ok ) throw Error( `metrics HTTP ${response.status}` );
	return { at: performance.now(), body: await response.json() };
}

/*
================
windowPercentile

The upper bound of the bucket holding the q-th fraction of the window's
ticks, from two cumulative histogram reads.
================
*/
function windowPercentile( bounds, counts, q ) {
	const total = counts.reduce( ( a, b ) => a + b, 0 );
	if ( total === 0 ) return null;
	const rank = Math.max( 1, Math.ceil( q * total ) );
	let seen = 0;
	for ( let i = 0; i < counts.length; i++ ) {
		seen += counts[i];
		if ( seen >= rank ) return i < bounds.length ? bounds[i] : `>${bounds.at( -1 )}`;
	}
	return null;
}

/*
================
windowReport

What happened between two cumulative reads: tick count, overruns, the
window's own mean and, when served, its percentiles and phase buckets.
================
*/
function windowReport( before, after ) {
	const a = before.body, b = after.body;
	const ticks = b.tick_count - a.tick_count;
	const report = {
		seconds: (after.at - before.at) / 1000,
		ticks,
		ticksPerSecond: ticks / ((after.at - before.at) / 1000),
		overruns: b.tick_overruns - a.tick_overruns,
		overrunRate: ticks > 0 ? (b.tick_overruns - a.tick_overruns) / ticks : null,
		meanMs: ticks > 0 ? (b.tick_mean_ms * b.tick_count - a.tick_mean_ms * a.tick_count) / ticks : null,
		maxSinceBootMs: b.tick_max_ms,
		liveSessions: b.live_sessions
	};
	const ha = a.tick_ms_histogram, hb = b.tick_ms_histogram;
	if ( ha && hb ) {
		const counts = hb.counts.map( ( n, i ) => n - (ha.counts[i] ?? 0) );
		report.percentilesMs = Object.fromEntries(
			[ [ "p50", 0.5 ], [ "p90", 0.9 ], [ "p99", 0.99 ], [ "p999", 0.999 ] ].map( ( [name, q] ) => [
				name,
				windowPercentile( hb.bounds_ms, counts, q )
			] )
		);
	}
	if ( a.tick_phase_ms && b.tick_phase_ms ) {
		report.phaseBuckets = Object.fromEntries(
			Object.entries( b.tick_phase_ms ).map( ( [phase, h] ) => [
				phase,
				h.buckets.map( ( n, i ) => n - (a.tick_phase_ms[phase]?.buckets[i] ?? 0) )
			] )
		);
	}
	return report;
}

const groups = await readSkillGroups();
const plan = planLoadouts( groups, options.count, Math.min( options.skills, MAX_LOADOUT_SKILLS ) );
console.log(
	`[tick-bench] ${options.count} peers x ${plan.groups} skill groups: ${plan.distinct} distinct skill ids`
);
await mkdir( path.dirname( options.journal ), { recursive: true } );
const crowd = await createCrowd( {
	count: options.count,
	fixture: MISSION_MOVEMENT_FIXTURES.movement,
	provisioningUrl: options.provisioning,
	tokenPath: options.token,
	journalPath: options.journal,
	loadoutFor: index => plan.loadouts[index]
} );
let report;
try {
	console.log( `[tick-bench] crowd admitted; warming up ${options.warmup} s` );
	await delay( options.warmup * 1000 );
	const before = await readMetrics();
	await delay( options.seconds * 1000 );
	const after = await readMetrics();
	report = {
		capturedAt: new Date().toISOString(),
		peers: options.count,
		skillGroups: plan.groups,
		distinctSkills: plan.distinct,
		window: windowReport( before, after )
	};
} finally {
	await crowd.close();
}
console.log( JSON.stringify( report, null, 2 ) );
if ( options.out ) await writeFile( options.out, JSON.stringify( report, null, 2 ) );
