/*
===========================================================================

animation-pose.ts - retained character pose sampling and skin palettes

Owns clip sampling, native event-before-timed blending, and lazy CPU palette
materialization. Layer lifetimes belong to the presentation producers; this
consumer evaluates every live fade without truncating sparse bone tracks.

===========================================================================
*/
import { createAnimationTimelines } from "./animation-timelines";
import type { CharacterModel, CharacterPrimitive, CharacterLayer, CharacterClip } from "@/engine/contracts/character";
import { compose, multiplyDisjoint as multiply, slerp } from "@/engine/foundation/math/pose-math";
import { paletteBindings } from "./palette-bindings";
import { bodyBoneScale } from "./body-shape";
/*
================
AnimationPoseProbe
Optional diagnostics injected at construction; no globals or clocks are read
by ordinary pose evaluation. A ceiling probe may freeze already-warm poses.
================
*/
export interface AnimationPoseProbe {
	ceiling?: {
		skip( ready: boolean, eligible: boolean ): boolean;
		palette( built: boolean, joints: number, eligible: boolean ): void;
	};
	phases?: {
		begin(
			model: CharacterModel,
			reason: string,
			layers: readonly { clip: CharacterClip | undefined; }[]
		): { phases: boolean; start( name: string ): void; end( name: string ): void; } | null;
	};
}

