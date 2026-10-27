/*
===========================================================================

camera-waypoints.ts - the debug console's camera waypoint store (/cw*)

CGInterface keeps a CEnvironmentVectorTrack at +0x3FC beside the camera
bytes: a timed keyframe track (one 16-byte record per keyframe - a Vector3
with its time in the last dword - plus the orientation channels the saved
Vector4 is sampled from; the clock sits at track+0xE8 and the sampled pose
at +0xF8/+0x104). Command rows 130-134 of CGInterface_OnCommandMessage
(690C40) drive it: /cwclear resets the track object, /cwpop and /cwgo share
one case (the free-camera byte at +0x3CE must stand and the store be
non-empty; the play byte at +0x3CD is written and the clock zeroed),
/cwstop clears the play byte, and /cwsave writes one sampled line per
record to c:\cw.txt.

No retail code ever loads the track: the only reader of
config\cameradata.txt is the mission scene's per-region query
(CPSMission_LoadCameraDataForRegion, 7789A0), and the track's only readers
are /cwsave and the playback stepper, so natively the store is always
empty and the family is dead GM plumbing. The port gives it the content
source issue #1716 assigns: the published assets/config/cameradata.json
rows load as keyframes (a port decision, recorded on the issue - the rows
carry no time of their own, so they stand one second apart in file order,
and each row's yaw/pitch become the keyframe's orientation quaternion).
The play byte and the stepping live with their owners (ui.ts, world.ts);
this module owns the store, its load, the save text, and the sampling
the stepper reads (the track's own reader, beside /cwsave's).

===========================================================================
*/

import { multiplyQuaternion } from "@/engine/foundation/math/pose-math";

// The published rows carry no time: one keyframe per second, in file order.
const KEYFRAME_SPACING_SECONDS = 1;

/*
================
CameraWaypoint

One keyframe: the native record's observable shape (a time, a Vector3 and
the Vector4 /cwsave prints). The orientation is a quaternion.
================
*/
export interface CameraWaypoint {
	readonly timeSeconds: number;
	readonly position: { readonly x: number; readonly y: number; readonly z: number; };
	readonly orientation: { readonly x: number; readonly y: number; readonly z: number; readonly w: number; };
}

/*
================
CameraWaypointStore

The track at CGInterface +0x3FC: its keyframes and its phase clock
(+0xE8), which /cwgo zeroes and /cwclear's reset wipes with the records.
================
*/
export interface CameraWaypointStore {
	keyframes: CameraWaypoint[];
	clockSeconds: number;
}

/*
================
CameraDataRow

One published cameradata.json row, reduced to what the store reads: the
mission camera's position and its rotation degrees (x pitch, y yaw - the
pair CPSMission_LoadCameraDataForRegion converts to radians).
================
*/
export interface CameraDataRow {
	readonly position: { readonly x: number; readonly y: number; readonly z: number; };
	readonly rotationDegrees: { readonly x: number; readonly y: number; };
}

/*
================
createCameraWaypointStore

The constructor's track: no keyframes, a zeroed clock.
================
*/
export function createCameraWaypointStore(): CameraWaypointStore {
	return { keyframes: [], clockSeconds: 0 };
}

/*
================
resetCameraWaypoints

690C40 case 130 (/cwclear): the store reset (CRenderContext_Reset on the
+0x3FC object) - the records and the clock go together.
================
*/
export function resetCameraWaypoints( store: CameraWaypointStore ): void {
	store.keyframes = [];
	store.clockSeconds = 0;
}

/*
================
decodeCameraDataRows

The published cameradata asset (scripts/build/shared/configResources.mjs):
an object whose rows each carry a position and rotation degrees. Only
those two fields are read, so only those two are pinned.
================
*/
export function decodeCameraDataRows( raw: unknown ): readonly CameraDataRow[] {
	const v = raw as { rows?: unknown; } | null;
	if ( !v || !Array.isArray( v.rows ) ) throw Error( "Invalid camera data" );
	return v.rows.map( row => {
		const r = row as {
			position?: { x?: unknown; y?: unknown; z?: unknown; };
			rotationDegrees?: { x?: unknown; y?: unknown; };
		} | null;
		const position = r?.position, rotation = r?.rotationDegrees;
		if (
			!r || !position || !rotation ||
			!Number.isFinite( position.x ) || !Number.isFinite( position.y ) || !Number.isFinite( position.z ) ||
			!Number.isFinite( rotation.x ) || !Number.isFinite( rotation.y )
		) throw Error( "Invalid camera data row" );
		return {
			position: { x: position.x as number, y: position.y as number, z: position.z as number },
			rotationDegrees: { x: rotation.x as number, y: rotation.y as number }
		};
	} );
}

/*
================
cameraDataOrientation

A row's yaw then pitch as a quaternion (Ry * Rx, the aim the mission
camera's angle pair describes): 776D60 turns the degree tokens to radians
natively; the port composes the rotation the track record holds.
================
*/
function cameraDataOrientation( row: CameraDataRow ): CameraWaypoint["orientation"] {
	const yawHalf = row.rotationDegrees.y * Math.PI / 180 / 2;
	const pitchHalf = row.rotationDegrees.x * Math.PI / 180 / 2;
	const out = new Float32Array( 4 );
	multiplyQuaternion(
		[ 0, Math.sin( yawHalf ), 0, Math.cos( yawHalf ) ],
		[ Math.sin( pitchHalf ), 0, 0, Math.cos( pitchHalf ) ],
		out
	);
	return { x: out[0]!, y: out[1]!, z: out[2]!, w: out[3]! };
}

