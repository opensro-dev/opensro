/*
===========================================================================

resources.ts - priority admission and lifetime of character asset requests

===========================================================================
*/
import {
	characterBytes,
	CHARACTER_MODELS,
	CHARACTER_RESIDENT_BYTES,
	CHARACTER_SOURCE_BYTES
} from "@/engine/foundation/animation/character-budget";
import { assetRequestBudget } from "@/engine/foundation/assets/asset-budget";
import { decodeNativeClip } from "@/engine/foundation/animation/native-clip";
import type { AssetOwner } from "@/engine/contracts/assets";
import type { Renderer } from "@/engine/contracts/runtime";
// A failed resource has its own retry deadline. It never stops actor reconciliation.

/*
================
createCharacterResources

Own one decode reservation and a bounded cache of admitted character sources.
================
*/
export function createCharacterResources( assets: AssetOwner, renderer: Renderer, origin: string ) {
	const durations = new Map<string, ReadonlyMap<string, number>>();
	const loaded = new Set<string>(), wanted = new Set<string>(), planned = new Set<string>();
	const costs = new Map<string, number>();
	let plannedBytes = 0, reserved: string | null = null;
	// Ordered requests are priority admission, not a list of everything nearby.
	// A decoded source is charged what it measured. A source that has never been
	// decoded reserves one source class, and only the single load slot may hold
	// that reservation: an unstarted load must never be charged for residency a
	// resident actor still needs. Reservations are headroom, never a charge, so
	// speculation cannot displace an actor that is already decoded.

	/*
	================
	reservable

	A cold source can reserve room only when the single load slot can start it.
	================
	*/
	function reservable( path: string ): boolean {
		if ( job ) return job.path === path;
		return reserved === null && !disposed && assets.available() > 0 &&
			(failures.get( path )?.retryAt ?? -Infinity) <= now;
	}

	/*
	================
	plan

	Charge shared sources once and reserve at most one undecoded source.
	================
	*/
	function plan( paths: readonly string[] ): boolean {
		// A path admitted this frame is already charged and wanted. Shared
		// crowd models must not allocate a Set/filter for each additional actor.
		if ( paths.every( path => planned.has( path ) ) ) return true;
		const extra = [ ...new Set( paths ) ].filter( path => !planned.has( path ) );
		// Equipped actors admit their parts one source at a time. A group that
		// needs two undecoded sources cannot start both and reserves neither.
		const cold = extra.filter( path => !costs.has( path ) );
		if ( cold.length > 1 || (cold.length === 1 && !reservable( cold[0]! )) ) return false;
		const bytes = extra.reduce( ( sum, path ) => sum + (costs.get( path ) ?? 0), 0 );
		if (
			planned.size + extra.length > CHARACTER_MODELS ||
			plannedBytes + bytes + (cold.length ? CHARACTER_SOURCE_BYTES : 0) > CHARACTER_RESIDENT_BYTES
		) return false;
		for ( const path of paths ) {
			planned.add( path );
			wanted.add( path );
		}
		plannedBytes += bytes;
		if ( cold.length ) reserved = cold[0]!;
		return true;
	}
	const failures = new Map<string, {
		retryAt: number;
		message: string;
	}>();
	let job: {
		id: number;
		path: string;
		model: boolean;
		animation?: { body: string; name: string; };
	} | null = null;
	let now = 0, disposed = false;

	/*
	================
	request

	Submit only published resources whose independent retry deadline has elapsed.
	================
	*/
	function request( path: string, model: boolean ) {
		if ( model && !plan( [ path ] ) ) return;
		wanted.add( path );
		if (
			assets.available() === 0 || disposed || job || loaded.has( path ) ||
			(failures.get( path )?.retryAt ?? -Infinity) > now
		) {
			return;
		}
		try {
			if ( !path.startsWith( "/assets/" ) || path.includes( ".." ) || path.includes( "\\" ) ) {
				throw new Error( "Unpublished character resource" );
			}
			const decode = model ? (path.includes( "/effects/programs.json#" ) ? "effect" : "character") : undefined;
			job = {
				id: assets.request( new URL( path, origin ).href, assetRequestBudget( decode ), decode ),
				path,
				model
			};
		} catch ( error ) {
			failures.set( path, { retryAt: now + 2, message: String( error ) } );
		}
	}
	return {
		/*
		================
		duration

		Read a clip duration from the admitted model or native animation metadata.
		================
		*/
		duration( path: string, clip: string ) {
			return durations.get( path )?.get( clip ) ?? 0;
		},

		/*
		================
		begin

		Reset frame admission intent while preserving the inactive bounded cache.
		================
		*/
		begin( seconds: number ) {
			now = seconds;
			wanted.clear();
			planned.clear();
			plannedBytes = 0;
			reserved = null;
		},

		/*
		================
		poll

		Transfer a completed source to the renderer, releasing rejected image resources.
		================
		*/
		poll(): {
			path: string;
			buffer: ArrayBuffer;
		} | null {
			if ( !job || disposed ) {
				return null;
			}
			const result = assets.take( job.id );
			if ( !result ) {
				return null;
			}
			const completed = job;
			job = null;
			try {
				if ( result.kind === "error" ) {
					throw new Error( result.error );
				}
				if ( completed.model && result.kind === "character" ) {
					try {
						costs.set( completed.path, characterBytes( result.model, result.images ) );
					} catch ( error ) {
						for ( const image of result.images ) if ( !("kind" in image) ) image.close();
						throw error;
					}
					renderer.setCharacterModel( completed.path, result.model, result.images );
					durations.set(
						completed.path,
						new Map( result.model.clips.map( clip => [ clip.name, clip.duration ] ) )
					);
					loaded.add( completed.path );
					failures.delete( completed.path );
				} else if ( completed.animation && result.kind === "bytes" ) {
					const { body, name } = completed.animation,
						source = decodeNativeClip( new Uint8Array( result.buffer ) );
					costs.set( body, renderer.setCharacterAnimation( body, name, source ) );
					durations.set( body, new Map( [ ...(durations.get( body ) ?? []), [ name, source.duration ] ] ) );
					failures.delete( completed.path );
				} else if ( !completed.model && result.kind === "bytes" ) {
					return { path: completed.path, buffer: result.buffer };
				} else {
					if ( result.kind === "image" ) {
						result.image.close();
					}
					if ( result.kind === "character" ) {
						for ( const image of result.images ) {
							if ( !("kind" in image) ) image.close();
						}
					}
					throw new Error( "Character resource result mismatch" );
				}
			} catch ( error ) {
				failures.set( completed.path, { retryAt: now + 2, message: String( error ) } );
			}
			return null;
		},

		/*
		================
		manifest

		Use the same request slot for metadata without a model residency reservation.
		================
		*/
		manifest( path: string ) {
			request( path, false );
		},

		/*
		================
		animation

		Load an authored clip only after its owning body has been admitted.
		================
		*/
		animation( body: string, name: string, path: string ) {
			if ( !loaded.has( body ) ) return false;
			if ( durations.get( body )?.has( name ) ) return true;
			if ( !path.startsWith( "/assets/anim/" ) || !path.endsWith( ".ban" ) ) {
				throw Error( "Unpublished native motion" );
			}
			const pending = job;
			request( path, false );
			if ( !pending && job && job.path === path ) job.animation = { body, name };
			return false;
		},

		/*
		================
		accepted

		Clear the retry record after the consumer accepts the resource.
		================
		*/
		accepted( path: string ) {
			failures.delete( path );
		},

		/*
		================
		rejected

		Keep the failing source isolated behind its own retry deadline.
		================
		*/
		rejected( path: string, error: unknown ) {
			failures.set( path, { retryAt: now + 2, message: String( error ) } );
		},
		plan,

		/*
		================
		ready

		Prioritize wanted sources and move admitted entries to the recent end of the cache.
		================
		*/
		ready( path: string ) {
			if ( disposed ) return false;
			// A failing source keeps its own retry deadline and stays wanted so
			// retainWanted does not drop the deadline and refetch every frame.
			if ( !loaded.has( path ) && (failures.get( path )?.retryAt ?? -Infinity) > now ) {
				wanted.add( path );
				return false;
			}
			if ( !plan( [ path ] ) ) return false;
			request( path, true );
			if ( !loaded.has( path ) ) return false;
			// Most recently used last: the cache retires the oldest inactive
			// source first when the frame takes its room back.
			loaded.delete( path );
			loaded.add( path );
			return true;
		},

		/*
		================
		retainWanted

		Preserve wanted sources first, then retain inactive sources within the remaining budget.
		================
		*/
		retainWanted( assemblies: readonly string[] ) {
			if ( job && !wanted.has( job.path ) ) {
				assets.cancel( job.id );
				job = null;
			}
			// A finished attack is not a resource invalidation. Keep decoded
			// models in the remaining bounded budget, most recently used first.
			// The in-flight source class is held back, so inactive cache entries
			// retire before the next poll can admit its decode.
			let bytes = plannedBytes +
					(reserved !== null || (job !== null && !costs.has( job.path )) ? CHARACTER_SOURCE_BYTES : 0),
				count = planned.size;
			const keep = new Set( planned );
			for ( const path of [ ...loaded ].reverse() ) {
				if ( !keep.has( path ) ) {
					const cost = costs.get( path )!;
					if ( count < CHARACTER_MODELS && bytes + cost <= CHARACTER_RESIDENT_BYTES ) {
						keep.add( path );
						count++;
						bytes += cost;
					}
				}
			}
			for ( const path of loaded ) {
				if ( !keep.has( path ) ) {
					loaded.delete( path );
					durations.delete( path );
					costs.delete( path );
				}
			}
			for ( const path of failures.keys() ) {
				if ( !wanted.has( path ) ) {
					failures.delete( path );
				}
			}
			renderer.retainCharacterModels( [ ...wanted, ...loaded, ...assemblies ] );
		},

		/*
		================
		failed

		Expose the per-source failure state without disturbing unrelated admission.
		================
		*/
		failed( path: string ) {
			return failures.has( path );
		},

		/*
		================
		error

		Report one retained failure for runtime diagnostics.
		================
		*/
		error() {
			return failures.values().next().value?.message ?? null;
		},

		/*
		================
		reset

		Cancel pending work and release all renderer residency for a new world.
		================
		*/
		reset() {
			if ( job ) {
				assets.cancel( job.id );
			}
			job = null;
			failures.clear();
			wanted.clear();
			loaded.clear();
			durations.clear();
			costs.clear();
			planned.clear();
			plannedBytes = 0;
			reserved = null;
			renderer.retainCharacterModels( [] );
		},

		/*
		================
		dispose

		Cancel work and release local records after the renderer lifecycle has ended.
		================
		*/
		dispose() {
			disposed = true;
			if ( job ) {
				assets.cancel( job.id );
			}
			job = null;
			failures.clear();
			wanted.clear();
			loaded.clear();
			durations.clear();
			costs.clear();
			planned.clear();
			plannedBytes = 0;
			reserved = null;
		}
	};
}
