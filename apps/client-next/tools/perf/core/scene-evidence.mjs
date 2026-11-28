/*
===========================================================================

scene-evidence.mjs - what a bench window really drew, frame by frame

A frame rate is only worth what its scene was. This owner holds the bench's
snow and proves the rest after a timed window: a short verify window that
keeps the scenario's own input running and, every interval, saves a
screenshot beside what the client held at that moment (the camera, every
peer's position, its worn chest garment and whether the imbue is attached
to it, the weather and the JS heap). judgeFrames turns those records into
the bench's verdict: the camera moved, the actors moved, clothes changed,
the imbue went on and it snowed in every frame.

The records come from the client's own state (the bench runtime), never
from the crowd's script, so a frame where the script sent a dress change
the client did not draw counts as no change.

===========================================================================
*/
import { mkdir, writeFile } from "node:fs/promises";

// Debug console weather (gameplay.ts debug-weather, event-environment.ts
// debugWeather): mode 3 is snow and holds until mode 1; 255 is the most.
export const SNOW_MODE = 3, SNOW_AMOUNT = 255;
// A drawn scene's luma spread (standard deviation, 0..255) is tens; a page
// whose canvas never presented (a blank white or black frame: a browser in
// a session with no display, a lost device) is under this.
export const DRAWN_LUMA_SPREAD = 8;
// Every PICTURE_STRIDE-th pixel of each axis is read: plenty for a spread.
const PICTURE_STRIDE = 4;
const SNOW_WAIT_MS = 10000, SNOW_POLL_MS = 200;
// The chest socket a busy peer dresses (crowd-activity.mjs) and its imbue.
const CHEST_SOCKET = 1;
// What counts as movement between two frames: a camera turn of a degree,
// a peer step of half a unit.
const CAMERA_MOVED_RAD = Math.PI / 180, PEER_MOVED = 0.5;

/*
================
holdSnow

Sets the client's weather to held snow and waits until the client says
so. The command is GM only: an observer the server does not name GM
(SRO_GM_CHARACTERS) never reaches snow, and the bench stops here rather
than measure a clear sky.
================
*/
export async function holdSnow( page ) {
	await page.evaluate( ( [mode, amount] ) =>
		globalThis.__benchRuntime.session( {
			kind: "gameplay",
			command: { kind: "debug-weather", mode, amount }
		} ), [ SNOW_MODE, SNOW_AMOUNT ] );
	const until = Date.now() + SNOW_WAIT_MS;
	for ( ;; ) {
		const weather = await page.evaluate( () => globalThis.__benchRuntime.gameplay()?.weather ?? null );
		if ( weather?.mode === SNOW_MODE && weather.amount === SNOW_AMOUNT ) return weather;
		if ( Date.now() > until ) {
			throw Error(
				`snow did not hold (weather ${JSON.stringify( weather )}): the observer must be a GM ` +
					"(SRO_GM_CHARACTERS=<division>:<character> on the scratch stack)"
			);
		}
		await page.waitForTimeout( SNOW_POLL_MS );
	}
}

/*
================
readFrameEvidence

Self-contained for page.evaluate: the client's state as one frame shows
it. imbue is the skill id whose attachment marks an imbued peer.
================
*/
/** @param {{ imbue: number, chest: number }} args */
export function readFrameEvidence( { imbue, chest } ) {
	const root = /** @type {any} */ (globalThis).__benchRuntime, game = root.gameplay();
	const local = game?.localGid;
	const poses = new Map( root.characterActors().map( actor => [ actor.gid, actor.pose ] ) );
	const attached = game?.attachedEffects ?? [];
	const peers = root.entities()
		.filter( entity => entity.kind === "player" && entity.gid !== local )
		.map( entity => {
			const pose = poses.get( entity.gid ) ?? entity;
			return {
				gid: entity.gid,
				x: pose.x,
				z: pose.z,
				chest: entity.equipment?.find( item => item.slot === chest )?.refObjId ?? 0,
				imbued: attached.some( effect => effect.gid === entity.gid && effect.skill === imbue )
			};
		} );
	const camera = root.camera();
	return {
		atMs: performance.now(),
		camera: { yaw: camera.yaw, pitch: camera.pitch, distance: camera.distance },
		weather: game?.weather ?? null,
		heapMB: (/** @type {any} */ (performance).memory?.usedJSHeapSize ?? 0) / 1048576,
		peers
	};
}

/*
================
pictureSpread

The luma spread of a screenshot, decoded by the page itself (the bench has
no image decoder): what tells a drawn scene from a frame that never
presented. The client's state can be all there while nothing is drawn, so
the verdict reads the picture too.
================
*/
export async function pictureSpread( page, png ) {
	return page.evaluate( async ( [base64, stride] ) => {
		const bytes = Uint8Array.from( atob( base64 ), c => c.charCodeAt( 0 ) );
		const bitmap = await createImageBitmap( new Blob( [ bytes ], { type: "image/png" } ) );
		const canvas = new OffscreenCanvas( bitmap.width, bitmap.height ), context = canvas.getContext( "2d" );
		context.drawImage( bitmap, 0, 0 );
		const { data, width, height } = context.getImageData( 0, 0, bitmap.width, bitmap.height );
		let n = 0, sum = 0, squares = 0;
		for ( let y = 0; y < height; y += stride ) {
			for ( let x = 0; x < width; x += stride ) {
				const at = (y * width + x) * 4, luma = .299 * data[at] + .587 * data[at + 1] + .114 * data[at + 2];
				n++;
				sum += luma;
				squares += luma * luma;
			}
		}
		const mean = sum / n;
		return { lumaMean: mean, lumaSpread: Math.sqrt( Math.max( 0, squares / n - mean * mean ) ) };
	}, [ png.toString( "base64" ), PICTURE_STRIDE ] );
}

