/*
===========================================================================

visual-capture.mjs - bounded browser screencast outside performance measurement

Acknowledges Chrome frames immediately and writes them after the scenario.
The encoded clip retains capture timestamps; recording overhead is excluded
from benchmark percentiles by calling this only after profiling stops.

===========================================================================
*/
import { mkdir, writeFile } from "node:fs/promises";
import { spawnSync } from "node:child_process";
import path from "node:path";

/*
================
captureVisual
================
*/
export async function captureVisual( page, directory, drive ) {
	const cdp = await page.context().newCDPSession( page ), frames = [];
	/*
	================
receive
	================
	*/
	const receive = event => {
		if ( frames.length < 180 ) frames.push( { jpeg: event.data, at: event.metadata.timestamp } );
		void cdp.send( "Page.screencastFrameAck", { sessionId: event.sessionId } ).catch( () => {} );
	};
	cdp.on( "Page.screencastFrame", receive );
	try {
		await cdp.send( "Page.startScreencast", {
			format: "jpeg",
			quality: 70,
			maxWidth: 960,
			maxHeight: 540,
			everyNthFrame: 4
		} );
		await drive();
	} finally {
		await cdp.send( "Page.stopScreencast" );
		cdp.off( "Page.screencastFrame", receive );
		await cdp.detach();
	}
	if ( frames.length < 2 ) throw Error( "Visual capture contains fewer than two frames" );
	const root = path.resolve( directory, "visual" );
	await mkdir( root, { recursive: true } );
	const entries = [ "ffconcat version 1.0" ];
	for ( let i = 0; i < frames.length; i++ ) {
		const name = `${String( i ).padStart( 4, "0" )}.jpg`;
		await writeFile( path.join( root, name ), Buffer.from( frames[i].jpeg, "base64" ) );
		entries.push( `file '${name}'` );
		if ( i + 1 < frames.length ) entries.push( `duration ${Math.max( .001, frames[i + 1].at - frames[i].at )}` );
	}
	const list = path.join( root, "frames.ffconcat" ), output = path.resolve( directory, "capture.mp4" );
	await writeFile( list, entries.join( "\n" ) + "\n" );
	const result = spawnSync( process.env.SRO_PROBE_FFMPEG ?? "ffmpeg", [
		"-y",
		"-loglevel",
		"error",
		"-f",
		"concat",
		"-safe",
		"1",
		"-i",
		list,
		"-vf",
		"scale=960:-2",
		"-c:v",
		"libx264",
		"-pix_fmt",
		"yuv420p",
		"-movflags",
		"+faststart",
		output
	], { encoding: "utf8", windowsHide: true } );
	if ( result.error || result.status !== 0 ) {
		throw Error( `Visual encoding failed: ${result.error ?? result.stderr}` );
	}
	return { frames: frames.length, seconds: frames.at( -1 ).at - frames[0].at, file: output };
}
