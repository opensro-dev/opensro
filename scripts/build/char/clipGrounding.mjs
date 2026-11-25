/*
===========================================================================

clipGrounding.mjs - foot grounding for authored clips that sink at import

A few retail BANs author their locomotion with the feet driven through
the model's ground plane. The v1.150 client has no ground snap to hide
that: its placement composes the world position as the region base plus
the server position verbatim (CICharactor_SetPositionFromSpawnPosition
0x850CB0 copies the spawn fields untouched; the placement builder at
0x853B20 adds local to base on every axis). The port keeps that rule, so
an authored sink is a data defect, and the owner's rule is that native
bugs are fixed, not copied. The correction therefore lives here, at the
point where the BAN is imported: the registered clips get their root
track lifted by exactly the penetration measured through the model's own
skin, per keyframe, until the lowest vertex clears the plane. The gait,
the timing and every other bone stay authored.

===========================================================================
*/
import { compileBanTimeline } from "./compileBanTimeline.mjs";
import { mulMat4, quatToMat4 } from "./mat4.mjs";

// BAN files whose authored locomotion keys drive the feet through the
// ground plane. Measured on the retail data through this module's own
// skinning (lowest skinned vertex, model units, ground plane = 0):
//
//   p_bear02_walk.ban (13 keys over 1666 ms): keyframes 3, 4, 5, 9, 10
//   and 11 reach -3.01, -2.88, -2.06, -2.56, -2.23 and -1.80; sampled
//   between keys the lowest paw reaches -3.52. The same model's stand
//   clip never goes below -0.04, and the smaller bear rig (p_bear01)
//   stays above -0.57 (#4416).
//   p_bear02_run.ban (16 keys): keyframe 8 reaches -1.78 and keyframe
//   10 -1.10; sampled between keys the paw reaches -1.94. A following
//   pet runs whenever its owner runs, so the run carries the same
//   defect at the faster gait (#4458 review).
//   p_seowon_walk.ban (14 keys): keyframes 1 and 2 reach -2.36 and
//   -2.38 and nearly every key plants a foot about a unit under;
//   sampled between keys the foot reaches -2.67. Its stand clip never
//   goes below -0.07: the same defect on the monkey rig. Its run
//   stays authored (sampled -1.26, about -1.19 under its stand floor,
//   above the census line in cosWalkClearance.test.mjs).
const GROUNDED_BAN_BASENAMES = new Set( [
	"p_bear02_walk.ban",
	"p_bear02_run.ban",
	"p_seowon_walk.ban"
] );

// How far under the plane still counts as touching it while the
// grounding iterates; the runtime interpolates a hair past the keys.
const GROUND_EPSILON = 0.002;

// Dense samples per keyframe interval when scanning for penetration
// between keys.
const GROUND_SAMPLES_PER_INTERVAL = 16;

// quatToMat4/mulMat4 live in mat4.mjs, shared with exportGlb.mjs. The
// inverse bind needs no general inverse: the BSK stores each bone's
// inverse world transform beside the world one.

/*
================
clipTracks

The clip's bone tracks keyed by global skeleton index; a bone the clip
does not track holds its bind local, exactly as the runtime poses it.
================
*/
function clipTracks( skeleton, clip ) {
	const tracks = new Map();
	for ( const bone of clip.bones ) {
		const gi = skeleton.byName.get( bone.name );
		if ( gi !== undefined ) tracks.set( gi, bone );
	}
	return tracks;
}

/*
================
skinMatricesAt

Per-bone skin matrices (world * inverse bind) at an exported keyframe,
optionally blended toward the next key the way the runtime's LINEAR
sampler reads it: translations lerp, rotations take the normalized
shortest-path lerp. A null clip poses the bind skeleton.
================
*/
function skinMatricesAt( skeleton, inverseBind, tracks, sourceIndices, keyIndex, frac ) {
	const world = new Array( skeleton.bones.length );
	skeleton.bones.forEach( ( bone, i ) => {
		const track = tracks?.get( i );
		let q = bone.local.q, t = bone.local.t;
		if ( track ) {
			const k0 = track.keys[sourceIndices[keyIndex]];
			const k1 = track.keys[sourceIndices[Math.min( keyIndex + 1, sourceIndices.length - 1 )]];
			if ( frac > 0 ) {
				t = [ 0, 1, 2 ].map( c => k0.t[c] + (k1.t[c] - k0.t[c]) * frac );
				let dot = 0;
				for ( let c = 0; c < 4; c += 1 ) dot += k0.q[c] * k1.q[c];
				const s = dot < 0 ? -1 : 1;
				const blended = [ 0, 1, 2, 3 ].map( c => k0.q[c] + (s * k1.q[c] - k0.q[c]) * frac );
				const len = Math.hypot( ...blended ) || 1;
				q = blended.map( v => v / len );
			} else {
				q = k0.q;
				t = k0.t;
			}
		}
		const local = quatToMat4( q, t );
		world[i] = bone.parentIndex >= 0 ? mulMat4( world[bone.parentIndex], local ) : local;
	} );
	return world.map( ( w, i ) => mulMat4( w, inverseBind[i] ) );
}

/*
================
lowestVertexY

The lowest skinned vertex height under one pose. Rigid parts ride the
model space unskinned and cannot sink through a bone, so they are not
part of the measure; a weightless vertex stays at the origin like the
exporter's model-space identity joint.
================
*/
function lowestVertexY( parts, skin ) {
	let best = Infinity;
	for ( const part of parts ) {
		const mesh = part.mesh;
		if ( mesh.rigid ) continue;
		for ( let v = 0; v < mesh.vertexCount; v += 1 ) {
			const px = mesh.positions[v * 3], py = mesh.positions[v * 3 + 1], pz = mesh.positions[v * 3 + 2];
			let y = 0;
			for ( let s = 0; s < 2; s += 1 ) {
				const w = mesh.boneWeights[v * 2 + s];
				if ( w <= 0 ) continue;
				const m = skin[part.localToGlobal[mesh.boneIndices[v * 2 + s]]];
				y += w * (m[1] * px + m[5] * py + m[9] * pz + m[13]);
			}
			if ( y < best ) best = y;
		}
	}
	return best;
}

