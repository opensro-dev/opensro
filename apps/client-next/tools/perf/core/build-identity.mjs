/*
===========================================================================

build-identity.mjs - observed page identity at measurement boundaries

The harness checkout is not the served build. Keep its revision separate
from the loaded entry URL, and reject changed or unproven capture state.
These observations do not identify a server commit or prove encoder output.
Bundle detection expects the release entry naming: /assets/index-<hash>.js.
servedCommit is the revision the served build was stamped with
(SRO_CLIENT_REVISION, exported by the bench entry and captured by
playable-session.mjs), never the harness checkout's.

===========================================================================
*/
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

/*
================
harnessRevision
================
*/
function harnessRevision() {
	try {
		const cwd = fileURLToPath( new URL( "../../../../../", import.meta.url ) );
		const git = ( ...args ) => execFileSync( "git", args, { cwd, encoding: "utf8" } ).trim();
		return git( "rev-parse", "HEAD" ) + (git( "status", "--porcelain" ) ? "+dirty" : "");
	} catch {
		return "unknown";
	}
}

/*
================
buildIdentity

Read only the recorder's own video. A live, advancing capture is different
from a preference, a paused video, or an unrelated media element.
================
*/
export async function buildIdentity( page, harnessCommit = harnessRevision() ) {
	const observed = await page.evaluate( () => {
		const scripts = [ ...document.querySelectorAll( 'script[type="module"][src]' ) ]
			.map( script => new URL( script.getAttribute( "src" ) ?? "", location.href ).href );
		// The dev-update owner removes Vite's websocket client. Its source entry
		// still identifies the served module graph; never infer a bundle from it.
		const source = scripts.find( src => {
			const url = new URL( src );
			return url.origin === location.origin && url.pathname === "/src/bootstrap.ts";
		} );
		const dev = !!source || scripts.some( src => new URL( src ).pathname === "/@vite/client" );
		const entry = source ?? scripts.find( src => /\/assets\/index-[^/]+\.js$/.test( new URL( src ).pathname ) ) ??
			null;
		const video = /** @type {HTMLVideoElement | null} */ (document.querySelector( "video.sro-replay-source" ));
		const stream = /** @type {MediaStream | null} */ (video?.srcObject ?? null);
		const tracks = stream?.getVideoTracks?.() ?? [];
		const playing = !!video && !video.paused && !video.ended && video.readyState >= 2 &&
			tracks.some( track => track.readyState === "live" );
		return {
			origin: location.origin,
			build: dev ? "dev-server" : entry ? "bundle" : "unknown",
			entry,
			documentTimeOrigin: performance.timeOrigin,
			replay: !video ? "off" : playing ? "capture-playing" : "unknown",
			replayTime: playing ? video.currentTime : null,
			replayPreference: localStorage.getItem( "sro:bug-report:replay:1" )
		};
	} );
	return { ...observed, harnessCommit, servedCommit: await servedRevision( page ) };
}

/*
================
servedRevision

The stamped revision the served build's runtime entry exported, or
"unknown" before the session captured it or for a build stamped with none.
================
*/
async function servedRevision( page ) {
	const revision = await page.evaluate( () => globalThis.__playableRevision ?? "" );
	return /^[0-9a-f]{7,40}$/.test( revision ) ? revision : "unknown";
}

/*
================
verifyServedCommit

Refuses a measurement of a build other than the one the run was asked to
measure: an A/B side served by a stale preview is a wrong reading, not a
noisy one. Both sides compare as prefixes, so a short SHA names it.
================
*/
export function verifyServedCommit( identity, expected ) {
	if ( !expected ) return;
	const served = identity.servedCommit;
	if ( served === "unknown" || !(served.startsWith( expected ) || expected.startsWith( served )) ) {
		throw Error( `Served commit ${served} is not the expected ${expected}` );
	}
}

/*
================
verifyMeasuredIdentity

Boundary snapshots are evidence, not a claim that no intermediate state
ever changed. A playing capture must also advance across the window.
================
*/
export function verifyMeasuredIdentity( before, after ) {
	for ( const key of [ "origin", "build", "entry", "documentTimeOrigin", "replay", "replayPreference" ] ) {
		if ( before[key] !== after[key] ) throw Error( `Measurement identity changed: ${key}` );
	}
	if ( before.build === "unknown" || before.replay === "unknown" ) {
		throw Error( "Measurement build or replay capture state is unknown" );
	}
	if ( before.replay === "capture-playing" && !(after.replayTime > before.replayTime) ) {
		throw Error( "Replay capture did not advance during measurement" );
	}
}
