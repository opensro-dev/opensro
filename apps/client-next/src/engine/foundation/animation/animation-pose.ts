/*
===========================================================================

animation-pose.ts - retained character pose sampling, skin palettes and sockets

Owns clip sampling, native event-before-timed blending, and lazy CPU palette
materialization. A pending pose builds only the nodes its consumers read: a
socket settles its node's ancestor chain, a palette asked for some entries
(a cloth step) the chains of their joints, and a whole palette the rest. A
direct pass samples only the channels of the nodes it settles. Every node
gets the operations of one whole materialization, so every consumer sees
its bytes. Direct samples retain exact local inputs in blend scratch;
palette revisions advance only with matrices or radial body shape. Layer
lifetimes belong to the presentation producers; this consumer evaluates
every live fade without truncating sparse bone tracks.
Equipment branches join the socket table without changing explicit private
bone names (A981A0 searches compound branches in order for a marker).

===========================================================================
*/
import { createAttachmentBindPose } from "./equipment-sockets";
import { clipNodeChannels, createAnimationTimelines, type ClipNodeChannelCache } from "./animation-timelines";
import type { CharacterModel, CharacterPrimitive, CharacterLayer, CharacterClip } from "@/engine/contracts/character";
import { compose, multiplyDisjoint as multiply, multiplyQuaternion, slerp } from "@/engine/foundation/math/pose-math";
import { paletteBindings } from "./palette-bindings";
import { bodyBoneScale } from "./body-shape";

