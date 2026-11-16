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
import { withBearRoar, withWolfHowl, withWolfRend, withPenguinBlizzard } from "@/engine/foundation/animation/bear-roar";
import { createProceduralEffect } from "./procedural-effects";
import type { AssetOwner } from "@/engine/contracts/assets";
import type { CharacterModel } from "@/engine/contracts/character";
import type { Renderer } from "@/engine/contracts/runtime";
import type { NativeTexture } from "@/engine/contracts/texture";
// A failed resource has its own retry deadline. It never stops actor reconciliation.

/*
================
CHARACTER_LOAD_SLOTS

Character sources in flight at once. One slot made a crowd's wearers
dress a part a fetch, one admission a frame: at world entry everyone was
drawn bare for seconds. The asset owner's request slots are shared with
the world, the interface and audio, so a second or third character load
starts only while another slot stays free for them (openSlots).
================
*/
const CHARACTER_LOAD_SLOTS = 3;

/*
================
createCharacterResources

Own the decode reservations of the sources in flight and a bounded cache of
admitted character sources.
================
*/
export function createCharacterResources(
	assets: AssetOwner,
	renderer: Pick<Renderer, "setCharacterModel" | "setCharacterAnimation" | "retainCharacterModels">,
	origin: string
) {
	const durations = new Map<string, ReadonlyMap<string, number>>();
	// Decoded sources, each with the use stamp of its last admission: the
	// cache retires the least recently used first. A stamp, not a delete and
	// re-add per use: that churned a long-lived set every frame, and V8 puts
	// such a set's next table in the old generation, where only a full
	// collection frees it.
	const loaded = new Map<string, number>();
	let useStamp = 0;
	// The frame's admission intent. begin starts each frame with new sets
	// instead of clearing these: a long-lived set's next table is allocated
	// in the old generation, where only a full collection frees it.
	let wanted = new Set<string>(), planned = new Set<string>();
	// The local player's worn sources for this frame (prioritize). A free
	// load slot serves them before any other cold source starts: a newly
	// equipped part must not wait behind unrelated actors.
	const priority = new Set<string>();
	const costs = new Map<string, number>();
	const proceduralCache = new Map<string, { model: CharacterModel; images: NativeTexture[]; }>();
	let plannedBytes = 0;
	// Ordered requests are priority admission, not a list of everything nearby.
	// A decoded source is charged what it measured. A source that has never been
	// decoded reserves one source class, and only a load slot may hold that
	// reservation: an unstarted load must never be charged for residency a
	// resident actor still needs. Reservations are headroom, never a charge, so
	// speculation cannot displace an actor that is already decoded. This
	// frame's reservations, by path; a new set each frame (begin).
	let reservations = new Set<string>();
	// The model set retainWanted last handed the renderer (undefined before
	// the first), and the scratch it builds the next in. This owner is the
	// only caller of retainCharacterModels, which reads its list as a set
	// and holds the last one until dispose: an equal set is not handed
	// again.
	let handed: Set<string> | undefined, handing = new Set<string>();

	/*
	================
	openSlots

	Loads that may start now besides those in flight: this owner's slots
	less the ones in flight and the reservations still waiting to start
	(except `own`, the caller's), and the asset owner's free slots, of
	which any character load past the first leaves one to the world, the
	interface and audio.
	================
	*/
	function openSlots( own?: string ): number {
		if ( disposed ) return 0;
		let waiting = 0;
		for ( const path of reservations ) if ( path !== own && !jobs.has( path ) ) waiting++;
		const busy = jobs.size + waiting;
		return Math.max(
			0,
			Math.min( CHARACTER_LOAD_SLOTS - busy, assets.available() - waiting - (busy > 0 ? 1 : 0) )
		);
	}

	/*
	================
	reservable

	A cold source can reserve room only when a load slot can start it, or
	already holds one.
	================
	*/
	function reservable( path: string ): boolean {
		if ( jobs.has( path ) || reservations.has( path ) ) return true;
		if ( (failures.get( path )?.retryAt ?? -Infinity) > now ) return false;
		if ( !priority.has( path ) && priorityPending() ) return false;
		return openSlots() > 0;
	}

	/*
	================
	priorityPending

	A declared wearer source is still coming and waits for a slot now:
	not loaded, not in flight, and not waiting out a failure's retry (a
	part in retry must not hold the slots against everyone else).
	================
	*/
	function priorityPending(): boolean {
		for ( const path of priority ) {
			if (
				!loaded.has( path ) && !jobs.has( path ) && (failures.get( path )?.retryAt ?? -Infinity) <= now
			) return true;
		}
		return false;
	}

	/*
	================
	heldClasses

	The source classes the frame already holds back: this frame's
	reservations, and every load in flight that has neither a cost nor a
	reservation yet (one started in an earlier frame whose group has not
	planned again). `own` are the caller's paths, charged by the caller.
	plan and retainWanted both count with this, so a load in flight is
	never charged in one and missed in the other (#3576).
	================
	*/
	function heldClasses( own: readonly string[] = [] ): number {
		let held = reservations.size;
		for ( const path of jobs.keys() ) {
			if ( !costs.has( path ) && !reservations.has( path ) && !own.includes( path ) ) held++;
		}
		return held;
	}

	/*
	================
	plan

	Charge shared sources once and reserve each undecoded source a slot
	holds or can start. Planning an undecoded source needs a source class
	of headroom for it and for every other held class (heldClasses); a
	decoded source is never held back by them.
	================
	*/
	function plan( paths: readonly string[] ): boolean {
		// A path admitted this frame is already charged and wanted. Shared
		// crowd models must not allocate a Set/filter for each additional actor.
		if ( paths.every( path => planned.has( path ) ) ) return true;
		const extra = [ ...new Set( paths ) ].filter( path => !planned.has( path ) );
		// Equipped actors admit their parts one source at a time. A group
		// that needs two sources no slot holds yet cannot start both and
		// reserves neither.
		const cold = extra.filter( path => !costs.has( path ) );
		const unheld = cold.filter( path => !reservations.has( path ) );
		if (
			unheld.filter( path => !jobs.has( path ) ).length > 1 || cold.some( path => !reservable( path ) )
		) return false;
		const bytes = extra.reduce( ( sum, path ) => sum + (costs.get( path ) ?? 0), 0 );
		const headroom = cold.length ? (heldClasses( unheld ) + unheld.length) * CHARACTER_SOURCE_BYTES : 0;
		if (
			planned.size + extra.length > CHARACTER_MODELS ||
			plannedBytes + bytes + headroom > CHARACTER_RESIDENT_BYTES
		) return false;
		for ( const path of paths ) {
			planned.add( path );
			wanted.add( path );
		}
		plannedBytes += bytes;
		for ( const path of cold ) reservations.add( path );
		return true;
	}
	const failures = new Map<string, {
		retryAt: number;
		message: string;
	}>();
	// The loads in flight, by path (at most CHARACTER_LOAD_SLOTS).
	const jobs = new Map<string, {
		id: number;
		path: string;
		model: boolean;
		animation?: { body: string; name: string; };
	}>();
	// Byte sources (manifests) completed in one poll beyond the one it
	// returns; later polls return them in completion order.
	const deliveries: { path: string; buffer: ArrayBuffer; }[] = [];
	let now = 0, disposed = false;

	/*
	================
	prioritize

	Declares the local player's worn sources for the frames ahead. The
	declaration survives frame begins; a part that is still coming owns
	the next free slot ahead of manifests, clips and other actors. A load
	already in flight is never cancelled - with every slot busy the
	wearer's part takes one when it lands, so the wait stays bounded by
	one fetch.
	Callers redeclare on every appearance change; passing an empty list
	releases the lane.
	================
	*/
	function prioritize( paths: readonly string[] ): void {
		priority.clear();
		for ( const path of paths ) priority.add( path );
	}

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
			disposed || jobs.has( path ) || loaded.has( path ) ||
			(failures.get( path )?.retryAt ?? -Infinity) > now
		) {
			return;
		}
		// Manifests and clips skip plan(); the wearer's declared part
		// still owns the free slot ahead of them.
		if ( !priority.has( path ) && priorityPending() ) return;
		// A model's own reservation is the slot it starts in.
		if ( openSlots( model ? path : undefined ) === 0 ) return;
		try {
			if ( !path.startsWith( "/assets/" ) || path.includes( ".." ) || path.includes( "\\" ) ) {
				throw new Error( "Unpublished character resource" );
			}
			const decode = model ? (path.includes( "/effects/programs.json#" ) ? "effect" : "character") : undefined;
			jobs.set( path, {
				id: assets.request( new URL( path, origin ).href, assetRequestBudget( decode ), decode ),
				path,
				model
			} );
		} catch ( error ) {
			failures.set( path, { retryAt: now + 2, message: String( error ) } );
		}
	}

	/*
	================
	complete

	Transfer one completed source to the renderer, releasing rejected image
	resources; a byte source is returned to the caller.
	================
	*/
	function complete(
		completed: { path: string; model: boolean; animation?: { body: string; name: string; }; },
		result: NonNullable<ReturnType<AssetOwner["take"]>>
	): { path: string; buffer: ArrayBuffer; } | null {
		try {
			if ( result.kind === "error" ) {
				throw new Error( result.error );
			}
			if ( completed.model && result.kind === "character" ) {
				const model = /\/npc\/cos\/p_bear0[12]\.glb$/i.test( completed.path ) ?
					withBearRoar( result.model ) :
					/\/npc\/cos\/p_wolf(_white)?_0[12]\.glb$/i.test( completed.path ) ?
					withWolfRend( withWolfHowl( result.model ) ) :
					/\/npc\/cos\/p_penguin0[12]\.glb$/i.test( completed.path ) ?
					withPenguinBlizzard( result.model ) :
					result.model;
				try {
					costs.set( completed.path, characterBytes( model, result.images ) );
				} catch ( error ) {
					for ( const image of result.images ) if ( !("kind" in image) ) image.close();
					throw error;
				}
				renderer.setCharacterModel( completed.path, model, result.images );
				durations.set(
					completed.path,
					new Map( model.clips.map( clip => [ clip.name, clip.duration ] ) )
				);
				loaded.set( completed.path, ++useStamp );
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
			wanted = new Set();
			planned = new Set();
			plannedBytes = 0;
			reservations = new Set();
		},

		/*
		================
		poll

		Admit every source completed since the last poll, so loads that land
		together are admitted together; return one completed byte source.
		================
		*/
		poll(): {
			path: string;
			buffer: ArrayBuffer;
		} | null {
			if ( disposed ) {
				return null;
			}
			for ( const [path, job] of jobs ) {
				const result = assets.take( job.id );
				if ( !result ) continue;
				jobs.delete( path );
				const delivered = complete( job, result );
				if ( delivered ) deliveries.push( delivered );
			}
			return deliveries.shift() ?? null;
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
			const fresh = !jobs.has( path );
			request( path, false );
			const started = jobs.get( path );
			if ( fresh && started ) started.animation = { body, name };
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
			// A resident source decides the same below without the procedural
			// build (charged when it was admitted: loaded and costs are set and
			// retired together), the failure retry (loaded) and the request (it
			// returns for a loaded source): it is planned, then moved to the
			// recent end. The common call, every worn part of every actor.
			if ( loaded.has( path ) ) {
				// A path planned this frame plans again to true, changing nothing.
				if ( !planned.has( path ) && !plan( [ path ] ) ) return false;
				loaded.set( path, ++useStamp );
				return true;
			}
			let built = proceduralCache.get( path );
			if ( !built ) {
				built = createProceduralEffect( path ) ?? undefined;
				if ( built ) proceduralCache.set( path, built );
			}
			if ( built && !costs.has( path ) ) costs.set( path, characterBytes( built.model, built.images ) );
			// A failing source keeps its own retry deadline and stays wanted so
			// retainWanted does not drop the deadline and refetch every frame.
			if ( !loaded.has( path ) && (failures.get( path )?.retryAt ?? -Infinity) > now ) {
				wanted.add( path );
				return false;
			}
			if ( !plan( [ path ] ) ) return false;
			if ( built ) {
				if ( !loaded.has( path ) ) {
					renderer.setCharacterModel( path, built.model, built.images );
					durations.set(
						path,
						new Map( built.model.clips.map( clip => [ clip.name, clip.duration ] ) )
					);
					loaded.set( path, ++useStamp );
				}
			} else {
				request( path, true );
			}
			if ( !loaded.has( path ) ) return false;
			// Most recently used last: the cache retires the oldest inactive
			// source first when the frame takes its room back.
			loaded.set( path, ++useStamp );
			return true;
		},

		/*
		================
		retainWanted

		Preserve wanted sources first, then retain inactive sources within the
		remaining budget; hand the renderer the model set when it changed.
		================
		*/
		retainWanted( assemblies: readonly string[] ) {
			for ( const [path, job] of jobs ) {
				if ( !wanted.has( path ) ) {
					assets.cancel( job.id );
					jobs.delete( path );
				}
			}
			// A finished attack is not a resource invalidation. Keep decoded
			// models in the remaining bounded budget, most recently used first.
			// Each in-flight source class is held back, so inactive cache entries
			// retire before the next poll can admit its decode.
			let bytes = plannedBytes + heldClasses() * CHARACTER_SOURCE_BYTES,
				count = planned.size;
			// Most frames keep everything: every inactive source fits, so every
			// prefix of the recency order fits too and nothing is retired. Only
			// an overflowing cache pays for the sort.
			let inactive = 0, inactiveBytes = 0;
			for ( const path of loaded.keys() ) {
				if ( !planned.has( path ) ) {
					inactive++;
					inactiveBytes += costs.get( path )!;
				}
			}
			if ( count + inactive > CHARACTER_MODELS || bytes + inactiveBytes > CHARACTER_RESIDENT_BYTES ) {
				const keep = new Set( planned );
				const recentFirst = [ ...loaded.keys() ].sort( ( a, b ) => loaded.get( b )! - loaded.get( a )! );
				for ( const path of recentFirst ) {
					if ( !keep.has( path ) ) {
						const cost = costs.get( path )!;
						if ( count < CHARACTER_MODELS && bytes + cost <= CHARACTER_RESIDENT_BYTES ) {
							keep.add( path );
							count++;
							bytes += cost;
						}
					}
				}
				for ( const path of loaded.keys() ) {
					if ( !keep.has( path ) ) {
						loaded.delete( path );
						durations.delete( path );
						costs.delete( path );
					}
				}
			}
			for ( const path of failures.keys() ) {
				if ( !wanted.has( path ) ) {
					failures.delete( path );
				}
			}
			handing.clear();
			for ( const path of wanted ) handing.add( path );
			for ( const path of loaded.keys() ) handing.add( path );
			for ( const path of assemblies ) handing.add( path );
			let same = handed?.size === handing.size;
			for ( const path of handing ) {
				if ( !same ) break;
				same = handed!.has( path );
			}
			if ( !same ) {
				renderer.retainCharacterModels( [ ...handing ] );
				const previous = handed;
				handed = handing;
				handing = previous ?? new Set();
			}
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
		failure

		The first retained failure with the path that produced it: a
		diagnostic that only says "asset-load" cannot name its source.
		================
		*/
		failure() {
			const first = failures.entries().next().value;
			return first ? { path: first[0], message: first[1].message } : null;
		},

		/*
		================
		reset

		Cancel pending work and release all renderer residency for a new world.
		================
		*/
		reset() {
			for ( const job of jobs.values() ) assets.cancel( job.id );
			jobs.clear();
			deliveries.length = 0;
			failures.clear();
			wanted.clear();
			loaded.clear();
			durations.clear();
			costs.clear();
			planned.clear();
			plannedBytes = 0;
			reservations = new Set();
			priority.clear();
			renderer.retainCharacterModels( [] );
			handed = new Set();
		},

		/*
		================
		dispose

		Cancel work and release local records after the renderer lifecycle has ended.
		================
		*/
		dispose() {
			disposed = true;
			for ( const job of jobs.values() ) assets.cancel( job.id );
			jobs.clear();
			deliveries.length = 0;
			failures.clear();
			wanted.clear();
			loaded.clear();
			durations.clear();
			costs.clear();
			planned.clear();
			plannedBytes = 0;
			reservations = new Set();
			priority.clear();
		},
		prioritize
	};
}