/*
================
measureClipLowestY

The lowest point the clip's whole body reaches, sampled at its keys and
between them. `skeleton` and `parts` are the assembled avatar's; a null
clip measures the bind pose. This is the ruler the grounding corrects
against and the census reads.
================
*/
export function measureClipLowestY( skeleton, parts, clip, samplesPerInterval = GROUND_SAMPLES_PER_INTERVAL ) {
	const inverseBind = skeleton.bones.map( bone => quatToMat4( bone.invWorld.q, bone.invWorld.t ) );
	if ( !clip ) {
		return lowestVertexY( parts, skinMatricesAt( skeleton, inverseBind, null, null, 0, 0 ) );
	}
	const tracks = clipTracks( skeleton, clip );
	const { sourceIndices } = compileBanTimeline( clip, clip.name );
	let best = Infinity;
	for ( let j = 0; j < sourceIndices.length - 1; j += 1 ) {
		for ( let s = 0; s < samplesPerInterval; s += 1 ) {
			const y = lowestVertexY(
				parts,
				skinMatricesAt( skeleton, inverseBind, tracks, sourceIndices, j, s / samplesPerInterval )
			);
			if ( y < best ) best = y;
		}
	}
	const last = lowestVertexY(
		parts,
		skinMatricesAt( skeleton, inverseBind, tracks, sourceIndices, sourceIndices.length - 1, 0 )
	);
	return Math.min( best, last );
}

/*
================
groundedBanBasename

True when the BAN at this import path is registered for grounding.
Paths arrive authored ("prim\ani\cos\p_bear02\p_bear02_walk.ban"), so
the comparison folds case and separators.
================
*/
export function groundedBanBasename( banPath ) {
	if ( !banPath ) return false;
	const base = String( banPath ).replaceAll( "\\", "/" ).split( "/" ).pop().toLowerCase();
	return GROUNDED_BAN_BASENAMES.has( base );
}

/*
================
groundClipFeet

Lift the clip's root track, per exported keyframe, until the lowest
skinned vertex clears the ground plane. A root lift is a rigid vertical
offset of the whole pose, so the lift a sample feels is the linear
interpolation of its bracketing keys' lifts: measuring the authored
penetration at dense samples and charging each sample's full depth to
both of its bracketing keys therefore clears every sample in one pass,
and no key rises past the worst sample it neighbours. When the loop's
first and last root keys are the same authored pose they take the same
lift, so the wrap cannot pop.

Mutates the clip's root keys (translations only). Returns the lift per
exported keyframe, or null when the clip has no root track to lift.
================
*/
export function groundClipFeet( skeleton, parts, clip ) {
	const rootIndex = skeleton.bones.findIndex( bone => bone.parentIndex < 0 );
	const rootTrack = clip.bones.find( bone => skeleton.byName.get( bone.name ) === rootIndex );
	if ( rootIndex < 0 || !rootTrack ) return null;
	const inverseBind = skeleton.bones.map( bone => quatToMat4( bone.invWorld.q, bone.invWorld.t ) );
	const tracks = clipTracks( skeleton, clip );
	const { sourceIndices } = compileBanTimeline( clip, clip.name );
	const keyCount = sourceIndices.length;
	const lifts = new Float64Array( keyCount );
	const firstKey = rootTrack.keys[sourceIndices[0]], lastKey = rootTrack.keys[sourceIndices[keyCount - 1]];
	const looped = firstKey.q.every( ( v, c ) => v === lastKey.q[c] ) &&
		firstKey.t.every( ( v, c ) => v === lastKey.t[c] );

	function apply( j, delta ) {
		if ( delta <= 0 ) return;
		rootTrack.keys[sourceIndices[j]].t[1] += delta;
		lifts[j] += delta;
	}
	function lowestAt( j, frac ) {
		return lowestVertexY( parts, skinMatricesAt( skeleton, inverseBind, tracks, sourceIndices, j, frac ) );
	}

	// The authored penetration at every sample, then each sample's depth
	// charged to both bracketing keys (see the banner). Penetration is
	// measured before any key moves, so the passes cannot interfere.
	const required = new Float64Array( keyCount );
	for ( let j = 0; j < keyCount - 1; j += 1 ) {
		for ( let s = 0; s < GROUND_SAMPLES_PER_INTERVAL; s += 1 ) {
			const y = lowestAt( j, s / GROUND_SAMPLES_PER_INTERVAL );
			if ( y < -GROUND_EPSILON ) {
				required[j] = Math.max( required[j], -y );
				required[j + 1] = Math.max( required[j + 1], -y );
			}
		}
	}
	const tailY = lowestAt( keyCount - 1, 0 );
	if ( tailY < -GROUND_EPSILON ) {
		required[keyCount - 1] = Math.max( required[keyCount - 1], -tailY );
	}
	for ( let j = 0; j < keyCount; j += 1 ) apply( j, required[j] );
	if ( looped && lifts[0] !== lifts[keyCount - 1] ) {
		const lift = Math.max( lifts[0], lifts[keyCount - 1] );
		apply( 0, lift - lifts[0] );
		apply( keyCount - 1, lift - lifts[keyCount - 1] );
	}
	return lifts;
}