/*
================
loadCameraDataKeyframes

The port's content source (the header's decision): the published rows
become the store's keyframes in file order, one second apart, and the
clock restarts. Loading replaces the records, as a fresh track would.
================
*/
export function loadCameraDataKeyframes( store: CameraWaypointStore, rows: readonly CameraDataRow[] ): void {
	store.keyframes = rows.map( ( row, index ) => ({
		timeSeconds: index * KEYFRAME_SPACING_SECONDS,
		position: { x: row.position.x, y: row.position.y, z: row.position.z },
		orientation: cameraDataOrientation( row )
	}) );
	store.clockSeconds = 0;
}

/*
================
sampleSegmentChannel

One channel of the segment curve at u: the four clamped neighbours
P0..P3 through the uniform Catmull-Rom basis (the curve passes P1 at
u = 0 and P2 at u = 1, with the neighbours as its tangents' shape).
================
*/
function sampleSegmentChannel( p0: number, p1: number, p2: number, p3: number, u: number ): number {
	const u2 = u * u, u3 = u2 * u;
	return 0.5 * (
		2 * p1 +
		(p2 - p0) * u +
		(2 * p0 - 5 * p1 + 4 * p2 - p3) * u2 +
		(3 * p1 - p0 - 3 * p2 + p3) * u3
	);
}

/*
================
sampleCameraWaypoints

The track sampled at a clock time: TimedValue_Advance (4DF160) clamps
the clock to the end time (+0xF0, the last record's time) and samples
the vector track and the four orientation channels at it. SampleAtTime
walks to the segment holding the time and evaluates it from the four
records around it - the segment's own pair plus one neighbour each
side, clamped to the ends - at u = (t - t1) / (t2 - t1); outside the
ends the first/last record stands. The native segment evaluator's
exact basis is set through a function pointer no constructor in the
trimmed binary was found to write, and the track is empty in retail
(never loaded, never rendered), so there is no native curve to copy:
the port infers the four-point Catmull-Rom the evaluator's argument
shape describes, per channel, and normalizes the orientation back to
a unit quaternion (the channels sampled apart do not keep length).
An empty track samples to null.
================
*/
export function sampleCameraWaypoints(
	keyframes: readonly CameraWaypoint[],
	timeSeconds: number
): { position: CameraWaypoint["position"]; orientation: CameraWaypoint["orientation"]; } | null {
	if ( !keyframes.length ) return null;
	const first = keyframes[0]!, last = keyframes[keyframes.length - 1]!;
	if ( timeSeconds <= first.timeSeconds || keyframes.length === 1 ) {
		return { position: first.position, orientation: first.orientation };
	}
	if ( timeSeconds >= last.timeSeconds ) {
		return { position: last.position, orientation: last.orientation };
	}
	let segment = 0;
	while ( segment < keyframes.length - 2 && keyframes[segment + 1]!.timeSeconds <= timeSeconds ) segment++;
	const p1 = keyframes[segment]!, p2 = keyframes[segment + 1]!;
	const p0 = keyframes[Math.max( segment - 1, 0 )]!, p3 = keyframes[Math.min( segment + 2, keyframes.length - 1 )]!;
	const span = p2.timeSeconds - p1.timeSeconds;
	const u = span > 0 ? (timeSeconds - p1.timeSeconds) / span : 0;
	const orientation = {
		x: sampleSegmentChannel( p0.orientation.x, p1.orientation.x, p2.orientation.x, p3.orientation.x, u ),
		y: sampleSegmentChannel( p0.orientation.y, p1.orientation.y, p2.orientation.y, p3.orientation.y, u ),
		z: sampleSegmentChannel( p0.orientation.z, p1.orientation.z, p2.orientation.z, p3.orientation.z, u ),
		w: sampleSegmentChannel( p0.orientation.w, p1.orientation.w, p2.orientation.w, p3.orientation.w, u )
	};
	const length = Math.hypot( orientation.x, orientation.y, orientation.z, orientation.w );
	if ( length > 1e-12 ) {
		orientation.x /= length;
		orientation.y /= length;
		orientation.z /= length;
		orientation.w /= length;
	}
	return {
		position: {
			x: sampleSegmentChannel( p0.position.x, p1.position.x, p2.position.x, p3.position.x, u ),
			y: sampleSegmentChannel( p0.position.y, p1.position.y, p2.position.y, p3.position.y, u ),
			z: sampleSegmentChannel( p0.position.z, p1.position.z, p2.position.z, p3.position.z, u )
		},
		orientation
	};
}

/*
================
nativeFloat

The /cwsave format's %ff: printf %f (six decimals) with the format's own
trailing f, so 1205 prints as 1205.000000f.
================
*/
function nativeFloat( value: number ): string {
	return value.toFixed( 6 ) + "f";
}

/*
================
cameraWaypointSaveText

690C40 case 134 (/cwsave): one line per record in the native form
({ %ff, Vector(%ff, %ff, %ff), Vector4(%ff, %ff, %ff, %ff) } with CRLF,
format string at BF9028), each record sampled by index
(CEnvironmentVectorTrack_SampleKeyframeAt, 68FD20) - at a record's own
time the sampled values are the record's. An empty store saves empty.
================
*/
export function cameraWaypointSaveText( store: CameraWaypointStore ): string {
	let text = "";
	for ( const keyframe of store.keyframes ) {
		const p = keyframe.position, o = keyframe.orientation;
		text += "{ " + nativeFloat( keyframe.timeSeconds ) +
			", Vector(" + nativeFloat( p.x ) + ", " + nativeFloat( p.y ) + ", " + nativeFloat( p.z ) + ")" +
			", Vector4(" + nativeFloat( o.x ) + ", " + nativeFloat( o.y ) + ", " + nativeFloat( o.z ) + ", " +
			nativeFloat( o.w ) + ") }\r\n";
	}
	return text;
}
