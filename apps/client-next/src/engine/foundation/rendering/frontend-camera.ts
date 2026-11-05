/*
===========================================================================

frontend-camera.ts - frontend camera tracks: key sampling and view cameras

Samples the authored title/dock/creation camera tracks (native camera
evidence: CameraTrack_CatmullRomPoint, CameraTrack_UnwrapAngles, EnvironmentVectorTrack_SampleAtTime, CCamera_ComposeEyeFromOrbit) and turns a key
into the WorldCamera the renderer consumes. Owns no state.

===========================================================================
*/
import type { FrontendCameraKey } from "@/engine/contracts/frontend";
import type { WorldCamera } from "@/engine/contracts/scene";
/*
================
curve

Catmull-Rom over four keys in native float precision.
================
*/
function curve( t: number, a: number, b: number, c: number, d: number ) {
	t = Math.fround( t );
	const t2 = Math.fround( t * t ), t3 = Math.fround( t2 * t );
	return Math.fround( .5 * (2 * b + (c - a) * t + (2 * a - 5 * b + 4 * c - d) * t2 + (-a + 3 * b - 3 * c + d) * t3) );
}
/*
================
angle

curve() over angles, unwrapped so the track takes the short way around.
================
*/
function angle( t: number, a: number, b: number, c: number, d: number ) {
	const values = [ a, b, c, d ].map( Math.fround ), pi = 3.1415927410125732, tau = 6.2831854820251465;
	for ( let i = 1; i < 4; i++ ) {
		const prior = values[i - 1]!;
		let v = values[i]!;
		const delta = Math.fround( v - prior );
		if ( delta <= -pi ) { while ( prior > v ) v = Math.fround( v + tau ); }
		else if ( delta > pi ) { while ( prior < v ) v = Math.fround( v - tau ); }
		values[i] = v;
	}
	return curve( t, values[0]!, values[1]!, values[2]!, values[3]! );
}
/*
================
sampleFrontendCamera

The track key in force at a time: clamped at both ends, interpolated
between the surrounding keys inside.
================
*/
export function sampleFrontendCamera( keys: readonly FrontendCameraKey[], time: number ): FrontendCameraKey {
	if ( !keys.length || !Number.isFinite( time ) ) throw new Error( "Missing camera track" );
	for ( let i = 0; i < keys.length; i++ ) {
		const k = keys[i]!;
		if (
			![
				k.timeSeconds,
				k.sectorX,
				k.sectorY,
				k.mode,
				...Object.values( k.position ),
				...Object.values( k.rotation )
			].every( Number.isFinite ) || Object.values( k.rotation ).some( v => Math.abs( v ) > 1000 ) ||
			i > 0 && k.timeSeconds <= keys[i - 1]!.timeSeconds
		) throw new Error( "Invalid camera track" );
	}
	if ( time <= keys[0]!.timeSeconds ) return keys[0]!;
	if ( time >= keys[keys.length - 1]!.timeSeconds ) return keys[keys.length - 1]!;
	const next = keys.findIndex( k => k.timeSeconds >= time ),
		b = keys[next - 1]!,
		c = keys[next]!,
		a = keys[Math.max( 0, next - 2 )]!,
		d = keys[Math.min( keys.length - 1, next + 1 )]!,
		t = (time - b.timeSeconds) / (c.timeSeconds - b.timeSeconds);
	return {
		...b,
		timeSeconds: time,
		mode: curve( t, a.mode, b.mode, c.mode, d.mode ),
		position: {
			x: curve( t, a.position.x, b.position.x, c.position.x, d.position.x ),
			y: curve( t, a.position.y, b.position.y, c.position.y, d.position.y ),
			z: curve( t, a.position.z, b.position.z, c.position.z, d.position.z )
		},
		rotation: {
			x: angle( t, a.rotation.x, b.rotation.x, c.rotation.x, d.rotation.x ),
			y: angle( t, a.rotation.y, b.rotation.y, c.rotation.y, d.rotation.y ),
			z: angle( t, a.rotation.z, b.rotation.z, c.rotation.z, d.rotation.z )
		}
	};
}
/*
================
frontendCameraView

The render camera for one track key: the eye sits mode units behind the
key position along its facing. Every frontend camera locks its horizontal
FOV (GH #1246); gameplay cameras never do.
================
*/
export function frontendCameraView( key: FrontendCameraKey ): WorldCamera {
	const p = key.position,
		r = key.rotation,
		cp = Math.cos( r.x ),
		sp = Math.sin( r.x ),
		cy = Math.cos( r.y ),
		sy = Math.sin( r.y ),
		cr = Math.cos( r.z ),
		sr = Math.sin( r.z );
	const forward = [ cp * sy, -sp, cp * cy ],
		up = [ -sr * cy + cr * sp * sy, cr * cp, sr * sy + cr * sp * cy ] as const;
	// Port-only (GH #1246): the frontend frame is authored landscape; portrait viewports lock its horizontal FOV.
	return {
		originRegion: key.sectorX | (key.sectorY << 8),
		eye: [ p.x - forward[0]! * key.mode, p.y - forward[1]! * key.mode, p.z - forward[2]! * key.mode ],
		target: [ p.x, p.y, p.z ],
		up,
		fov: Math.PI / 3,
		near: 1,
		far: 3500,
		lockHorizontalFov: true
	};
}