/*
================
createCharacterPose
================
*/
export function createCharacterPose( model: CharacterModel, probe?: AnimationPoseProbe ) {
	const bindings = paletteBindings( model );
	const ceilingEligible = !!probe?.ceiling && model.primitives.some( p => p.joints.length > 1 ) &&
		!model.primitives.some( p => p.emission || p.ribbon );
	let volume = 2, female = false;
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
	let poseVersion = 0;
	const order: number[] = [], visited = new Set<number>();
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
		order.push( n );
	}
	for ( let n = 0; n < model.nodes.length; n++ ) {
		visit( n );
	}
	const animatedLocal = new Uint8Array( model.nodes.length ), animatedGlobal = new Uint8Array( model.nodes.length );
	let matricesInitialized = false;
	for ( const clip of model.clips ) {
		for ( const channel of clip.channels ) {
			if ( !model.nodes[channel.node]!.matrix ) animatedLocal[channel.node] = 1;
		}
	}
	for ( const n of order ) {
		const parent = model.nodes[n]!.parent;
		animatedGlobal[n] = animatedLocal[n]! || (parent >= 0 ? animatedGlobal[parent]! : 0);
	}
	const clips = new Map( model.clips.map( clip => [ clip.name, clip ] ) );
	const timelines = new Map<CharacterClip, ReturnType<typeof createAnimationTimelines>>();
	// One reusable clip's quaternion brackets, not a cache for every admitted
	// animation. Switching layer clips invalidates this bounded scratch.
	const rotationSamples = new Float64Array(
		model.clips.reduce( ( max, clip ) => Math.max( max, clip.channels.length ), 0 ) * 4
	).fill( NaN );
	let rotationClip: CharacterClip | undefined;
	const singleTrackClips = new Set( model.clips.filter( clip => {
		const paths = new Set<string>();
		for ( const channel of clip.channels ) {
			const key = channel.node + ":" + channel.path;
			if ( paths.has( key ) ) return false;
			paths.add( key );
		}
		return true;
	} ) );
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
	let cpuPending = false, cpuEvaluations = 0;
	const gpuClips = new Set(
		model.clips.filter( clip =>
			singleTrackClips.has( clip ) && clip.channels.length > 0 &&
			clip.channels.every( channel => channel.interpolation !== "CUBICSPLINE" && channel.times.length > 0 )
		)
	);
	const sampleRequest = { clip: model.clips[0]!, time: 0 };
	/*
================
gpuSample
================
	*/
	function gpuSample(): { readonly clip: CharacterClip; readonly time: number; } | null {
		const layer = resolved[0];
		if (
			volume !== 2 || resolved.length !== 1 || !layer?.clip || layer.weight !== 1 || !gpuClips.has( layer.clip )
		) return null;
		sampleRequest.clip = layer.clip;
		sampleRequest.time = layer.time;
		return sampleRequest;
	}
	/*
================
materialize
================
	*/
	function materialize( reason = "evaluate" ) {
		if ( !cpuPending ) return;
		const timer = probe?.phases?.begin( model, reason, resolved );
		if ( timer && !timer.phases ) timer.start( "materialization" );
		// A single full-weight pass with one channel per node/path has no
		// blending to accumulate. Sample with the identical interpolation,
		// then commit directly. Layered/duplicate-channel inputs keep the
		// native event-before-timed accumulation below.
		const direct = resolved.length === 1 && resolved[0]!.weight === 1 &&
			(!resolved[0]!.clip || singleTrackClips.has( resolved[0]!.clip ));
		// Reset contiguous float32 rest poses in three copies instead of
		// three small typed-array calls per node. Channel views stay stable.
		translationData.set( restTranslations );
		rotationData.set( restRotations );
		scaleData.set( restScales );
		if ( !direct ) committed.fill( 0 );
		// CRTBone 0xab56c0 accumulates each pass by w/(sum+w). 0xab5780
		// caps the next pass to remaining headroom; event tracks precede timed tracks.
		for ( let laneIndex = 0; laneIndex < (direct ? 1 : 2); laneIndex++ ) {
			const lane = direct ? resolved[0]!.lane : laneIndex === 0 ? "event" : "timed";
			if ( !direct ) weights.fill( 0 );
			for ( const layer of resolved ) {
				if ( layer.lane !== lane || layer.weight === 0 ) continue;
				const time = layer.time;
				if ( !layer.clip ) continue;
				if ( rotationClip !== layer.clip ) {
					rotationSamples.fill( NaN );
					rotationClip = layer.clip;
				}
				let clocks = timelines.get( layer.clip );
				if ( !clocks ) {
					clocks = createAnimationTimelines( layer.clip );
					timelines.set( layer.clip, clocks );
				}
				if ( timer?.phases ) timer.start( "timelines" );
				clocks.sample( time );
				if ( timer?.phases ) {
					timer.end( "timelines" );
					timer.start( "sampling" );
				}
				for ( let channelIndex = 0; channelIndex < layer.clip.channels.length; channelIndex++ ) {
					const channel: CharacterClip["channels"][number] = layer.clip.channels[channelIndex]!;
					const path = channel.path === "translation" ? 0 : channel.path === "rotation" ? 1 : 2,
						width = path === 1 ? 4 : 3;
					// A single full-weight track owns this node/path. Its
					// float32 destination is the sampler output itself.
					const target = direct ?
						(path === 0 ?
							translations[channel.node]! :
							path === 1 ?
							rotations[channel.node]! :
							scales[channel.node]!) :
						width === 4 ?
						sample :
						sample3;
					const { low, next, span, fraction } = clocks.channels[channelIndex]!;
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
						if ( timer?.phases ) timer.start( "quaternion" );
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
						if ( timer?.phases ) timer.end( "quaternion" );
					} else {
						for ( let c = 0; c < width; c++ ) {
							target[c] = values[low * width + c]! * (1 - fraction) +
								values[next * width + c]! * fraction;
						}
					}
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
				if ( timer?.phases ) timer.end( "sampling" );
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
		for ( const n of order ) {
			const node = model.nodes[n]!;
			if ( !matricesInitialized || animatedLocal[n] ) {
				if ( node.matrix ) {
					locals[n]!.set( node.matrix );
				} else {
					if ( timer?.phases ) timer.start( "composition" );
					compose( translations[n]!, rotations[n]!, scales[n]!, locals[n]! );
					if ( timer?.phases ) timer.end( "composition" );
				}
			}
			if ( !matricesInitialized || animatedGlobal[n] ) {
				if ( node.parent < 0 ) {
					globals[n]!.set( locals[n]! );
				} else {
					if ( timer?.phases ) timer.start( "propagation" );
					multiply( globals[node.parent]!, locals[n]!, globals[n]! );
					if ( timer?.phases ) timer.end( "propagation" );
				}
			}
		}
		matricesInitialized = true;
		cpuPending = false;
		cpuEvaluations++;
		if ( timer && !timer.phases ) timer.end( "materialization" );
	}
	return {
		/*
        ================
        bodyVolume
        ================
        */
		bodyVolume( index = 2, isFemale = false ) {
			if ( volume === index && female === isFemale ) return;
			volume = index;
			female = isFemale;
			poseVersion++;
			for ( let i = 0; i < model.nodes.length; i++ ) {
				thickness[i] = bodyBoneScale( model.nodes[i]!.name, index, isFemale );
			}
		},
		revision: () => poseVersion,
		/*
        ================
        evaluate
        ================
        */
		evaluate( name: string, seconds: number, loop = true, layers?: readonly CharacterLayer[], defer = false ) {
			if ( probe?.ceiling?.skip( hasPose, ceilingEligible ) ) return false;
			// Several independent producers may each retain outgoing 200 ms fades.
			// Their combined population is not bounded by eight. The mixer uses
			// skeleton-sized scratch and must preserve every live sparse track.
			const count = layers?.length ?? 1;
			let changed = !hasPose || count !== resolved.length;
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
				if ( !clip && clipName ) throw new Error( `Missing animation ${clipName}` );
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
			const previous = resolved;
			resolved = pendingLayers;
			pendingLayers = previous;
			hasPose = true;
			poseVersion++;
			cpuPending = true;
			if ( !defer || !gpuSample() ) materialize();
			return true;
		},
		gpuSample,
		cpuEvaluations: () => cpuEvaluations,
		/*
        ================
        palette
        ================
        */
		palette( primitive: CharacterPrimitive, out: Float32Array, offset = 0 ) {
			materialize( "palette" );
			primitive = bindings.get( primitive ) ?? primitive;
			let views = inverseViews.get( primitive );
			if ( !views ) {
				views = Array.from(
					primitive.joints,
					( _, i ) => primitive.inverseBind.subarray( i * 16, i * 16 + 16 )
				);
				inverseViews.set( primitive, views );
			}
			let cached = palettes.get( primitive );
			if ( !cached ) {
				cached = { version: -1, data: new Float32Array( primitive.joints.length * 16 ) };
				palettes.set( primitive, cached );
			}
			probe?.ceiling?.palette( cached.version !== poseVersion, primitive.joints.length, ceilingEligible );
			if ( cached.version !== poseVersion ) {
				for ( let i = 0; i < primitive.joints.length; i++ ) {
					const joint = primitive.joints[i]!, factor = thickness[joint]!;
					let matrix = globals[joint]!;
					// AB6B20 scales radial Y/Z rows of the native global matrix,
					// then multiplies inverse bind. Children and sockets stay unchanged.
					if ( factor !== 1 ) {
						radial.set( matrix );
						for ( const n of [ 4, 5, 6, 8, 9, 10 ] ) radial[n]! *= factor;
						matrix = radial;
					}
					multiply( matrix, views[i]!, cached.data, i * 16 );
				}
				cached.version = poseVersion;
			}
			out.set( cached.data, offset );
		},
		/*
        ================
        socket
        ================
        */
		socket( name: string ) {
			materialize( "socket:" + name );
			const index = model.nodes.findIndex( node => node.name === name );
			return index < 0 ? null : globals[index]!.slice();
		}
	};
}