const DEFAULT_BODY_VOLUME = 2;
// A node's materialization state in one evaluation (createCharacterPose ready).
const SETTLING = 1, SETTLED = 2;
// The radial (Y and Z) rows of a global matrix body volume scales (paletteEntry).
const RADIAL_WORDS = [ 4, 5, 6, 8, 9, 10 ] as const;
// Distinct missing clip names one evaluator records: a model is one
// catalogue row, so a handful of names says everything about it.
export const MAX_MISSING_CLIPS = 8;
/*
================
CharacterPoseInputs

Everything a pose's skeleton and palettes are a function of: its resolved
layers (clip, wrapped time, weight, lane), its body volume and its bone
rotator. An evaluator of the same model given these (restore) builds the
same skeleton, bit for bit, as the one they were read from (inputs).
================
*/
export type CharacterPoseInputs = {
	readonly layers: { clip: CharacterClip | undefined; time: number; weight: number; lane: "event" | "timed"; }[];
	volume: number;
	female: boolean;
	// The rotator's node, or -1, and its rotation (x y z w).
	rotated: number;
	readonly rotation: Float32Array;
};
/*
================
createCharacterPoseInputs
================
*/
export function createCharacterPoseInputs(): CharacterPoseInputs {
	return { layers: [], volume: DEFAULT_BODY_VOLUME, female: false, rotated: -1, rotation: new Float32Array( 4 ) };
}
/*
================
createCharacterPose

nodeChannels is the shared per-clip channel index (clipNodeChannels); the
character renderer passes its plan caches' one. Without it the pose keeps
its own, built only for a clip it samples in part.
================
*/
export function createCharacterPose( model: CharacterModel, nodeChannels: ClipNodeChannelCache = new WeakMap() ) {
	const bindings = paletteBindings( model );
	const attachmentBind = createAttachmentBindPose( model.nodes );
	let volume = DEFAULT_BODY_VOLUME, female = false;
	// Body nodes precede attached handles. Keep that compound order for
	// unqualified lookups; explicit branch names stay distinct even when both
	// weapons carry an identically named marker.
	const sockets = new Map<string, number>();
	const equipmentMarker = /^equipment:\d+:[^:]+:(.+)$/;
	for ( let index = 0; index < model.nodes.length; index++ ) {
		const name = model.nodes[index]!.name;
		if ( !sockets.has( name ) ) sockets.set( name, index );
		const marker = equipmentMarker.exec( name )?.[1];
		if ( marker && marker !== "$root" && !sockets.has( marker ) ) sockets.set( marker, index );
	}
	const thickness = new Float32Array( model.nodes.length ).fill( 1 ), radial = new Float32Array( 16 );
	const locals = model.nodes.map( () => new Float32Array( 16 ) ),
		globals = model.nodes.map( () => new Float32Array( 16 ) );
	const restTranslations = new Float32Array( model.nodes.flatMap( n => [ ...n.translation ] ) ),
		restRotations = new Float32Array( model.nodes.flatMap( n => [ ...n.rotation ] ) ),
		restScales = new Float32Array( model.nodes.flatMap( n => [ ...n.scale ] ) );
	const translationData = restTranslations.slice(),
		rotationData = restRotations.slice(),
		scaleData = restScales.slice();
	const translations = model.nodes.map( ( _, i ) => translationData.subarray( i * 3, i * 3 + 3 ) ),
		rotations = model.nodes.map( ( _, i ) => rotationData.subarray( i * 4, i * 4 + 4 ) ),
		scales = model.nodes.map( ( _, i ) => scaleData.subarray( i * 3, i * 3 + 3 ) );
	const inverseViews = new WeakMap<CharacterPrimitive, Float32Array[]>();
	const palettes = new WeakMap<CharacterPrimitive, { version: number; data: Float32Array; }>();
	let poseVersion = 0, paletteVersion = 0;
	const visits: number[] = [], visited = new Set<number>();
	/*
================
visit
================
	*/
	function visit( n: number ) {
		if ( visited.has( n ) ) {
			return;
		}
		const parent = model.nodes[n]!.parent;
		if ( parent >= 0 ) {
			visit( parent );
		}
		visited.add( n );
		visits.push( n );
	}
	for ( let n = 0; n < model.nodes.length; n++ ) {
		visit( n );
	}
	// Parent-first node order, and scratch for the unsettled ancestor chains
	// of one request (gather): each node enters it at most once.
	const order = Int32Array.from( visits ), chain = new Int32Array( model.nodes.length );
	const animatedLocal = new Uint8Array( model.nodes.length ), changedGlobal = new Uint8Array( model.nodes.length );
	// Per node of the begun evaluation: 0, SETTLING (gathered by the request
	// being settled) or SETTLED. A socket or a palette's entries settle only
	// their ancestor chains; materialize settles the rest.
	const ready = new Uint8Array( model.nodes.length );
	// initialized: every node has settled once. Skipping an unchanged node
	// stays exact after a partial evaluation because only a fully settled one
	// re-enables direct reuse: until then every animated node recomposes, and
	// its change reaches every descendant.
	let initialized = false, directMatrices = false, begun = false, readyCount = 0;
	let directPass = false, reuseMatrices = false, refresh = false, matricesChanged = false;
	let directClocks: ReturnType<typeof createAnimationTimelines> | undefined;
	const clips = new Map<string, CharacterClip>();
	const missingClips = new Set<string>(), unreportedClips: string[] = [];
	const timelines = new Map<CharacterClip, ReturnType<typeof createAnimationTimelines>>();
	// One reusable clip's quaternion brackets, not a cache for every admitted
	// animation. Switching layer clips invalidates this bounded scratch.
	let rotationSamples = new Float64Array(
		model.clips.reduce( ( max, clip ) => Math.max( max, clip.channels.length ), 0 ) * 4
	).fill( NaN );
	let rotationClip: CharacterClip | undefined;
	const singleTrackClips = new Set<CharacterClip>(), gpuClips = new Set<CharacterClip>();
	/*
================
registerClip

The caller owns validation and binding. A catalog entry adds sampling metadata
without changing skeleton storage, the selected layers or their revision.
================
	*/
	function registerClip( clip: CharacterClip ) {
		const paths = new Set<string>();
		let singleTrack = true;
		for ( const channel of clip.channels ) {
			const key = channel.node + ":" + channel.path;
			if ( paths.has( key ) ) singleTrack = false;
			paths.add( key );
			if ( !model.nodes[channel.node]!.matrix ) animatedLocal[channel.node] = 1;
		}
		clips.set( clip.name, clip );
		if ( singleTrack ) singleTrackClips.add( clip );
		if (
			singleTrack && clip.channels.length > 0 &&
			clip.channels.every( channel => channel.interpolation !== "CUBICSPLINE" && channel.times.length > 0 )
		) gpuClips.add( clip );
	}
	for ( const clip of model.clips ) registerClip( clip );
	const pass = model.nodes.map( () => [ new Float32Array( 3 ), new Float32Array( 4 ), new Float32Array( 3 ) ] );
	const weights = new Float32Array( model.nodes.length * 3 ),
		committed = new Float32Array( model.nodes.length * 3 ),
		sample = new Float32Array( 4 );
	const sample3 = sample.subarray( 0, 3 );
	/*
    ================
    ResolvedLayer
    Validated sampling identity retained between frames.
    ================
    */
	type ResolvedLayer = { clip: CharacterClip | undefined; time: number; weight: number; lane: "event" | "timed"; };
	let resolved: ResolvedLayer[] = [], pendingLayers: ResolvedLayer[] = [], hasPose = false;
	// One CCompChar bone rotator (spine-aim.ts): its node, rotation and whether
	// it changed since the last evaluation. A released node recomposes once.
	const boneRotation = new Float32Array( 4 );
	let rotatedNode = -1, releasedNode = -1, rotationChanged = false;
	let cpuPending = false, cpuEvaluations = 0;
	const sampleRequest = { clip: model.clips[0]!, time: 0 };
	/*
================
gpuSample
================
	*/
	function gpuSample(): { readonly clip: CharacterClip; readonly time: number; } | null {
		const layer = resolved[0];
		if (
			volume !== DEFAULT_BODY_VOLUME || resolved.length !== 1 || !layer?.clip || layer.weight !== 1 ||
			!gpuClips.has( layer.clip ) || rotatedNode >= 0
		) return null;
		sampleRequest.clip = layer.clip;
		sampleRequest.time = layer.time;
		return sampleRequest;
	}
	/*
================
sameLocalSample

The direct sampler does not use blend scratch. Compare its retained TRS
words numerically with signed-zero preservation; NaNs always recompute.
================
	*/
	function sameLocalSample( n: number ) {
		for ( let path = 0; path < 3; path++ ) {
			const current = path === 0 ? translations[n]! : path === 1 ? rotations[n]! : scales[n]!;
			const previous = pass[n]![path]!;
			for ( let c = 0; c < current.length; c++ ) {
				const value = current[c]!;
				if ( !Object.is( value, previous[c] ) || Number.isNaN( value ) ) return false;
			}
		}
		return true;
	}
	/*
================
clipClocks

Prepare the clip's quaternion bracket scratch and its timeline cursors at
time, as every sampled layer needs before its channels.
================
	*/
	function clipClocks( clip: CharacterClip, time: number ) {
		// Catalog admission can reach every standing peer. Grow scratch
		// only when this pose samples the new clip, after frame admission
		// has charged its updated pose budget.
		if ( rotationSamples.length < clip.channels.length * 4 ) {
			rotationSamples = new Float64Array( clip.channels.length * 4 );
			rotationClip = undefined;
		}
		if ( rotationClip !== clip ) {
			rotationSamples.fill( NaN );
			rotationClip = clip;
		}
		let clocks = timelines.get( clip );
		if ( !clocks ) {
			clocks = createAnimationTimelines( clip );
			timelines.set( clip, clocks );
		}
		clocks.sample( time );
		return clocks;
	}
	/*
================
sampleChannel

Channel channelIndex of clip at its cursor in clocks, written to target
(width floats). Its result depends only on its values, its cursor and its
bracket scratch, so a direct pass may sample its nodes in any groups and
any order and get the bytes of one whole pass.
================
	*/
	function sampleChannel(
		clip: CharacterClip,
		clocks: ReturnType<typeof createAnimationTimelines>,
		channelIndex: number,
		target: Float32Array,
		width: number
	) {
		const channel = clip.channels[channelIndex]!;
		const { low, next, span, fraction } = clocks.cursors[clocks.index[channelIndex]!]!;
		const values = channel.values;
		if ( channel.interpolation === "CUBICSPLINE" ) {
			const t = fraction, t2 = t * t, t3 = t2 * t;
			for ( let c = 0; c < width; c++ ) {
				target[c] = (2 * t3 - 3 * t2 + 1) * values[(low * 3 + 1) * width + c]! +
					(t3 - 2 * t2 + t) * span * values[(low * 3 + 2) * width + c]! +
					(-2 * t3 + 3 * t2) * values[(next * 3 + 1) * width + c]! +
					(t3 - t2) * span * values[next * 3 * width + c]!;
			}
		} else if ( channel.interpolation === "STEP" || next === low ) {
			for ( let c = 0; c < width; c++ ) target[c] = values[low * width + c]!;
		} else if ( width === 4 ) {
			const at = channelIndex * 4;
			if ( rotationSamples[at] !== low ) {
				let dot = 0;
				for ( let i = 0; i < 4; i++ ) dot += values[low * 4 + i]! * values[next * 4 + i]!;
				const sign = dot < 0 ? -1 : 1;
				dot = Math.min( 1, Math.abs( dot ) );
				const angle = Math.acos( dot );
				rotationSamples[at] = low;
				rotationSamples[at + 1] = sign;
				rotationSamples[at + 2] = angle;
				rotationSamples[at + 3] = Math.sin( angle );
			}
			const sign = rotationSamples[at + 1]!,
				angle = rotationSamples[at + 2]!,
				sin = rotationSamples[at + 3]!;
			const left = sin < 1e-6 ? 1 - fraction : Math.sin( (1 - fraction) * angle ) / sin,
				right = sin < 1e-6 ? fraction : Math.sin( fraction * angle ) / sin;
			for ( let i = 0; i < 4; i++ ) {
				target[i] = values[low * 4 + i]! * left + values[next * 4 + i]! * right * sign;
			}
		} else {
			for ( let c = 0; c < width; c++ ) {
				target[c] = values[low * width + c]! * (1 - fraction) +
					values[next * width + c]! * fraction;
			}
		}
	}
	/*
================
directTarget

A direct pass's destination for node n's path: the sampler output itself.
================
	*/
	function directTarget( n: number, path: number ) {
		return path === 0 ? translations[n]! : path === 1 ? rotations[n]! : scales[n]!;
	}
	/*
================
sampleNodes

Sample a direct pass's channels of chain[0..count) alone, through the
clip's shared node index (clipNodeChannels).
================
	*/
	function sampleNodes( count: number ) {
		const clip = resolved[0]!.clip;
		if ( !clip ) return;
		const index = clipNodeChannels( clip, nodeChannels ), indexed = index.first.length - 1;
		for ( let i = 0; i < count; i++ ) {
			const n = chain[i]!;
			if ( n >= indexed ) continue;
			for ( let k = index.first[n]!; k < index.first[n + 1]!; k++ ) {
				const channelIndex = index.channels[k]!, kind = clip.channels[channelIndex]!.path;
				const path = kind === "translation" ? 0 : kind === "rotation" ? 1 : 2;
				sampleChannel( clip, directClocks!, channelIndex, directTarget( n, path ), path === 1 ? 4 : 3 );
			}
		}
	}
	/*
================
sampleLayers

Sample every channel of the begun evaluation's unsettled nodes: all of
them for a blended pass (begin), the rest of a direct pass (materialize).
================
	*/
	function sampleLayers() {
		const direct = directPass;
		// A single full-weight pass with one channel per node/path has no
		// blending to accumulate. Sample with the identical interpolation,
		// then commit directly. Layered/duplicate-channel inputs keep the
		// native event-before-timed accumulation below.
		if ( !direct ) committed.fill( 0 );
		// CRTBone 0xab56c0 accumulates each pass by w/(sum+w). 0xab5780
		// caps the next pass to remaining headroom; event tracks precede timed tracks.
		for ( let laneIndex = 0; laneIndex < (direct ? 1 : 2); laneIndex++ ) {
			const lane = direct ? resolved[0]!.lane : laneIndex === 0 ? "event" : "timed";
			if ( !direct ) weights.fill( 0 );
			for ( const layer of resolved ) {
				if ( layer.lane !== lane || layer.weight === 0 ) continue;
				if ( !layer.clip ) continue;
				// begin prepared a direct pass's clock once for all its groups.
				const clocks = direct ? directClocks! : clipClocks( layer.clip, layer.time );
				for ( let channelIndex = 0; channelIndex < layer.clip.channels.length; channelIndex++ ) {
					const channel: CharacterClip["channels"][number] = layer.clip.channels[channelIndex]!;
					if ( ready[channel.node] !== 0 ) continue;
					const path = channel.path === "translation" ? 0 : channel.path === "rotation" ? 1 : 2,
						width = path === 1 ? 4 : 3;
					// A single full-weight track owns this node/path. Its
					// float32 destination is the sampler output itself.
					const target = direct ? directTarget( channel.node, path ) : width === 4 ? sample : sample3;
					sampleChannel( layer.clip, clocks, channelIndex, target, width );
					if ( direct ) continue;
					const index = channel.node * 3 + path;
					const out = pass[channel.node]![path]!, before = weights[index]!, total = before + layer.weight;
					if ( before === 0 ) out.set( target );
					else if ( path === 1 ) slerp( out, target, layer.weight / total, out );
					else {for ( let c = 0; c < width; c++ ) {
							out[c] = out[c]! + (target[c]! - out[c]!) * layer.weight / total;
						}}
					weights[index] = total;
				}
			}
			if ( !direct ) {
				for ( let n = 0; n < model.nodes.length; n++ ) {
					for ( let path = 0; path < 3; path++ ) {
						const index = n * 3 + path, weight = Math.min( weights[index]!, 1 - committed[index]! );
						if ( weight <= 0 ) continue;
						const out = path === 0 ? translations[n]! : path === 1 ? rotations[n]! : scales[n]!,
							value = pass[n]![path]!,
							previous = committed[index]!;
						if ( previous === 0 ) out.set( value );
						else if ( path === 1 ) slerp( out, value, weight / (previous + weight), out );
						else {for ( let c = 0; c < 3; c++ ) {
								out[c] = out[c]! + (value[c]! - out[c]!) * weight / (previous + weight);
							}}
						committed[index] = previous + weight;
					}
				}
			}
		}
	}
	/*
================
begin

Start materializing the pending evaluation: everything its nodes share. A
blended pass accumulates every node's local TRS here. A direct pass (one
full-weight layer with one channel per node/path) only prepares its clock:
each group of nodes samples its own channels before it settles.
================
	*/
	function begin() {
		const layer = resolved[0];
		const direct = resolved.length === 1 && layer!.weight === 1 &&
			(!layer!.clip || singleTrackClips.has( layer!.clip ));
		directPass = direct;
		reuseMatrices = direct && directMatrices;
		// A blended pass overwrites scratch. A failed sample, or a partial
		// evaluation whose unsettled nodes keep older matrices than their
		// settled parents, must also leave reuse disabled until a complete
		// materialization publishes new matrices.
		directMatrices = false;
		refresh = !initialized;
		matricesChanged = refresh;
		// Reset contiguous float32 rest poses in three copies instead of
		// three small typed-array calls per node. Channel views stay stable.
		translationData.set( restTranslations );
		rotationData.set( restRotations );
		scaleData.set( restScales );
		ready.fill( 0 );
		readyCount = 0;
		if ( direct && layer!.clip ) directClocks = clipClocks( layer!.clip, layer!.time );
		else if ( !direct ) sampleLayers();
		begun = true;
		cpuEvaluations++;
	}
	/*
================
settle

Compute the local and global matrices of nodes[first..end) for the begun
evaluation, skipping settled ones; every parent precedes its children or has
settled, and a direct pass has sampled each node's channels. A node depends
only on its own TRS and its parent's global matrix, so any parent-first
order gives the bytes of the full traversal.
================
	*/
	function settle( nodes: Int32Array, first: number, end: number ) {
		const rotator = rotatedNode, released = releasedNode, all = refresh, reuse = reuseMatrices, direct = directPass;
		let changed = false, count = 0;
		for ( let i = first; i < end; i++ ) {
			const n = nodes[i]!;
			if ( ready[n] === SETTLED ) continue;
			const node = model.nodes[n]!;
			// A9ADD0 multiplies the rotator after the sampled local rotation.
			if ( n === rotator ) multiplyQuaternion( boneRotation, rotations[n]!, rotations[n]! );
			let localChanged = all;
			const rotated = n === rotator || n === released;
			if ( all || animatedLocal[n] || rotated ) {
				localChanged = rotated || !reuse || !sameLocalSample( n );
				if ( localChanged ) {
					if ( node.matrix ) {
						locals[n]!.set( node.matrix );
					} else {
						compose( translations[n]!, rotations[n]!, scales[n]!, locals[n]! );
					}
					if ( direct ) {
						pass[n]![0]!.set( translations[n]! );
						pass[n]![1]!.set( rotations[n]! );
						pass[n]![2]!.set( scales[n]! );
					}
				}
			}
			// Parent-first settling propagates a changed ancestor through static
			// descendants. Unchanged local and parent bytes imply identical globals.
			const globalChanged = localChanged || (node.parent >= 0 && changedGlobal[node.parent] !== 0);
			changedGlobal[n] = globalChanged ? 1 : 0;
			if ( globalChanged ) {
				if ( node.parent < 0 ) {
					globals[n]!.set( locals[n]! );
				} else {
					multiply( globals[node.parent]!, locals[n]!, globals[n]! );
				}
				changed = true;
			}
			ready[n] = SETTLED;
			count++;
		}
		if ( changed ) matricesChanged = true;
		readyCount += count;
	}
	/*
================
publishSettled

Publish a fully settled evaluation.
================
	*/
	function publishSettled() {
		if ( matricesChanged ) paletteVersion++;
		initialized = true;
		releasedNode = -1;
		directMatrices = directPass;
		cpuPending = false;
		begun = false;
	}
	/*
================
gather

Append to chain[end..) node n's ancestors that neither have settled nor
were gathered by this request, root first, and mark them SETTLING; returns
the new end. An ancestor gathered earlier sits earlier in chain, so the
whole run stays parent first.
================
	*/
	function gather( n: number, end: number ): number {
		let low = end;
		for ( let at = n; at >= 0 && ready[at] === 0; at = model.nodes[at]!.parent ) {
			chain[end++] = at;
			ready[at] = SETTLING;
		}
		for ( let high = end - 1; low < high; low++, high-- ) {
			const swap = chain[low]!;
			chain[low] = chain[high]!;
			chain[high] = swap;
		}
		return end;
	}
	/*
================
settleGathered

Sample (a direct pass) and settle chain[0..count) of the begun evaluation;
the rest stays pending for a later request. The last node settled
publishes the evaluation, as materialize does.
================
	*/
	function settleGathered( count: number ) {
		if ( directPass ) sampleNodes( count );
		settle( chain, 0, count );
		if ( readyCount === model.nodes.length ) publishSettled();
	}
	/*
================
materializeNode

Settle only node n's ancestor chain of the pending evaluation. A socket of
a GPU-sampled pose needs no more.
================
	*/
	function materializeNode( n: number ) {
		if ( !cpuPending ) return;
		if ( !begun ) begin();
		settleGathered( gather( n, 0 ) );
	}
	/*
================
materializeEntries

Settle only the ancestor chains of the joints behind palette entries
entries of a primitive with joints. A cloth step reads no others.
================
	*/
	function materializeEntries( joints: readonly number[], entries: Int32Array ) {
		if ( !cpuPending ) return;
		if ( !begun ) begin();
		let end = 0;
		for ( let i = 0; i < entries.length; i++ ) end = gather( joints[entries[i]!]!, end );
		settleGathered( end );
	}
	/*
================
materialize

Settle every node of the pending evaluation not yet settled.
================
	*/
	function materialize() {
		if ( !cpuPending ) return;
		if ( !begun ) begin();
		if ( directPass ) sampleLayers();
		settle( order, 0, order.length );
		publishSettled();
	}
	/*
================
paletteEntry

The palette entry of joint, whose inverse bind is view, into out at at.
================
	*/
	function paletteEntry( joint: number, view: Float32Array, out: Float32Array, at: number ) {
		const factor = thickness[joint]!;
		let matrix = globals[joint]!;
		// AB6B20 scales radial Y/Z rows of the native global matrix,
		// then multiplies inverse bind. Children and sockets stay unchanged.
		if ( factor !== 1 ) {
			radial.set( matrix );
			for ( const n of RADIAL_WORDS ) radial[n]! *= factor;
			matrix = radial;
		}
		multiply( matrix, view, out, at );
	}
	/*
================
setBodyVolume
================
	*/
	function setBodyVolume( index = DEFAULT_BODY_VOLUME, isFemale = false ) {
		if ( volume === index && female === isFemale ) return;
		volume = index;
		female = isFemale;
		poseVersion++;
		paletteVersion++;
		for ( let i = 0; i < model.nodes.length; i++ ) {
			thickness[i] = bodyBoneScale( model.nodes[i]!.name, index, isFemale );
		}
	}
	return {
		/*
================
takeMissingClips

The clip names evaluate found missing since the last take, each once per
evaluator lifetime (MAX_MISSING_CLIPS at most).
================
		*/
		takeMissingClips(): string[] {
			return unreportedClips.splice( 0 );
		},
		/*
================
admitClip

Append a validated bound clip without retiring active playback or palettes.
Names are immutable once admitted, matching renderer clip admission. Longer
quaternion scratch is charged and allocated only when playback needs it.
================
		*/
		admitClip( clip: CharacterClip ) {
			if ( clips.has( clip.name ) ) return false;
			// A begun evaluation settles with the catalogue it began under,
			// as one whole materialization before this admission did.
			if ( begun ) materialize();
			registerClip( clip );
			// A newly animated node has no retained direct sample in blend
			// scratch; the next materialization must not compare against it.
			directMatrices = false;
			return true;
		},
		/*
        ================
        bodyVolume
        ================
        */
		bodyVolume: setBodyVolume,
		revision: () => poseVersion,
		/*
        ================
        setBoneRotation

        Install, update or drop the named bone's rotator rotation (x y z w in
        model space) before the next evaluate. A bone the model lacks is ignored,
        as CAnimTrack_FindMarkerByName fails in A9C370.
        ================
        */
		setBoneRotation( bone: string, rotation: readonly number[] | null ) {
			const node = rotation ? sockets.get( bone ) ?? -1 : -1;
			if ( node < 0 ) {
				if ( rotatedNode < 0 ) return;
				// A begun evaluation settles with the rotator it began under.
				if ( begun ) materialize();
				releasedNode = rotatedNode;
				rotatedNode = -1;
				rotationChanged = true;
				return;
			}
			if (
				node === rotatedNode && boneRotation[0] === rotation![0] && boneRotation[1] === rotation![1] &&
				boneRotation[2] === rotation![2] && boneRotation[3] === rotation![3]
			) return;
			if ( begun ) materialize();
			if ( rotatedNode >= 0 && rotatedNode !== node ) releasedNode = rotatedNode;
			rotatedNode = node;
			boneRotation.set( rotation! );
			rotationChanged = true;
		},
		/*
        ================
        evaluate
        ================
        */
		evaluate( name: string, seconds: number, loop = true, layers?: readonly CharacterLayer[], defer = false ) {
			// Several independent producers may each retain outgoing 200 ms fades.
			// Their combined population is not bounded by eight. The mixer uses
			// skeleton-sized scratch and must preserve every live sparse track.
			const count = layers?.length ?? 1;
			let changed = !hasPose || count !== resolved.length || rotationChanged;
			for ( let i = 0; i < count; i++ ) {
				const source = layers?.[i],
					clipName = source ? source.clip : name,
					rawTime = source ? source.time : seconds,
					weight = source ? source.weight : 1,
					lane = source ? source.lane : "timed",
					repeat = source ? source.loop : loop;
				if (
					layers && !source || !Number.isFinite( rawTime ) || rawTime < 0 || !Number.isFinite( weight ) ||
					weight < 0 || weight > 1 || (lane !== "event" && lane !== "timed")
				) throw new Error( "Invalid animation layer" );
				const clip = clips.get( clipName );
				// A clip the model lacks plays the rest pose, as an unnamed
				// layer does, and is recorded for the owner to report
				// (takeMissingClips): one model's catalogue gap costs that
				// motion, never the renderer. Inference: the original plays
				// no motion for an animation it cannot find and keeps drawing.
				if ( !clip && clipName && !missingClips.has( clipName ) && missingClips.size < MAX_MISSING_CLIPS ) {
					missingClips.add( clipName );
					unreportedClips.push( clipName );
				}
				const time = clip?.duration ?
					(repeat ? rawTime % clip.duration : Math.min( rawTime, clip.duration )) :
					0;
				const previous = resolved[i];
				if (
					!previous || previous.clip !== clip || previous.time !== time || previous.weight !== weight ||
					previous.lane !== lane
				) changed = true;
				const pending = pendingLayers[i];
				if ( pending ) {
					pending.clip = clip;
					pending.time = time;
					pending.weight = weight;
					pending.lane = lane;
				} else pendingLayers[i] = { clip, time, weight, lane };
			}
			// Validate the entire request before publishing its identity. A bad
			// later layer must not poison the retained pose or its revision.
			if ( !changed ) {
				if ( !defer ) materialize();
				return false;
			}
			pendingLayers.length = count;
			rotationChanged = false;
			const previous = resolved;
			resolved = pendingLayers;
			pendingLayers = previous;
			hasPose = true;
			poseVersion++;
			cpuPending = true;
			begun = false;
			if ( !defer || !gpuSample() ) materialize();
			return true;
		},
		gpuSample,
		cpuEvaluations: () => cpuEvaluations,
		/*
        ================
        inputs

        Copies the inputs of the skeleton this pose holds, or would build on
        its next palette, into into (CharacterPoseInputs). Cheap: a few
        numbers a layer, no skeleton. False for a pose never evaluated.
        ================
        */
		inputs( into: CharacterPoseInputs ): boolean {
			if ( !hasPose || rotationChanged ) return false;
			into.layers.length = resolved.length;
			for ( let i = 0; i < resolved.length; i++ ) {
				const layer = resolved[i]!, copy = into.layers[i];
				if ( copy ) {
					copy.clip = layer.clip;
					copy.time = layer.time;
					copy.weight = layer.weight;
					copy.lane = layer.lane;
				} else into.layers[i] = { ...layer };
			}
			into.volume = volume;
			into.female = female;
			into.rotated = rotatedNode;
			into.rotation.set( boneRotation );
			return true;
		},
		/*
        ================
        restore

        Takes the inputs another evaluator of this model read (inputs): the
        next palette or socket is the skeleton that evaluator held then. A
        clip this evaluator has not admitted is registered first, so it
        samples it as the other did.
        ================
        */
		restore( inputs: CharacterPoseInputs ) {
			setBodyVolume( inputs.volume, inputs.female );
			if ( rotatedNode >= 0 && rotatedNode !== inputs.rotated ) releasedNode = rotatedNode;
			rotatedNode = inputs.rotated;
			boneRotation.set( inputs.rotation );
			pendingLayers.length = inputs.layers.length;
			for ( let i = 0; i < inputs.layers.length; i++ ) {
				const layer = inputs.layers[i]!;
				if ( layer.clip && clips.get( layer.clip.name ) !== layer.clip ) {
					registerClip( layer.clip );
					directMatrices = false;
				}
				pendingLayers[i] = { ...layer };
			}
			const previous = resolved;
			resolved = pendingLayers;
			pendingLayers = previous;
			rotationChanged = false;
			hasPose = true;
			poseVersion++;
			cpuPending = true;
			// A begun evaluation belongs to the inputs replaced here: what it
			// settled is not these inputs' skeleton. Its begin left reuse off.
			begun = false;
		},
		/*
        ================
        palette

        primitive's skin palette into out at offset. entries (ascending palette
        indices) asks for those alone: only their joints' ancestor chains are
        built, only they are written, each with the bytes of the whole palette,
        and the rest of out is left as it was. A whole palette is cached per
        binding and revision; a partial one is not, and leaves the cache alone.
        ================
        */
		palette( primitive: CharacterPrimitive, out: Float32Array, offset = 0, entries?: Int32Array ) {
			primitive = bindings.get( primitive ) ?? primitive;
			let views = inverseViews.get( primitive );
			if ( !views ) {
				views = Array.from(
					primitive.joints,
					( _, i ) => primitive.inverseBind.subarray( i * 16, i * 16 + 16 )
				);
				inverseViews.set( primitive, views );
			}
			if ( entries ) {
				materializeEntries( primitive.joints, entries );
				for ( let k = 0; k < entries.length; k++ ) {
					const i = entries[k]!;
					paletteEntry( primitive.joints[i]!, views[i]!, out, offset + i * 16 );
				}
				return;
			}
			materialize();
			let cached = palettes.get( primitive );
			if ( !cached ) {
				cached = { version: -1, data: new Float32Array( primitive.joints.length * 16 ) };
				palettes.set( primitive, cached );
			}
			if ( cached.version !== paletteVersion ) {
				for ( let i = 0; i < primitive.joints.length; i++ ) {
					paletteEntry( primitive.joints[i]!, views[i]!, cached.data, i * 16 );
				}
				cached.version = paletteVersion;
			}
			out.set( cached.data, offset );
		},
		/*
        ================
        socket

        The first compound marker's sampled matrix, or a named private branch
        when the caller supplies an explicit equipment handle.
        ================
        */
		socket( name: string, compound = false ) {
			const index = sockets.get( name );
			if ( index === undefined ) return null;
			// Missing markers fall back to the holder or mount. Their immutable
			// lookup needs no CPU bones; keep a GPU-owned pose lazy until used,
			// and then build only the marker's ancestor chain.
			materializeNode( index );
			const current = globals[index]!;
			if ( !compound ) return current.slice();
			// AB58C5..AB5924 cancels bind rotation but copies current world position.
			// The child resource supplies its own import adapter exactly once.
			const rest = attachmentBind( index ), result = current.slice();
			for ( let c = 0; c < 3; c++ ) {
				for ( let r = 0; r < 3; r++ ) {
					result[c * 4 + r] = current[r]! * rest[c]! + current[4 + r]! * rest[4 + c]! +
						current[8 + r]! * rest[8 + c]!;
				}
			}
			return result;
		}
	};
}
