/*
===========================================================================

gpu-pose-layers.ts - which of a pose's layers the GPU skeleton pass takes

The GPU skeleton pass (runtime/renderer/device/animation.ts) samples at
most MAX_GPU_LAYERS clips and mixes them as the CPU mixer would
(animation-pose.ts sampleLayers). This module decides whether a pose's
resolved layers fit that pass and, when they do, describes them as the
pass's CharacterGpuSample; animation-pose.ts owns the layers themselves.

===========================================================================
*/
import type { CharacterClip, CharacterGpuSample } from "@/engine/contracts/character";

// The layers the GPU skeleton pass mixes (CharacterGpuSample): a
// crossfade's two. More fall back to the CPU mixer.
export const MAX_GPU_LAYERS = 2;

/*
================
PoseLayer

One resolved layer of a pose: its clip, local time, weight and lane.
================
*/
export type PoseLayer = { clip: CharacterClip | undefined; time: number; weight: number; lane: "event" | "timed"; };

/*
================
gpuPoseRefusal

Why the GPU sampler cannot take these layers (null when it can): it
samples at most MAX_GPU_LAYERS resident clips on the default body with no
rotated bone.
================
*/
export function gpuPoseRefusal(
	layers: readonly PoseLayer[],
	gpuClips: ReadonlySet<CharacterClip>,
	defaultBody: boolean,
	rotatedNode: number
): string | null {
	if ( !defaultBody ) return "body volume";
	if ( rotatedNode >= 0 ) return "bone rotation";
	// The mixer skips a layer without a clip or weight (sampleLayers):
	// only the layers that contribute count.
	let live = 0;
	for ( const layer of layers ) {
		if ( !layer.clip || layer.weight === 0 ) continue;
		if ( ++live > MAX_GPU_LAYERS ) return "layers";
		if ( !gpuClips.has( layer.clip ) ) return "clip not on gpu";
	}
	return live ? null : "no clip";
}

/*
================
gpuPoseSample

The layers gpuPoseRefusal accepted, written into request: the first clip
and time, the second (or none) and how far toward it the mix goes.
================
*/
export function gpuPoseSample(
	layers: readonly PoseLayer[],
	request: { -readonly [K in keyof CharacterGpuSample]: CharacterGpuSample[K]; }
): CharacterGpuSample {
	let first: PoseLayer | undefined, second: PoseLayer | undefined;
	for ( const layer of layers ) {
		if ( !layer.clip || layer.weight === 0 ) continue;
		if ( first ) second = layer;
		else first = layer;
	}
	if ( !first?.clip ) throw Error( "No GPU pose layer" );
	// Event tracks commit before timed ones (sampleLayers): the event layer
	// of a split pair goes first.
	if ( second && first.lane === "timed" && second.lane === "event" ) [first, second] = [ second, first ];
	request.clip = first.clip!;
	request.time = first.time;
	request.next = second?.clip ?? null;
	request.nextTime = second?.time ?? 0;
	request.share = second ? layerMixShare( first, second ) : 0;
	return request;
}

/*
================
layerMixShare

How far toward the second layer a node both animate is mixed, as the CPU
mixer computes it: one lane accumulates by w/(sum+w); across lanes the
event layer commits min(w, 1) first and the timed one gets only the
remaining headroom.
================
*/
export function layerMixShare( first: PoseLayer, second: PoseLayer ): number {
	if ( first.lane === second.lane ) return second.weight / (first.weight + second.weight);
	const committed = Math.min( first.weight, 1 ), weight = Math.min( second.weight, 1 - committed );
	return weight > 0 ? weight / (committed + weight) : 0;
}