/*
================
assertDrawn

Refuses a page whose picture is blank, before any window measures it: fps
of a client that presents nothing is not a reading.
================
*/
export async function assertDrawn( page, label ) {
	const picture = await pictureSpread( page, await page.screenshot() );
	if ( picture.lumaSpread < DRAWN_LUMA_SPREAD ) {
		throw Error(
			`${label}: the page shows no drawn scene (luma spread ${picture.lumaSpread.toFixed( 1 )}); ` +
				"a browser without a display (an SSH session) renders nothing - run in the desktop session"
		);
	}
	return picture;
}

/*
================
captureFrames

The verify window: input( more ) is the scenario's own driver, run until
count frames are saved, one every intervalMs, as DIR/PREFIX-NN.png and
DIR/PREFIX-frames.json. Returns the records.
================
*/
export async function captureFrames( { page, input, count, intervalMs, dir, prefix, imbue } ) {
	await mkdir( dir, { recursive: true } );
	let done = false;
	const driving = Promise.resolve( input( () => !done ) );
	const frames = [];
	try {
		for ( let index = 0; index < count; index++ ) {
			const started = Date.now();
			const file = `${prefix}-${String( index ).padStart( 2, "0" )}.png`;
			const evidence = await page.evaluate( readFrameEvidence, { imbue, chest: CHEST_SOCKET } );
			const png = await page.screenshot( { path: `${dir}/${file}` } );
			frames.push( { index, file, ...evidence, picture: await pictureSpread( page, png ) } );
			await page.waitForTimeout( Math.max( 0, intervalMs - (Date.now() - started) ) );
		}
	} finally {
		done = true;
		await driving;
	}
	await writeFile( `${dir}/${prefix}-frames.json`, JSON.stringify( frames, null, 2 ) );
	return frames;
}

/*
================
judgeFrames

The verdict over a verify window's records: for each property, how many
frame pairs (or peers) showed it, and pass only when every one the bench
asked for was seen. want names what the scenario promises (a still
camera promises no turn; a standing crowd no moves).
================
*/
export function judgeFrames( frames, want ) {
	let cameraTurns = 0, peerMoves = 0;
	const dressed = new Map(), imbued = new Set(), seen = new Set();
	for ( let i = 1; i < frames.length; i++ ) {
		const a = frames[i - 1].camera, b = frames[i].camera;
		if ( Math.abs( b.yaw - a.yaw ) >= CAMERA_MOVED_RAD || Math.abs( b.pitch - a.pitch ) >= CAMERA_MOVED_RAD ) {
			cameraTurns++;
		}
		const before = new Map( frames[i - 1].peers.map( peer => [ peer.gid, peer ] ) );
		for ( const peer of frames[i].peers ) {
			const last = before.get( peer.gid );
			if ( last && Math.hypot( peer.x - last.x, peer.z - last.z ) >= PEER_MOVED ) peerMoves++;
		}
	}
	for ( const frame of frames ) {
		for ( const peer of frame.peers ) {
			seen.add( peer.gid );
			if ( !dressed.has( peer.gid ) ) dressed.set( peer.gid, new Set() );
			dressed.get( peer.gid ).add( peer.chest );
			if ( peer.imbued ) imbued.add( peer.gid );
		}
	}
	const verdict = {
		frames: frames.length,
		peers: seen.size,
		cameraTurns,
		peerMoves,
		// A peer seen both wearing its chest garment and without it.
		dressChanged: [ ...dressed.values() ].filter( chests => chests.size > 1 && chests.has( 0 ) ).length,
		imbuedPeers: imbued.size,
		snowFrames: frames.filter( frame => frame.weather?.mode === SNOW_MODE ).length,
		drawnFrames: frames.filter( frame => (frame.picture?.lumaSpread ?? 0) >= DRAWN_LUMA_SPREAD ).length,
		heapMB: frames.length ?
			{
				min: Math.min( ...frames.map( frame => frame.heapMB ) ),
				max: Math.max( ...frames.map( frame => frame.heapMB ) )
			} :
			null
	};
	const failed = [];
	// Whatever the scenario promises, every frame must show a drawn scene:
	// the client's state alone passed a page that presented nothing.
	if ( verdict.drawnFrames !== frames.length ) failed.push( "a frame shows no drawn scene" );
	if ( want.camera && !cameraTurns ) failed.push( "camera never moved" );
	if ( want.actors && !peerMoves ) failed.push( "no actor moved" );
	if ( want.dress && !verdict.dressChanged ) failed.push( "no actor changed clothes" );
	if ( want.imbue && !verdict.imbuedPeers ) failed.push( "no actor wore the imbue" );
	if ( want.snow && verdict.snowFrames !== frames.length ) failed.push( "not snowing in every frame" );
	return { ...verdict, pass: failed.length === 0, failed };
}
